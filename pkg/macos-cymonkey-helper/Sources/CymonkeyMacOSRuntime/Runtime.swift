import AppKit
import Foundation

public actor CymonkeyRuntime {
    private let configuration: HelperConfiguration
    private let appleEvents: AppleEventSending
    private let accessibility: AccessibilityProviding?
    private let viewer: ViewerProviding?
    private var events: [SemanticEvent] = []
    private var sequence: UInt64 = 0

    public init(
        configuration: HelperConfiguration,
        appleEvents: AppleEventSending = SystemAppleEventSender(),
        accessibility: AccessibilityProviding? = nil,
        viewer: ViewerProviding? = nil
    ) throws {
        try configuration.validate()
        self.configuration = configuration
        self.appleEvents = appleEvents
        if let accessibility {
            self.accessibility = accessibility
        } else if let policy = configuration.accessibility {
            self.accessibility = SystemAccessibilityProvider(policy: policy)
        } else {
            self.accessibility = nil
        }
        if let viewer {
            self.viewer = viewer
        } else if let policy = configuration.viewer, policy.enabled {
            self.viewer = SystemViewerProvider(allowedBundleIDs: configuration.allowedBundleIds, policy: policy)
        } else {
            self.viewer = nil
        }
    }

    public func handle(_ request: ControlRequest) -> ControlResponse {
        do {
            let result: JSONValue
            switch request.method {
            case "hello": result = hello()
            case "capabilities": result = try encodeToJSON(capabilities())
            case "describe": result = describe()
            case "act": result = try act(request.params)
            case "events": result = try readEvents(request.params)
            default: throw RuntimeError.invalidRequest("unsupported Cymonkey method")
            }
            return ControlResponse(id: request.id, result: result)
        } catch let error as RuntimeError {
            return ControlResponse(id: request.id, error: ControlError(code: error.code, message: error.description))
        } catch {
            return ControlResponse(id: request.id, error: ControlError(code: "internal", message: "native helper operation failed"))
        }
    }

    public func capabilities() -> [Capability] {
        var result: [Capability] = []
        if !configuration.appleEventCommands.isEmpty {
            result.append(Capability(
                name: "app.command.list",
                description: "List owner-allowlisted application commands.",
                driver: "macos-apple-events",
                effect: "read",
                required: ["surfaceId"]
            ))
            result.append(Capability(
                name: "app.command.describe",
                description: "Describe one owner-allowlisted application command.",
                driver: "macos-apple-events",
                effect: "read",
                required: ["surfaceId", "command"]
            ))
            result.append(Capability(
                name: "app.command.invoke",
                description: "Invoke one typed, owner-allowlisted Apple Event command.",
                driver: "macos-apple-events",
                effect: "external",
                required: ["surfaceId", "command"],
                additionalProperties: true
            ))
        }
        if let accessibility, accessibility.authorized {
            result.append(Capability(
                name: "ui.query",
                description: "Query a bounded Accessibility subtree in one allowlisted application.",
                driver: "macos-accessibility",
                effect: "read",
                required: ["surfaceId", "selector"]
            ))
            result.append(Capability(
                name: "ui.action.invoke",
                description: "Invoke an action reported by an attachment-scoped Accessibility element.",
                driver: "macos-accessibility",
                effect: "write",
                required: ["surfaceId", "elementId", "action"]
            ))
            if !(configuration.accessibility?.allowedWritableAttributes.isEmpty ?? true) {
                result.append(Capability(
                    name: "ui.attribute.set",
                    description: "Set an allowlisted attribute reported settable by an Accessibility element.",
                    driver: "macos-accessibility",
                    effect: "write",
                    required: ["surfaceId", "elementId", "attribute", "value"]
                ))
            }
        }
        if let viewer, viewer.captureAuthorized {
            result.append(Capability(name: "display.describe", description: "Describe an allowlisted desktop viewer surface.", driver: "macos-viewer", effect: "read", required: ["surfaceId"], runtime: "macos-viewer"))
            result.append(Capability(name: "display.capture", description: "Capture an allowlisted desktop viewer surface.", driver: "macos-viewer", effect: "read", required: ["surfaceId"], runtime: "macos-viewer"))
        }
        if let viewer, viewer.inputAuthorized {
            for name in ["pointer.move", "pointer.click", "pointer.drag", "pointer.scroll", "keyboard.type", "keyboard.press"] {
                result.append(Capability(name: name, description: "Inject policy-bounded input into an allowlisted viewer surface.", driver: "macos-viewer", effect: "write", required: ["surfaceId"], additionalProperties: true, runtime: "macos-viewer"))
            }
        }
        return result.sorted { $0.name < $1.name }
    }

    private func hello() -> JSONValue {
        var drivers: [JSONValue] = []
        if !configuration.appleEventCommands.isEmpty { drivers.append(.string("macos-apple-events")) }
        if accessibility != nil { drivers.append(.string("macos-accessibility")) }
        if viewer != nil { drivers.append(.string("macos-viewer")) }
        var runtimes: [JSONValue] = [.string("macos-app")]
        if viewer != nil { runtimes.append(.string("macos-viewer")) }
        return .object([
            "protocolVersion": .string(cymonkeyProtocolVersion),
            "implementation": .object([
                "name": .string("jangolova-cymonkey-macos-helper"),
                "version": .string("0.1.0"),
            ]),
            "domains": .array([.string("viewer")]),
            "runtimes": .array(runtimes),
            "drivers": .array(drivers),
            "features": .array([.string("events.cursor"), .string("consent.negotiated")]),
        ])
    }

    private func describe() -> JSONValue {
        var surfaces = runningSurfaces().map { surface in
            JSONValue.object([
                "id": .string(surface.id),
                "domain": .string("viewer"),
                "runtime": .string("macos-app"),
                "kind": .string("window"),
                "label": surface.label.map(JSONValue.string) ?? .null,
                "properties": .object([
                    "bundleId": .string(surface.bundleId),
                    "processId": .number(Double(surface.processId)),
                ]),
            ])
        }
        if let viewer {
            surfaces.append(contentsOf: viewer.surfaces().map { surface in
                .object([
                    "id": .string(surface.id), "domain": .string("viewer"), "runtime": .string("macos-viewer"), "kind": .string("viewport"),
                    "label": surface.label.map(JSONValue.string) ?? .null,
                    "properties": .object(["processId": .number(Double(surface.processId)), "windowId": .number(Double(surface.windowId)), "x": .number(Double(surface.x)), "y": .number(Double(surface.y)), "width": .number(Double(surface.width)), "height": .number(Double(surface.height))]),
                ])
            })
        }
        return .object([
            "revision": .string(surfaceRevision(surfaces: runningSurfaces())),
            "surfaces": .array(surfaces),
            "augmentations": .array([]),
            "consent": .object([
                "accessibility": .string(accessibility?.authorized == true ? "granted" : "unavailable"),
                "automation": .string("per-command"),
                "screenCapture": .string(viewer?.captureAuthorized == true ? "granted" : "unavailable"),
            ]),
        ])
    }

    private func act(_ params: JSONValue) throws -> JSONValue {
        let action = try decode(ActionRequest.self, from: params)
        guard let input = action.input.objectValue else {
            throw RuntimeError.invalidRequest("Cymonkey action input must be an object")
        }
        let result: JSONValue
        switch action.name {
        case "app.command.list":
            let bundleID = try bundleIDForSurface(requiredString(input, "surfaceId"))
            result = .array(commands(bundleID: bundleID).map { .string($0.name) })
        case "app.command.describe":
            let command = try requireCommand(input)
            result = describeCommand(command)
        case "app.command.invoke":
            let command = try requireCommand(input)
            result = try appleEvents.invoke(command: command, input: input)
            publish("app.command.invoked", driver: "macos-apple-events", data: .object([
                "bundleId": .string(command.bundleId), "command": .string(command.name),
            ]))
        case "ui.query":
            guard let accessibility else { throw RuntimeError.unavailable("Accessibility backend is unavailable") }
            let surfaceID = try requiredString(input, "surfaceId")
            guard let selector = input["selector"]?.objectValue else {
                throw RuntimeError.invalidRequest("ui.query selector must be an object")
            }
            result = try encodeToJSON(accessibility.query(surfaceId: surfaceID, selector: selector))
        case "ui.action.invoke":
            guard let accessibility else { throw RuntimeError.unavailable("Accessibility backend is unavailable") }
            let surfaceID = try requiredString(input, "surfaceId")
            let elementID = try requiredString(input, "elementId")
            let nativeAction = try requiredString(input, "action")
            try accessibility.perform(surfaceId: surfaceID, elementId: elementID, action: nativeAction)
            result = .object(["ok": .bool(true)])
            publish("ui.action.invoked", driver: "macos-accessibility", data: .object([
                "surfaceId": .string(surfaceID), "elementId": .string(elementID), "action": .string(nativeAction),
            ]))
        case "ui.attribute.set":
            guard let accessibility else { throw RuntimeError.unavailable("Accessibility backend is unavailable") }
            let surfaceID = try requiredString(input, "surfaceId")
            let elementID = try requiredString(input, "elementId")
            let attribute = try requiredString(input, "attribute")
            guard let value = input["value"] else { throw RuntimeError.invalidRequest("value is required") }
            try accessibility.setAttribute(surfaceId: surfaceID, elementId: elementID, attribute: attribute, value: value)
            result = .object(["ok": .bool(true)])
            publish("ui.attribute.updated", driver: "macos-accessibility", data: .object([
                "surfaceId": .string(surfaceID), "elementId": .string(elementID), "attribute": .string(attribute),
            ]))
        case "display.describe":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            let surfaceID = try requiredString(input, "surfaceId")
            guard let surface = viewer.surfaces().first(where: { $0.id == surfaceID }) else { throw RuntimeError.staleReference("viewer surface is unavailable or stale") }
            result = .object(["surfaceId": .string(surface.id), "width": .number(Double(surface.width)), "height": .number(Double(surface.height)), "coordinateSpace": .string("window-local")])
        case "display.capture":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            let capture = try viewer.capture(surfaceId: try requiredString(input, "surfaceId"))
            result = try encodeToJSON(capture)
        case "pointer.move":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            try viewer.move(surfaceId: try requiredString(input, "surfaceId"), x: try requiredInt(input, "x"), y: try requiredInt(input, "y")); result = .object(["ok": .bool(true)])
        case "pointer.click":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            try viewer.click(surfaceId: try requiredString(input, "surfaceId"), x: try requiredInt(input, "x"), y: try requiredInt(input, "y"), button: input["button"]?.stringValue ?? "left"); result = .object(["ok": .bool(true)])
        case "pointer.drag":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            try viewer.drag(surfaceId: try requiredString(input, "surfaceId"), startX: try requiredInt(input, "startX"), startY: try requiredInt(input, "startY"), endX: try requiredInt(input, "endX"), endY: try requiredInt(input, "endY")); result = .object(["ok": .bool(true)])
        case "pointer.scroll":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            try viewer.scroll(surfaceId: try requiredString(input, "surfaceId"), x: try requiredInt(input, "x"), y: try requiredInt(input, "y"), deltaY: try requiredInt(input, "deltaY")); result = .object(["ok": .bool(true)])
        case "keyboard.type":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            let text = try requiredString(input, "text")
            try viewer.type(surfaceId: try requiredString(input, "surfaceId"), text: text)
            result = .object(["ok": .bool(true)])
        case "keyboard.press":
            guard let viewer else { throw RuntimeError.unavailable("viewer backend is unavailable") }
            try viewer.press(surfaceId: try requiredString(input, "surfaceId"), key: try requiredString(input, "key")); result = .object(["ok": .bool(true)])
        default:
            throw RuntimeError.denied("Cymonkey capability was not advertised")
        }
        return result
    }

    private func readEvents(_ params: JSONValue) throws -> JSONValue {
        let input = params.objectValue ?? [:]
        let after = input["after"]?.stringValue.flatMap(UInt64.init) ?? 0
        let requestedLimit = input["limit"].flatMap { value -> Int? in
            guard case .number(let number) = value else { return nil }
            return Int(number)
        } ?? 100
        let limit = max(1, min(500, requestedLimit))
        let selected = events.filter { UInt64($0.id) ?? 0 > after }.prefix(limit)
        return .object([
            "events": (try? encodeToJSON(Array(selected))) ?? .array([]),
            "cursor": .string(selected.last?.id ?? String(after)),
        ])
    }

    private func publish(_ type: String, driver: String, data: JSONValue) {
        sequence += 1
        events.append(SemanticEvent(
            id: String(sequence), type: type,
            occurredAt: ISO8601DateFormatter().string(from: Date()),
            domain: "viewer", runtime: "macos-app", driver: driver, data: data
        ))
        if events.count > 1_000 { events.removeFirst(events.count - 1_000) }
    }

    private func runningSurfaces() -> [AccessibilitySurface] {
        let configured = Set(configuration.allowedBundleIds)
        var result = accessibility?.surfaces() ?? []
        let existing = Set(result.map(\.id))
        for bundleID in configured {
            for application in NSRunningApplication.runningApplications(withBundleIdentifier: bundleID) {
                let candidate = AccessibilitySurface(
                    id: "macos:\(bundleID):\(application.processIdentifier)",
                    bundleId: bundleID,
                    processId: application.processIdentifier,
                    label: application.localizedName
                )
                if !existing.contains(candidate.id) { result.append(candidate) }
            }
        }
        return result.sorted { $0.id < $1.id }
    }

    private func surfaceRevision(surfaces: [AccessibilitySurface]) -> String {
        surfaces.map(\.id).joined(separator: "|")
    }

    private func commands(bundleID: String) -> [AppleEventCommand] {
        configuration.appleEventCommands.filter { $0.bundleId == bundleID }.sorted { $0.name < $1.name }
    }

    private func describeCommand(_ command: AppleEventCommand) -> JSONValue {
        .object([
            "name": .string(command.name),
            "bundleId": .string(command.bundleId),
            "inputs": .array(command.parameters.map { parameter in
                .object([
                    "name": .string(parameter.input),
                    "type": .string(parameter.type.rawValue),
                    "required": .bool(parameter.required),
                ])
            }),
        ])
    }

    private func requireCommand(_ input: [String: JSONValue]) throws -> AppleEventCommand {
        let bundleID = try bundleIDForSurface(requiredString(input, "surfaceId"))
        let name = try requiredString(input, "command")
        guard let command = commands(bundleID: bundleID).first(where: { $0.name == name }) else {
            throw RuntimeError.denied("Apple Event command is not allowlisted for the surface")
        }
        return command
    }

    private func bundleIDForSurface(_ surfaceID: String) throws -> String {
        guard runningSurfaces().contains(where: { $0.id == surfaceID }),
              let bundleID = configuration.allowedBundleIds.first(where: { surfaceID.hasPrefix("macos:\($0):") }) else {
            throw RuntimeError.staleReference("application surface is unavailable or stale")
        }
        return bundleID
    }
}

private func requiredString(_ input: [String: JSONValue], _ name: String) throws -> String {
    guard let value = input[name]?.stringValue, !value.isEmpty else {
        throw RuntimeError.invalidRequest("\(name) is required")
    }
    return value
}

private func requiredInt(_ input: [String: JSONValue], _ name: String) throws -> Int {
    guard case .number(let value) = input[name], value.rounded() == value else {
        throw RuntimeError.invalidRequest("\(name) must be an integer")
    }
    return Int(value)
}

private func encodeToJSON<T: Encodable>(_ value: T) throws -> JSONValue {
    let data = try JSONEncoder().encode(value)
    return try JSONDecoder().decode(JSONValue.self, from: data)
}

private func decode<T: Decodable>(_ type: T.Type, from value: JSONValue) throws -> T {
    let data = try JSONEncoder().encode(value)
    return try JSONDecoder().decode(type, from: data)
}

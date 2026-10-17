import ApplicationServices
import AppKit
import Foundation

public struct AccessibilitySurface: Codable, Equatable, Sendable {
    public let id: String
    public let bundleId: String
    public let processId: Int32
    public let label: String?
}

public struct AccessibilityElementDescription: Codable, Equatable, Sendable {
    public let id: String
    public let surfaceId: String
    public let role: String?
    public let title: String?
    public let identifier: String?
    public let actions: [String]
    public let settableAttributes: [String]
    public let bounds: NativeBounds?
    public let attributes: [String: JSONValue]?

    public init(id: String, surfaceId: String, role: String?, title: String?, identifier: String?, actions: [String], settableAttributes: [String], bounds: NativeBounds? = nil, attributes: [String: JSONValue]? = nil) {
        self.id = id; self.surfaceId = surfaceId; self.role = role; self.title = title
        self.identifier = identifier; self.actions = actions; self.settableAttributes = settableAttributes
        self.bounds = bounds; self.attributes = attributes
    }
}

public protocol AccessibilityProviding: Sendable {
    var authorized: Bool { get }
    func surfaces() -> [AccessibilitySurface]
    func query(surfaceId: String, selector: [String: JSONValue]) throws -> [AccessibilityElementDescription]
    func perform(surfaceId: String, elementId: String, action: String) throws
    func setAttribute(surfaceId: String, elementId: String, attribute: String, value: JSONValue) throws
}

public final class SystemAccessibilityProvider: ObservationProviding, AttributeReading, @unchecked Sendable {
    private struct Reference {
        let surfaceId: String
        let processId: pid_t
        let element: AXUIElement
        let generation: String
        let expires: UInt64
    }

    private let policy: AccessibilityPolicy
    private let lock = NSRecursiveLock()
    private let producerID = UUID().uuidString.lowercased()
    private var observations: [String: (NativeTargetReference, UInt64, Set<String>, AXUIElement?)] = [:]
    private var references: [String: Reference] = [:]

    public init(policy: AccessibilityPolicy) {
        self.policy = policy
        if policy.promptForAccessibilityConsent {
            _ = AXIsProcessTrustedWithOptions([
                kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true,
            ] as CFDictionary)
        }
    }

    public var authorized: Bool { AXIsProcessTrusted() }

    public func surfaces() -> [AccessibilitySurface] {
        policy.allowedBundleIds.flatMap { bundleID in
            NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).map { application in
                AccessibilitySurface(
                    id: surfaceID(bundleID: bundleID, processID: application.processIdentifier),
                    bundleId: bundleID,
                    processId: application.processIdentifier,
                    label: application.localizedName
                )
            }
        }.sorted { $0.id < $1.id }
    }

    public func query(surfaceId: String, selector: [String: JSONValue]) throws -> [AccessibilityElementDescription] {
        try observe(surfaceId: surfaceId, selector: selector).elements
    }

    public func observe(surfaceId: String, selector: [String: JSONValue]) throws -> NativeObservation {
        if ownProcess(surfaceId) && !Thread.isMainThread { return try DispatchQueue.main.sync { try observe(surfaceId: surfaceId, selector: selector) } }
        lock.lock(); defer { lock.unlock() }
        try requireAuthorization()
        let surface = try requireSurface(surfaceId)
        let generation = try nativeProcessGeneration(pid_t(surface.processId))
        let allowedKeys: Set<String> = ["role", "title", "identifier", "limit", "windowId"]
        guard Set(selector.keys).isSubset(of: allowedKeys) else { throw RuntimeError.invalidRequest("unsupported Accessibility selector") }
        for key in ["role", "title", "identifier", "windowId"] {
            if let v = selector[key], v.stringValue == nil { throw RuntimeError.invalidRequest("selector must use string values") }
        }
        let requestedLimit = selector["limit"].flatMap(numberValue) ?? Double(policy.maxResults)
        guard requestedLimit.isFinite, requestedLimit.rounded() == requestedLimit, requestedLimit >= 1, requestedLimit <= 500 else { throw RuntimeError.invalidRequest("invalid query limit") }
        let limit = min(policy.maxResults, Int(requestedLimit))
        let root = AXUIElementCreateApplication(pid_t(surface.processId))
        AXUIElementSetMessagingTimeout(root, 0.25)
        let windowID = selector["windowId"]?.stringValue
        var queryRoot = root
        if let windowID {
            queryRoot = try exactWindow(root: root, pid: surface.processId, windowID: windowID)
        }
        let start = DispatchTime.now().uptimeNanoseconds
        var queue: [(AXUIElement, Int)] = [(queryRoot, 0)]
        var cursor = 0
        var results: [AccessibilityElementDescription] = []
        var incomplete = false
        var reason: String?
        while cursor < queue.count {
            if cursor >= 5000 || DispatchTime.now().uptimeNanoseconds - start > 2_000_000_000 {
                incomplete = true; reason = "native read budget exceeded"; break
            }
            let (element, depth) = queue[cursor]; cursor += 1
            AXUIElementSetMessagingTimeout(element, 0.25)
            let match = matchesKnown(element, selector: selector)
            if match == nil { incomplete = true; reason = "selector attribute unavailable" }
            if match == true {
                results.append(try describe(element, surface: surface, generation: generation))
                if results.count >= limit { incomplete = true; reason = "result limit reached"; break }
            }
            var childrenValue: CFTypeRef?
            let status = AXUIElementCopyAttributeValue(element, kAXChildrenAttribute as CFString, &childrenValue)
            let children = childrenValue as? [AXUIElement] ?? []
            if status != .success && status != .noValue && status != .attributeUnsupported {
                incomplete = true; reason = "children unavailable"
            }
            if !children.isEmpty {
                if depth >= policy.maxDepth { incomplete = true; reason = "depth limit reached" }
                else if queue.count + children.count > 5000 { incomplete = true; reason = "node limit reached" }
                else { queue.append(contentsOf: children.map { ($0, depth + 1) }) }
            }
        }
        guard try nativeProcessGeneration(surface.processId) == generation else { throw RuntimeError.staleReference("process changed during observation") }
        let now = Date()
        let expiry = DispatchTime.now().uptimeNanoseconds + 15_000_000_000
        observations = observations.filter { $0.value.1 > DispatchTime.now().uptimeNanoseconds }
        references = references.filter { $0.value.expires > DispatchTime.now().uptimeNanoseconds }
        if observations.count >= 128 { observations.removeAll() }
        let target = NativeTargetReference(surfaceId: surfaceId, producerId: producerID, processGeneration: generation, windowId: windowID)
        let id = UUID().uuidString.lowercased()
        observations[id] = (target, expiry, Set(results.map(\.id)), windowID == nil ? nil : queryRoot)
        return NativeObservation(id: id, target: target, observedAt: nativeTimestamp(now), expiresAt: nativeTimestamp(now.addingTimeInterval(15)), coordinateSpace: "screen-points", completeness: incomplete ? "partial" : "complete", reason: reason, visited: cursor, elements: results)
    }

    public func validateObservation(id: String, target: NativeTargetReference) throws {
        if ownProcess(target.surfaceId) && !Thread.isMainThread { return try DispatchQueue.main.sync { try validateObservation(id: id, target: target) } }
        lock.lock(); defer { lock.unlock() }
        guard let snapshot = observations[id], snapshot.0 == target,
              snapshot.1 > DispatchTime.now().uptimeNanoseconds else { throw RuntimeError.staleReference("observation is expired or foreign") }
        let surface = try requireSurface(target.surfaceId)
        guard try nativeProcessGeneration(surface.processId) == target.processGeneration else { throw RuntimeError.staleReference("observed process changed") }
        if let windowID = target.windowId, let observedWindow = snapshot.3 {
            let root = AXUIElementCreateApplication(surface.processId)
            AXUIElementSetMessagingTimeout(root, 0.25)
            let liveWindow = try exactWindow(root: root, pid: surface.processId, windowID: windowID)
            guard CFEqual(liveWindow, observedWindow) else { throw RuntimeError.staleReference("observed window identity changed") }
        }
    }

    public func validateElement(observationId: String, elementId: String) throws {
        lock.lock(); defer { lock.unlock() }
        guard let snapshot = observations[observationId], snapshot.1 > DispatchTime.now().uptimeNanoseconds,
              snapshot.2.contains(elementId) else { throw RuntimeError.staleReference("element does not belong to the observation") }
    }

    private func exactWindow(root: AXUIElement, pid: Int32, windowID: String) throws -> AXUIElement {
        guard let id = UInt32(windowID),
              let windows = CGWindowListCopyWindowInfo(.optionIncludingWindow, id) as? [[String: Any]],
              let info = windows.first,
              (info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid,
              let bounds = info[kCGWindowBounds as String] as? [String: Any],
              let rect = CGRect(dictionaryRepresentation: bounds as CFDictionary) else { throw RuntimeError.staleReference("exact window is unavailable") }
        let candidates = elementArrayAttribute(root, kAXWindowsAttribute as String).filter { element in
            guard let b = nativeBounds(element) else { return false }
            return abs(b.x - rect.origin.x) < 1 && abs(b.y - rect.origin.y) < 1 && abs(b.width - rect.width) < 1 && abs(b.height - rect.height) < 1
        }
        guard candidates.count == 1 else { throw RuntimeError.unavailable("window cannot be uniquely correlated with Accessibility") }
        return candidates[0]
    }

    public func perform(surfaceId: String, elementId: String, action: String) throws {
        if ownProcess(surfaceId) && !Thread.isMainThread { return try DispatchQueue.main.sync { try perform(surfaceId: surfaceId, elementId: elementId, action: action) } }
        lock.lock(); defer { lock.unlock() }
        try requireAuthorization()
        guard action.hasPrefix("AX"), !action.contains("\n") else {
            throw RuntimeError.invalidRequest("invalid Accessibility action")
        }
        let reference = try resolve(surfaceId: surfaceId, elementId: elementId)
        var names: CFArray?
        guard AXUIElementCopyActionNames(reference.element, &names) == .success,
              let supported = names as? [String], supported.contains(action) else {
            throw RuntimeError.denied("Accessibility action is not supported by the element")
        }
        guard AXUIElementPerformAction(reference.element, action as CFString) == .success else {
            throw RuntimeError.nativeFailure("Accessibility action failed")
        }
    }

    public func setAttribute(surfaceId: String, elementId: String, attribute: String, value: JSONValue) throws {
        if ownProcess(surfaceId) && !Thread.isMainThread { return try DispatchQueue.main.sync { try setAttribute(surfaceId: surfaceId, elementId: elementId, attribute: attribute, value: value) } }
        lock.lock(); defer { lock.unlock() }
        try requireAuthorization()
        guard policy.allowedWritableAttributes.contains(attribute) else {
            throw RuntimeError.denied("Accessibility attribute is not allowlisted")
        }
        let reference = try resolve(surfaceId: surfaceId, elementId: elementId)
        var settable = DarwinBoolean(false)
        guard AXUIElementIsAttributeSettable(reference.element, attribute as CFString, &settable) == .success,
              settable.boolValue else {
            throw RuntimeError.denied("Accessibility attribute is not settable")
        }
        let nativeValue: CFTypeRef
        switch value {
        case .string(let decoded): nativeValue = decoded as CFString
        case .bool(let decoded): nativeValue = decoded as CFBoolean
        case .number(let decoded):
            guard decoded.isFinite else { throw RuntimeError.invalidRequest("attribute number must be finite") }
            nativeValue = decoded as CFNumber
        case .object(let object):
            if attribute == kAXPositionAttribute as String, object.count == 2,
               case .number(let x) = object["x"], case .number(let y) = object["y"], x.isFinite, y.isFinite,
               abs(x) <= 1_000_000, abs(y) <= 1_000_000 {
                var point = CGPoint(x: x, y: y)
                guard let encoded = AXValueCreate(.cgPoint, &point) else { throw RuntimeError.invalidRequest("invalid position") }
                nativeValue = encoded
            } else if attribute == kAXSizeAttribute as String, object.count == 2,
                      case .number(let width) = object["width"], case .number(let height) = object["height"],
                      width.isFinite, height.isFinite, width > 0, height > 0, width <= 100_000, height <= 100_000 {
                var size = CGSize(width: width, height: height)
                guard let encoded = AXValueCreate(.cgSize, &size) else { throw RuntimeError.invalidRequest("invalid size") }
                nativeValue = encoded
            } else if attribute == kAXSelectedTextRangeAttribute as String, object.count == 2,
                      case .number(let location) = object["location"], case .number(let length) = object["length"],
                      location.isFinite, length.isFinite, location >= 0, length >= 0,
                      location.rounded() == location, length.rounded() == length, location + length <= 1_000_000 {
                // AX text offsets are UTF-16 units, not bytes or grapheme indices.
                var range = CFRange(location: Int(location), length: Int(length))
                guard let encoded = AXValueCreate(.cfRange, &range) else { throw RuntimeError.invalidRequest("invalid text range") }
                nativeValue = encoded
            } else { throw RuntimeError.invalidRequest("unsupported structured Accessibility value") }
        default: throw RuntimeError.invalidRequest("unsupported Accessibility attribute value")
        }
        guard AXUIElementSetAttributeValue(reference.element, attribute as CFString, nativeValue) == .success else {
            throw RuntimeError.nativeFailure("Accessibility attribute update failed")
        }
    }

    private func requireAuthorization() throws {
        guard authorized else {
            throw RuntimeError.unavailable("Accessibility consent is not granted")
        }
    }

    private func requireSurface(_ id: String) throws -> AccessibilitySurface {
        guard let surface = surfaces().first(where: { $0.id == id }) else {
            throw RuntimeError.staleReference("application surface is unavailable or stale")
        }
        return surface
    }

    private func resolve(surfaceId: String, elementId: String) throws -> Reference {
        let surface = try requireSurface(surfaceId)
        lock.lock()
        let reference = references[elementId]
        lock.unlock()
        guard let reference,
              reference.surfaceId == surfaceId,
              reference.processId == pid_t(surface.processId),
              reference.expires > DispatchTime.now().uptimeNanoseconds,
              reference.generation == (try nativeProcessGeneration(surface.processId)) else {
            throw RuntimeError.staleReference("Accessibility element reference is unavailable or stale")
        }
        return reference
    }

    private func describe(_ element: AXUIElement, surface: AccessibilitySurface, generation: String) throws -> AccessibilityElementDescription {
        let id = UUID().uuidString.lowercased()
        lock.lock()
        references[id] = Reference(surfaceId: surface.id, processId: pid_t(surface.processId), element: element, generation: generation, expires: DispatchTime.now().uptimeNanoseconds + 15_000_000_000)
        if references.count > policy.maxResults * 20 {
            references.removeAll(keepingCapacity: true)
            references[id] = Reference(surfaceId: surface.id, processId: pid_t(surface.processId), element: element, generation: generation, expires: DispatchTime.now().uptimeNanoseconds + 15_000_000_000)
        }
        lock.unlock()
        let actions = actionNames(element)
        let settable = policy.allowedWritableAttributes.filter { attribute in
            var result = DarwinBoolean(false)
            return AXUIElementIsAttributeSettable(element, attribute as CFString, &result) == .success && result.boolValue
        }
        return AccessibilityElementDescription(
            id: id,
            surfaceId: surface.id,
            role: stringAttribute(element, kAXRoleAttribute as String),
            title: stringAttribute(element, kAXTitleAttribute as String),
            identifier: stringAttribute(element, kAXIdentifierAttribute as String),
            actions: actions,
            settableAttributes: settable.sorted(),
            bounds: nativeBounds(element),
            attributes: nativeAttributes(element)
        )
    }

    private func matchesKnown(_ element: AXUIElement, selector: [String: JSONValue]) -> Bool? {
        for (key, attribute) in [("role", kAXRoleAttribute as String), ("title", kAXTitleAttribute as String), ("identifier", kAXIdentifierAttribute as String)] {
            if let expected = selector[key]?.stringValue {
                var value: CFTypeRef?
                let status = AXUIElementCopyAttributeValue(element, attribute as CFString, &value)
                if status == .attributeUnsupported || status == .noValue { return false }
                guard status == .success, let actual = value as? String else { return nil }
                if actual != expected { return false }
            }
        }
        return true
    }

    private func ownProcess(_ surfaceID: String) -> Bool {
        surfaceID.split(separator: ":").last.flatMap { Int32($0) } == getpid()
    }

    public func readAttribute(surfaceId: String, elementId: String, attribute: String) throws -> JSONValue? {
        if ownProcess(surfaceId) && !Thread.isMainThread { return try DispatchQueue.main.sync { try readAttribute(surfaceId: surfaceId, elementId: elementId, attribute: attribute) } }
        lock.lock(); defer { lock.unlock() }
        try requireAuthorization()
        guard policy.allowedWritableAttributes.contains(attribute) else { throw RuntimeError.denied("attribute readback is not allowlisted") }
        return jsonAttribute(try resolve(surfaceId: surfaceId, elementId: elementId).element, attribute)
    }

    private func jsonAttribute(_ element: AXUIElement, _ name: String) -> JSONValue? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success, let value else { return nil }
        if CFGetTypeID(value) == CFBooleanGetTypeID(), let n = value as? NSNumber { return .bool(n.boolValue) }
        if let text = value as? String, text.utf8.count <= 16_384 { return .string(text) }
        if let n = value as? NSNumber, n.doubleValue.isFinite { return .number(n.doubleValue) }
        if let date = value as? Date { return .string(nativeTimestamp(date)) }
        guard CFGetTypeID(value) == AXValueGetTypeID() else { return nil }
        let native = value as! AXValue
        switch AXValueGetType(native) {
        case .cgPoint:
            var point = CGPoint.zero
            if AXValueGetValue(native, .cgPoint, &point), point.x.isFinite, point.y.isFinite { return .object(["x": .number(point.x), "y": .number(point.y)]) }
        case .cgSize:
            var size = CGSize.zero
            if AXValueGetValue(native, .cgSize, &size), size.width.isFinite, size.height.isFinite { return .object(["width": .number(size.width), "height": .number(size.height)]) }
        case .cfRange:
            var range = CFRange(location: 0, length: 0)
            if AXValueGetValue(native, .cfRange, &range), range.location >= 0, range.length >= 0 { return .object(["location": .number(Double(range.location)), "length": .number(Double(range.length))]) }
        default: break
        }
        return nil
    }

    private func nativeAttributes(_ element: AXUIElement) -> [String: JSONValue] {
        var result: [String: JSONValue] = [:]
        for name in [kAXValueAttribute, kAXEnabledAttribute, kAXFocusedAttribute, kAXSelectedAttribute, kAXPositionAttribute, kAXSizeAttribute, kAXSelectedTextRangeAttribute] {
            if let value = jsonAttribute(element, name as String) { result[name as String] = value }
        }
        return result
    }

    private func nativeBounds(_ element: AXUIElement) -> NativeBounds? {
        var position: CFTypeRef?; var size: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXPositionAttribute as CFString, &position) == .success,
              AXUIElementCopyAttributeValue(element, kAXSizeAttribute as CFString, &size) == .success,
              let position, let size, CFGetTypeID(position) == AXValueGetTypeID(), CFGetTypeID(size) == AXValueGetTypeID() else { return nil }
        var point = CGPoint.zero; var dimensions = CGSize.zero
        guard AXValueGetValue(position as! AXValue, .cgPoint, &point), AXValueGetValue(size as! AXValue, .cgSize, &dimensions) else { return nil }
        return NativeBounds(x: point.x, y: point.y, width: dimensions.width, height: dimensions.height)
    }

    private func stringAttribute(_ element: AXUIElement, _ name: String) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
        return value as? String
    }

    private func elementArrayAttribute(_ element: AXUIElement, _ name: String) -> [AXUIElement] {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success,
              let result = value as? [AXUIElement] else { return [] }
        return result
    }

    private func actionNames(_ element: AXUIElement) -> [String] {
        var value: CFArray?
        guard AXUIElementCopyActionNames(element, &value) == .success,
              let result = value as? [String] else { return [] }
        return result.sorted()
    }
}

private func surfaceID(bundleID: String, processID: pid_t) -> String {
    "macos:\(bundleID):\(processID)"
}

private func numberValue(_ value: JSONValue) -> Double? {
    guard case .number(let decoded) = value else { return nil }
    return decoded
}

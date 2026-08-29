import Foundation
import XCTest
@testable import CymonkeyMacOSRuntime

final class RuntimeTests: XCTestCase {
    func testHelloAndCapabilitiesExposeComputerDomain() async throws {
        let runtime = try makeRuntime(accessibilityAuthorized: true)
        let hello = await runtime.handle(ControlRequest(id: 1, method: "hello"))
        XCTAssertNil(hello.error)
        XCTAssertEqual(hello.result?.objectValue?["protocolVersion"]?.stringValue, cymonkeyProtocolVersion)
        XCTAssertEqual(hello.result?.objectValue?["domains"], .array([.string("viewer")]))
        XCTAssertEqual(hello.result?.objectValue?["runtimes"], .array([.string("macos-app")]))

        let capabilities = await runtime.capabilities().map(\.name)
        XCTAssertTrue(capabilities.contains("app.command.invoke"))
        XCTAssertTrue(capabilities.contains("ui.query"))
        XCTAssertTrue(capabilities.contains("ui.action.invoke"))
        XCTAssertTrue(capabilities.contains("ui.attribute.set"))
        XCTAssertFalse(capabilities.contains("applescript.execute"))
    }

    func testMissingAccessibilityConsentReducesCapabilities() async throws {
        let runtime = try makeRuntime(accessibilityAuthorized: false)
        let capabilities = await runtime.capabilities().map(\.name)
        XCTAssertEqual(capabilities, ["app.command.describe", "app.command.invoke", "app.command.list"])
    }

    func testTypedAllowlistedAppleEventCommandIsInvoked() async throws {
        let sender = FakeAppleEvents()
        let runtime = try makeRuntime(appleEvents: sender, accessibilityAuthorized: true)
        let response = await runtime.handle(ControlRequest(
            id: 2,
            method: "act",
            params: .object([
                "name": .string("app.command.invoke"),
                "input": .object([
                    "surfaceId": .string("macos:com.example.Target:42"),
                    "command": .string("play"),
                    "volume": .number(0.5),
                ]),
            ])
        ))
        XCTAssertNil(response.error)
        XCTAssertEqual(sender.invocations.count, 1)
        XCTAssertEqual(sender.invocations.first?.0.name, "play")

        let eventReply = await runtime.handle(ControlRequest(id: 3, method: "events"))
        guard case .array(let events)? = eventReply.result?.objectValue?["events"] else {
            return XCTFail("missing semantic events")
        }
        XCTAssertEqual(events.count, 1)
    }

    func testUnknownCommandIsDeniedBeforeNativeDispatch() async throws {
        let sender = FakeAppleEvents()
        let runtime = try makeRuntime(appleEvents: sender, accessibilityAuthorized: true)
        let response = await runtime.handle(ControlRequest(
            id: 4,
            method: "act",
            params: .object([
                "name": .string("app.command.invoke"),
                "input": .object([
                    "surfaceId": .string("macos:com.example.Target:42"),
                    "command": .string("do-script"),
                ]),
            ])
        ))
        XCTAssertEqual(response.error?.code, "denied")
        XCTAssertTrue(sender.invocations.isEmpty)
    }

    func testPlaintextRemoteControlEndpointIsRejected() throws {
        XCTAssertThrowsError(try ControlEndpoint(
            url: XCTUnwrap(URL(string: "ws://example.com/control")),
            bearerToken: "secret"
        ))
        XCTAssertNoThrow(try ControlEndpoint(
            url: XCTUnwrap(URL(string: "ws://127.0.0.1:7394/control")),
            bearerToken: "secret"
        ))
        XCTAssertNoThrow(try ControlEndpoint(
            url: XCTUnwrap(URL(string: "wss://native.example/control")),
            bearerToken: "secret"
        ))
    }

    func testConfigurationRejectsUnallowlistedTargetAndRawStyleCommandName() throws {
        XCTAssertThrowsError(try HelperConfiguration(
            allowedBundleIds: ["com.example.Allowed"],
            appleEventCommands: [AppleEventCommand(
                name: "play", bundleId: "com.example.Denied", eventClass: "hook", eventId: "Play"
            )]
        ))
        XCTAssertThrowsError(try HelperConfiguration(
            allowedBundleIds: ["com.example.Allowed"],
            appleEventCommands: [AppleEventCommand(
                name: "do script", bundleId: "com.example.Allowed", eventClass: "hook", eventId: "Play"
            )]
        ))
    }

    func testViewerCapabilitiesRequireTheirOwnConsent() async throws {
        let runtime = try makeRuntime(
            accessibilityAuthorized: false,
            viewer: FakeViewer(captureAuthorized: true, inputAuthorized: false)
        )
        let names = await runtime.capabilities().map(\.name)
        XCTAssertTrue(names.contains("display.capture"))
        XCTAssertFalse(names.contains("pointer.click"))
        let response = await runtime.handle(ControlRequest(
            id: 9,
            method: "act",
            params: .object(["name": .string("display.capture"), "input": .object(["surfaceId": .string("macos-viewer:42-7")])])
        ))
        XCTAssertNil(response.error)
    }

    private func makeRuntime(
        appleEvents: AppleEventSending = FakeAppleEvents(),
        accessibilityAuthorized: Bool,
        viewer: ViewerProviding? = nil
    ) throws -> CymonkeyRuntime {
        let accessibilityPolicy = AccessibilityPolicy(
            allowedBundleIds: ["com.example.Target"],
            allowedWritableAttributes: ["AXValue"]
        )
        let configuration = try HelperConfiguration(
            allowedBundleIds: ["com.example.Target"],
            appleEventCommands: [AppleEventCommand(
                name: "play",
                bundleId: "com.example.Target",
                eventClass: "hook",
                eventId: "Play",
                parameters: [AppleEventParameter(input: "volume", keyword: "pVol", type: .number)]
            )],
            accessibility: accessibilityPolicy
        )
        return try CymonkeyRuntime(
            configuration: configuration,
            appleEvents: appleEvents,
            accessibility: FakeAccessibility(authorized: accessibilityAuthorized),
            viewer: viewer
        )
    }
}

private final class FakeViewer: ViewerProviding, @unchecked Sendable {
    let captureAuthorized: Bool
    let inputAuthorized: Bool

    init(captureAuthorized: Bool, inputAuthorized: Bool) {
        self.captureAuthorized = captureAuthorized
        self.inputAuthorized = inputAuthorized
    }

    func surfaces() -> [ViewerSurface] {
        [ViewerSurface(id: "macos-viewer:42-7", processId: 42, windowId: 7, label: "Target", x: 0, y: 0, width: 20, height: 20)]
    }

    func capture(surfaceId: String) throws -> ViewerCapture { ViewerCapture(format: "image/png", width: 1, height: 1, base64Data: "AA==") }
    func move(surfaceId: String, x: Int, y: Int) throws {}
    func click(surfaceId: String, x: Int, y: Int, button: String) throws {}
    func drag(surfaceId: String, startX: Int, startY: Int, endX: Int, endY: Int) throws {}
    func scroll(surfaceId: String, x: Int, y: Int, deltaY: Int) throws {}
    func type(surfaceId: String, text: String) throws {}
    func press(surfaceId: String, key: String) throws {}
}

private final class FakeAppleEvents: AppleEventSending, @unchecked Sendable {
    private(set) var invocations: [(AppleEventCommand, [String: JSONValue])] = []

    func invoke(command: AppleEventCommand, input: [String: JSONValue]) throws -> JSONValue {
        invocations.append((command, input))
        return .object(["ok": .bool(true)])
    }
}

private final class FakeAccessibility: AccessibilityProviding, @unchecked Sendable {
    let authorized: Bool
    init(authorized: Bool) { self.authorized = authorized }

    func surfaces() -> [AccessibilitySurface] {
        [AccessibilitySurface(id: "macos:com.example.Target:42", bundleId: "com.example.Target", processId: 42, label: "Target")]
    }

    func query(surfaceId: String, selector: [String: JSONValue]) throws -> [AccessibilityElementDescription] {
        [AccessibilityElementDescription(
            id: "element-1", surfaceId: surfaceId, role: "AXButton", title: "Play",
            identifier: "play", actions: ["AXPress"], settableAttributes: ["AXValue"]
        )]
    }

    func perform(surfaceId: String, elementId: String, action: String) throws {}

    func setAttribute(surfaceId: String, elementId: String, attribute: String, value: JSONValue) throws {}
}

import Foundation
import XCTest
@testable import CymonkeyIOSHelper

@MainActor
final class FakeSurface: BrowserSurface {
    var currentURL: URL?
    var pageTitle: String? = "Example"
    var executed: [String] = []
    var storedCookies: [HTTPCookie] = []

    init(url: String) { currentURL = URL(string: url) }
    func evaluateApprovedScript(_ source: String) async throws -> Bool { executed.append(source); return true }
    func allCookies() async -> [HTTPCookie] { storedCookies }
}

final class RuntimeTests: XCTestCase {
    func testRegisteredScriptOnlyRunsOnApprovedOrigin() async throws {
        let runtime = await IOSWebViewRuntime()
        let surface = await FakeSurface(url: "https://example.com/page")
        try await runtime.register(surface: surface, id: "tab")
        try await runtime.registerApprovedScript(
            id: "approved", source: "window.test = true",
            allowedOrigins: [URL(string: "https://example.com")!]
        )
        let params: JSONValue = .object([
            "name": .string("userscript.apply"),
            "input": .object(["surfaceId": .string("tab"), "scriptId": .string("approved")]),
        ])
        let allowed = await runtime.handle(ControlRequest(id: 1, method: "act", params: params))
        XCTAssertNil(allowed.error)
        let executed = await surface.executed
        XCTAssertEqual(executed.count, 1)
        XCTAssertTrue(executed[0].contains("location.origin !== \"https://example.com\""))
        XCTAssertTrue(executed[0].contains("window.test = true"))

        await surface.setURL(URL(string: "https://other.example/page")!)
        let denied = await runtime.handle(ControlRequest(id: 2, method: "act", params: params))
        XCTAssertEqual(denied.error?.code, "denied")
        let afterDenied = await surface.executed
        XCTAssertEqual(afterDenied.count, 1)
    }

    func testCookieReadNeedsHostAuthorizationAndNamedScope() async throws {
        let runtime = await IOSWebViewRuntime(authorizeCookieRead: { surfaceID, _, names in
            surfaceID == "tab" && names == ["session"]
        })
        let surface = await FakeSurface(url: "https://example.com/path")
        await surface.setCookies([
            HTTPCookie(properties: [.domain: "example.com", .path: "/", .name: "session", .value: "abc"])!,
            HTTPCookie(properties: [.domain: "other.example", .path: "/", .name: "session", .value: "wrong"])!,
        ])
        try await runtime.register(surface: surface, id: "tab")
        let params: JSONValue = .object([
            "name": .string("cookie.read"),
            "input": .object(["surfaceId": .string("tab"), "names": .array([.string("session")])]),
        ])
        let result = await runtime.handle(ControlRequest(id: 1, method: "act", params: params))
        XCTAssertNil(result.error)
        XCTAssertEqual(result.result?.object?["cookies"], .array([
            .object(["name": .string("session"), "value": .string("abc")]),
        ]))
    }

    func testScriptRegistrationRejectsPathThatWouldWidenScope() async {
        let runtime = await IOSWebViewRuntime()
        do {
            try await runtime.registerApprovedScript(
                id: "too-broad", source: "window.test = true",
                allowedOrigins: [URL(string: "https://example.com/private")!]
            )
            XCTFail("path-scoped input must not become an origin-wide approval")
        } catch let error as ControlError {
            XCTAssertEqual(error.code, "invalid_request")
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }
}

@MainActor
private extension FakeSurface {
    func setURL(_ url: URL) { currentURL = url }
    func setCookies(_ cookies: [HTTPCookie]) { storedCookies = cookies }
}

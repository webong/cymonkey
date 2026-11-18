import Foundation
import WebKit

@MainActor
public protocol BrowserSurface: AnyObject {
    var currentURL: URL? { get }
    var pageTitle: String? { get }
    func evaluateApprovedScript(_ source: String) async throws -> Bool
    func allCookies() async -> [HTTPCookie]
}

@MainActor
public final class WKBrowserSurface: BrowserSurface {
    private weak var webView: WKWebView?

    public init(_ webView: WKWebView) { self.webView = webView }
    public var currentURL: URL? { webView?.url }
    public var pageTitle: String? { webView?.title }

    public func evaluateApprovedScript(_ source: String) async throws -> Bool {
        guard let webView else { throw ControlError(code: "stale_reference", message: "web view is gone") }
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Bool, Error>) in
            webView.evaluateJavaScript(source) { result, error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: result as? Bool == true) }
            }
        }
    }

    public func allCookies() async -> [HTTPCookie] {
        guard let webView else { return [] }
        return await withCheckedContinuation { continuation in
            webView.configuration.websiteDataStore.httpCookieStore.getAllCookies {
                continuation.resume(returning: $0)
            }
        }
    }
}

/// A caller-owned, in-process binding. The host authenticates and transports requests.
@MainActor
public final class IOSWebViewRuntime {
    public typealias CookieAuthorization = @MainActor (String, URL, [String]) async -> Bool

    private struct ApprovedScript {
        let source: String
        let origins: Set<String>
    }

    private var surfaces: [String: BrowserSurface] = [:]
    private var scripts: [String: ApprovedScript] = [:]
    private var eventLog: [JSONValue] = []
    private var sequence: UInt64 = 0
    private let authorizeCookieRead: CookieAuthorization?

    public init(authorizeCookieRead: CookieAuthorization? = nil) {
        self.authorizeCookieRead = authorizeCookieRead
    }

    public func register(surface: BrowserSurface, id: String) throws {
        guard !id.isEmpty, surfaces[id] == nil else {
            throw ControlError(code: "invalid_request", message: "surface ID is empty or already registered")
        }
        surfaces[id] = surface
    }

    public func unregister(surfaceID: String) { surfaces.removeValue(forKey: surfaceID) }

    /// Only the trusted host can provide source. Remote requests refer to a registered ID.
    public func registerApprovedScript(id: String, source: String, allowedOrigins: [URL]) throws {
        let origins = Set(allowedOrigins.compactMap(Self.approvedOrigin))
        guard !id.isEmpty, scripts[id] == nil, !source.isEmpty, !origins.isEmpty,
              origins.count == allowedOrigins.count else {
            throw ControlError(code: "invalid_request", message: "script ID must be new and have source and valid HTTPS origins")
        }
        scripts[id] = ApprovedScript(source: source, origins: origins)
    }

    public func removeApprovedScript(id: String) { scripts.removeValue(forKey: id) }

    /// Call from the host's WKNavigationDelegate after navigation completes.
    public func navigationDidFinish(surfaceID: String) async {
        for id in scripts.keys.sorted() {
            _ = try? await apply(scriptID: id, surfaceID: surfaceID)
        }
    }

    public func handle(_ request: ControlRequest) async -> ControlResponse {
        do {
            let result: JSONValue
            switch request.method {
            case "hello": result = hello()
            case "capabilities": result = .array(capabilities())
            case "describe": result = describe()
            case "act": result = try await act(request.params)
            case "events": result = try events(request.params)
            default: throw ControlError(code: "invalid_request", message: "unsupported Cymonkey method")
            }
            return ControlResponse(id: request.id, result: result)
        } catch let error as ControlError {
            return ControlResponse(id: request.id, error: error)
        } catch {
            return ControlResponse(id: request.id, error: ControlError(code: "native_failure", message: "web view operation failed"))
        }
    }

    private func hello() -> JSONValue {
        .object([
            "protocolVersion": .string(cymonkeyMobileProtocolVersion),
            "implementation": .object(["name": .string("cymonkey-ios-helper"), "version": .string("0.1.0")]),
            "domains": .array([.string("render")]),
            "runtimes": .array([.string("browser-dom")]),
            "drivers": .array([.string("ios-wkwebview")]),
            "features": .array([.string("events.cursor"), .string("host.reviewed-scripts")]),
        ])
    }

    private func capabilities() -> [JSONValue] {
        var result = [capability("userscript.apply", effect: "write", required: ["surfaceId", "scriptId"])]
        if authorizeCookieRead != nil {
            result.append(capability("cookie.read", effect: "read", required: ["surfaceId", "names"]))
        }
        return result
    }

    private func capability(_ name: String, effect: String, required: [String]) -> JSONValue {
        .object([
            "name": .string(name), "description": .string("Host-approved operation on a registered WKWebView."),
            "domain": .string("render"), "runtime": .string("browser-dom"),
            "driver": .string("ios-wkwebview"), "support": .string("mapped"),
            "lifetime": .string("attachment"), "persistence": .string("session"),
            "effect": .string(effect),
            "inputSchema": .object([
                "type": .string("object"), "required": .array(required.map(JSONValue.string)),
                "additionalProperties": .bool(false),
            ]),
        ])
    }

    private func describe() -> JSONValue {
        let items = surfaces.keys.sorted().map { id -> JSONValue in
            let surface = surfaces[id]!
            return .object([
                "id": .string(id), "domain": .string("render"), "runtime": .string("browser-dom"),
                "kind": .string("webview"), "label": surface.pageTitle.map(JSONValue.string) ?? .null,
                "properties": .object(["url": surface.currentURL.map { .string($0.absoluteString) } ?? .null]),
            ])
        }
        return .object([
            "revision": .string(surfaces.keys.sorted().joined(separator: "|")),
            "surfaces": .array(items), "augmentations": .array([]),
        ])
    }

    private func act(_ params: JSONValue) async throws -> JSONValue {
        guard let action = params.object,
              let name = action["name"]?.string,
              let input = action["input"]?.object,
              let surfaceID = input["surfaceId"]?.string else {
            throw ControlError(code: "invalid_request", message: "action requires name, input, and surfaceId")
        }
        switch name {
        case "userscript.apply":
            guard let scriptID = input["scriptId"]?.string else {
                throw ControlError(code: "invalid_request", message: "scriptId is required")
            }
            try await apply(scriptID: scriptID, surfaceID: surfaceID)
            return .object(["applied": .bool(true)])
        case "cookie.read":
            guard let authorizeCookieRead else {
                throw ControlError(code: "denied", message: "cookie reading is unavailable")
            }
            guard let names = input["names"]?.strings, !names.isEmpty,
                  names.allSatisfy({ !$0.isEmpty }) else {
                throw ControlError(code: "invalid_request", message: "explicit cookie names are required")
            }
            guard let surface = surfaces[surfaceID], let url = surface.currentURL,
                  Self.origin(url) != nil else {
                throw ControlError(code: "stale_reference", message: "registered web view has no HTTPS page")
            }
            guard await authorizeCookieRead(surfaceID, url, names) else {
                throw ControlError(code: "denied", message: "cookie read was not approved")
            }
            guard surface.currentURL == url else {
                throw ControlError(code: "stale_reference", message: "web view navigated during cookie approval")
            }
            let selected = Set(names)
            let cookies = await surface.allCookies().filter {
                selected.contains($0.name) && Self.matches($0, url: url)
            }
            guard surface.currentURL == url else {
                throw ControlError(code: "stale_reference", message: "web view navigated during cookie read")
            }
            emit("cookie.read", surfaceID: surfaceID, scriptID: nil)
            return .object(["cookies": .array(cookies.map {
                .object(["name": .string($0.name), "value": .string($0.value)])
            })])
        default:
            throw ControlError(code: "denied", message: "capability was not advertised")
        }
    }

    private func apply(scriptID: String, surfaceID: String) async throws {
        guard let surface = surfaces[surfaceID], let url = surface.currentURL,
              let origin = Self.origin(url) else {
            throw ControlError(code: "stale_reference", message: "registered web view has no HTTPS page")
        }
        guard let script = scripts[scriptID], script.origins.contains(origin) else {
            throw ControlError(code: "denied", message: "script is not approved for this page")
        }
        let quotedOrigin = try Self.javascriptString(origin)
        let guarded = "(() => { if (location.origin !== \(quotedOrigin)) return false;\n\(script.source)\nreturn true; })()"
        guard try await surface.evaluateApprovedScript(guarded) else {
            throw ControlError(code: "stale_reference", message: "page changed before script execution")
        }
        emit("userscript.applied", surfaceID: surfaceID, scriptID: scriptID)
    }

    private func events(_ params: JSONValue) throws -> JSONValue {
        let input = params.object ?? [:]
        let after = UInt64(input["after"]?.string ?? "0") ?? 0
        let limit: Int
        if case .number(let number) = input["limit"] { limit = max(1, min(500, Int(number))) }
        else { limit = 100 }
        let selected = Array(eventLog.filter {
            UInt64($0.object?["id"]?.string ?? "0") ?? 0 > after
        }.prefix(limit))
        return .object(["events": .array(selected), "cursor": .string(selected.last?.object?["id"]?.string ?? String(after))])
    }

    private func emit(_ type: String, surfaceID: String, scriptID: String?) {
        sequence += 1
        var data: [String: JSONValue] = ["surfaceId": .string(surfaceID)]
        if let scriptID { data["scriptId"] = .string(scriptID) }
        eventLog.append(.object([
            "id": .string(String(sequence)), "type": .string(type),
            "occurredAt": .string(ISO8601DateFormatter().string(from: Date())),
            "domain": .string("render"), "runtime": .string("browser-dom"),
            "driver": .string("ios-wkwebview"), "data": .object(data),
        ]))
        if eventLog.count > 1_000 { eventLog.removeFirst(eventLog.count - 1_000) }
    }

    private static func origin(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == "https", let host = url.host?.lowercased(), !host.isEmpty else { return nil }
        let authority = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return "https://\(authority)\(url.port.flatMap { $0 == 443 ? nil : ":\($0)" } ?? "")"
    }

    private static func approvedOrigin(_ url: URL) -> String? {
        guard url.user == nil, url.password == nil,
              url.path.isEmpty || url.path == "/",
              url.query == nil, url.fragment == nil else { return nil }
        return origin(url)
    }

    private static func javascriptString(_ value: String) throws -> String {
        let data = try JSONSerialization.data(withJSONObject: [value], options: [.fragmentsAllowed, .withoutEscapingSlashes])
        guard let array = String(data: data, encoding: .utf8), array.count >= 2 else {
            throw ControlError(code: "native_failure", message: "could not encode script origin")
        }
        return String(array.dropFirst().dropLast())
    }

    private static func matches(_ cookie: HTTPCookie, url: URL) -> Bool {
        guard let host = url.host?.lowercased() else { return false }
        let domain = cookie.domain.lowercased()
        let hostMatches = domain.hasPrefix(".")
            ? host == String(domain.dropFirst()) || host.hasSuffix(domain)
            : host == domain
        let path = url.path.isEmpty ? "/" : url.path
        let cookiePath = cookie.path
        let pathMatches = path == cookiePath ||
            (path.hasPrefix(cookiePath) && (cookiePath.hasSuffix("/") || path.dropFirst(cookiePath.count).first == "/"))
        return hostMatches && pathMatches && (!cookie.isSecure || url.scheme == "https")
    }
}

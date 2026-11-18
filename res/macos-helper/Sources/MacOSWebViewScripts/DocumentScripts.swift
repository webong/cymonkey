import Foundation
import WebKit

public enum DocumentScriptTiming: Sendable {
    case documentStart
    case documentEnd

    var native: WKUserScriptInjectionTime {
        switch self {
        case .documentStart: return .atDocumentStart
        case .documentEnd: return .atDocumentEnd
        }
    }
}

public struct InstalledDocumentScript: Equatable, Sendable {
    public let id: String
    public let origins: [String]
    public let timing: DocumentScriptTiming
    public let forMainFrameOnly: Bool
}

public enum DocumentScriptError: Error, Equatable {
    case invalidID
    case duplicateID
    case emptySource
    case invalidOrigin
}

/// Installs reviewed source into a caller-owned WKUserContentController.
/// The controller may be shared by multiple web views; the host controls that scope.
@MainActor
public final class MacOSWebViewScriptInstaller {
    private let controller: WKUserContentController
    private var installed: [String: InstalledDocumentScript] = [:]

    public init(contentController: WKUserContentController) {
        self.controller = contentController
    }

    public var installedScripts: [InstalledDocumentScript] {
        installed.values.sorted { $0.id < $1.id }
    }

    /// Source must be approved by the host before it calls this method.
    /// WKUserContentController has no per-script removal API, so installation
    /// lasts for this controller's lifetime and runs on future navigations.
    @discardableResult
    public func installApprovedScript(
        id: String,
        source: String,
        allowedOrigins: [URL],
        timing: DocumentScriptTiming,
        forMainFrameOnly: Bool = true
    ) throws -> InstalledDocumentScript {
        guard !id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw DocumentScriptError.invalidID
        }
        guard installed[id] == nil else { throw DocumentScriptError.duplicateID }
        guard !source.isEmpty else { throw DocumentScriptError.emptySource }
        let parsed = allowedOrigins.compactMap(Self.approvedOrigin)
        guard !parsed.isEmpty, parsed.count == allowedOrigins.count else {
            throw DocumentScriptError.invalidOrigin
        }
        let origins = Array(Set(parsed)).sorted()
        let checks = try origins.map { origin -> String in
            let encoded = try JSONSerialization.data(withJSONObject: [origin], options: [.withoutEscapingSlashes])
            guard let array = String(data: encoded, encoding: .utf8) else {
                throw DocumentScriptError.invalidOrigin
            }
            return "location.origin !== \(array.dropFirst().dropLast())"
        }
        let guarded = "(() => { if (\(checks.joined(separator: " && "))) return;\n\(source)\n})()"
        let userScript = WKUserScript(
            source: guarded,
            injectionTime: timing.native,
            forMainFrameOnly: forMainFrameOnly,
            in: .page
        )
        controller.addUserScript(userScript)
        let record = InstalledDocumentScript(
            id: id, origins: origins, timing: timing, forMainFrameOnly: forMainFrameOnly
        )
        installed[id] = record
        return record
    }

    private static func approvedOrigin(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == "https",
              let host = url.host?.lowercased(), !host.isEmpty,
              url.user == nil, url.password == nil,
              url.path.isEmpty || url.path == "/",
              url.query == nil, url.fragment == nil else { return nil }
        let authority = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return "https://\(authority)\(url.port.flatMap { $0 == 443 ? nil : ":\($0)" } ?? "")"
    }
}

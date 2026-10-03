import CryptoKit
import Foundation
import SweetCookieKit

/// Browser identifiers accepted by the native macOS cookie reader.
public struct MacOSBrowserName: Hashable, Sendable, RawRepresentable {
    public let rawValue: String

    public init?(rawValue: String) {
        guard Browser(rawValue: rawValue) != nil else { return nil }
        self.rawValue = rawValue
    }

    public static let safari = MacOSBrowserName(rawValue: "safari")!
    public static let chrome = MacOSBrowserName(rawValue: "chrome")!
    public static let firefox = MacOSBrowserName(rawValue: "firefox")!
    public static let edge = MacOSBrowserName(rawValue: "edge")!
    public static let brave = MacOSBrowserName(rawValue: "brave")!
}

/// A discovered profile and cookie store. Pass this value back to `read` to
/// bind a query to the user's selected store.
public struct MacOSCookieStore: Hashable, Sendable {
    public let id: String
    public let browser: MacOSBrowserName
    public let profileName: String
    public let profileID: String
    public let kind: String
    public let label: String
    public let databaseURL: URL?

    let source: BrowserCookieStore
    let explicit: Bool

    init(_ source: BrowserCookieStore, explicit: Bool = false) {
        let identity = [source.browser.rawValue, source.profile.id, source.kind.rawValue,
                        source.databaseURL?.standardizedFileURL.path ?? ""].joined(separator: "\u{0}")
        let digest = SHA256.hash(data: Data(identity.utf8))
        self.id = "sha256:" + digest.map { String(format: "%02x", $0) }.joined()
        self.browser = MacOSBrowserName(rawValue: source.browser.rawValue)!
        self.profileName = source.profile.name
        self.profileID = source.profile.id
        self.kind = source.kind.rawValue
        self.label = source.label
        self.databaseURL = source.databaseURL
        self.source = source
        self.explicit = explicit
    }
}

/// The fields the native reader can preserve from a local cookie store.
/// Browser partition, container, and SameSite metadata are not available from
/// the underlying reader and must not be inferred from this record.
public struct MacOSCookieRecord: Sendable {
    public let name: String
    public let value: String
    public let domain: String
    public let path: String
    public let hostOnly: Bool
    public let expires: Date?
    public let secure: Bool
    public let httpOnly: Bool

    init(_ record: BrowserCookieRecord) {
        self.name = record.name
        self.value = record.value
        self.domain = record.domain
        self.path = record.path
        self.hostOnly = record.scope == .hostOnly
        self.expires = record.expires
        self.secure = record.isSecure
        self.httpOnly = record.isHTTPOnly
    }
}

public enum MacOSCookieReadError: Error, LocalizedError {
    case invalidScope
    case invalidSource
    case storeUnavailable

    public var errorDescription: String? {
        switch self {
        case .invalidScope: "Select at least one exact domain and use nonempty cookie names."
        case .invalidSource: "Select a readable cookie file for the chosen browser."
        case .storeUnavailable: "The selected browser cookie store is no longer available."
        }
    }
}

/// Reusable native macOS cookie access. The host owns approval and delivery;
/// this reader never sends or persists cookie values.
public struct MacOSBrowserCookieReader: Sendable {
    private let client: BrowserCookieClient

    public init(homeDirectories: [URL]? = nil) {
        let configuration = BrowserCookieClient.Configuration(
            homeDirectories: homeDirectories ?? BrowserCookieClient.defaultHomeDirectories())
        self.client = BrowserCookieClient(configuration: configuration)
    }

    public func discover(
        browsers: [MacOSBrowserName] = [.safari, .chrome, .firefox]
    ) -> [MacOSCookieStore] {
        client.stores(in: browsers.compactMap { Browser(rawValue: $0.rawValue) })
            .filter { store in
                guard let url = store.databaseURL else { return false }
                return FileManager.default.isReadableFile(atPath: url.path)
            }
            .map { MacOSCookieStore($0) }
    }

    /// Select a caller-supplied cookie file outside standard discovery roots.
    /// The file must have the browser's expected name and be readable now.
    public func select(browser: MacOSBrowserName, cookieFile: URL) throws -> MacOSCookieStore {
        guard cookieFile.isFileURL, cookieFile.path.hasPrefix("/"),
              FileManager.default.isReadableFile(atPath: cookieFile.path),
              let nativeBrowser = Browser(rawValue: browser.rawValue) else {
            throw MacOSCookieReadError.invalidSource
        }
        let file = cookieFile.standardizedFileURL
        let parent = file.deletingLastPathComponent()
        let profileDirectory: URL
        let kind: BrowserCookieStoreKind
        if browser == .safari {
            guard file.lastPathComponent == "Cookies.binarycookies" else {
                throw MacOSCookieReadError.invalidSource
            }
            profileDirectory = parent
            kind = .safari
        } else if nativeBrowser.usesGeckoProfileStore {
            guard file.lastPathComponent == "cookies.sqlite" else {
                throw MacOSCookieReadError.invalidSource
            }
            profileDirectory = parent
            kind = .primary
        } else {
            guard nativeBrowser.usesChromiumProfileStore, file.lastPathComponent == "Cookies" else {
                throw MacOSCookieReadError.invalidSource
            }
            kind = parent.lastPathComponent == "Network" ? .network : .primary
            profileDirectory = kind == .network ? parent.deletingLastPathComponent() : parent
        }
        let profile = BrowserProfile(id: profileDirectory.path, name: profileDirectory.lastPathComponent)
        let source = BrowserCookieStore(
            browser: nativeBrowser, profile: profile, kind: kind,
            label: "\(nativeBrowser.displayName) \(profile.name)", databaseURL: file)
        return MacOSCookieStore(source, explicit: true)
    }

    /// Reads only the selected store and exact domains. Keychain UI is disabled
    /// by default; an interactive host may opt in after informing the user.
    public func read(
        from store: MacOSCookieStore,
        domains: [String],
        names: [String]? = nil,
        includeExpired: Bool = false,
        allowKeychainUI: Bool = false
    ) throws -> [MacOSCookieRecord] {
        let selectedDomains = Set(domains.map { $0.lowercased().trimmingCharacters(in: .whitespacesAndNewlines) })
        guard !selectedDomains.isEmpty,
              selectedDomains.allSatisfy(Self.validDomain),
              names == nil || !(names?.isEmpty ?? true),
              names?.allSatisfy({ !$0.isEmpty }) ?? true else {
            throw MacOSCookieReadError.invalidScope
        }
        let available = store.explicit
            ? (store.databaseURL.map { FileManager.default.isReadableFile(atPath: $0.path) } ?? false)
            : client.stores(for: store.source.browser).contains(store.source)
        guard available else {
            throw MacOSCookieReadError.storeUnavailable
        }
        let query = BrowserCookieQuery(
            domains: Array(selectedDomains).sorted(), domainMatch: .exact,
            includeExpired: includeExpired)
        let load = { try client.records(matching: query, in: store.source) }
        let records = try allowKeychainUI ? load()
            : BrowserCookieKeychainAccessGate.withUserInteractionDisallowed(load)
        let selectedNames = names.map(Set.init)
        return records.filter { record in
            selectedDomains.contains(record.domain.lowercased())
                && (selectedNames?.contains(record.name) ?? true)
        }.map(MacOSCookieRecord.init)
    }

    private static func validDomain(_ domain: String) -> Bool {
        guard !domain.isEmpty, !domain.hasPrefix("."), !domain.hasSuffix("."),
              !domain.contains("/"), !domain.contains("*"), !domain.contains(" ") else { return false }
        return URL(string: "https://\(domain)/")?.host?.lowercased() == domain
    }
}

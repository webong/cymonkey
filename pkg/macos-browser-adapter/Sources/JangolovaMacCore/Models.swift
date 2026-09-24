import Foundation

public enum HelperMode: String, Codable, CaseIterable, Sendable {
    case external
    case managed
    case auto

    public func resolved(localRuntimeAvailable: Bool) -> HelperMode {
        switch self {
        case .auto: return localRuntimeAvailable ? .managed : .external
        case .external, .managed: return self
        }
    }
}

public enum ManagedRuntimeStatus: Equatable, Sendable {
    case stopped
    case connecting
    case connected
    case failed(String)
}

public struct ManagedRuntimeConfiguration: Sendable {
    public let endpoint: URL
    public let bearerToken: String
    public let protocolVersion: String
    public let helperConfiguration: URL

    public init(environment: [String: String]) throws {
        guard let rawEndpoint = environment["JANGOLOVA_CYMONKEY_CONTROL_URL"],
              let endpoint = URL(string: rawEndpoint),
              let token = environment["JANGOLOVA_CYMONKEY_CONTROL_TOKEN"], !token.isEmpty,
              let protocolVersion = environment["JANGOLOVA_CYMONKEY_PROTOCOL"],
              let path = environment["JANGOLOVA_CYMONKEY_CONFIG"], NSString(string: path).isAbsolutePath else {
            throw MacExtensionError.invalidManagedConfiguration
        }
        self.endpoint = endpoint
        self.bearerToken = token
        self.protocolVersion = protocolVersion
        self.helperConfiguration = URL(fileURLWithPath: path)
    }
}

public struct UserscriptSummary: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let revision: String
    public var enabled: Bool
    public let matches: [String]

    public init(id: String, name: String, revision: String, enabled: Bool, matches: [String]) {
        self.id = id
        self.name = name
        self.revision = revision
        self.enabled = enabled
        self.matches = matches
    }
}

public enum MacExtensionError: Error, Equatable, CustomStringConvertible {
    case invalidManagedConfiguration
    case protocolMismatch
    case userscriptCatalogInvalid

    public var description: String {
        switch self {
        case .invalidManagedConfiguration: return "managed Cymonkey launch configuration is missing or invalid"
        case .protocolMismatch: return "managed Cymonkey protocol is incompatible"
        case .userscriptCatalogInvalid: return "userscript metadata catalog is invalid"
        }
    }
}

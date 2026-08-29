import Foundation

public let cymonkeyProtocolVersion = "jangolova.cymonkey/v1alpha2"

public enum JSONValue: Codable, Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() { self = .null }
        else if let decoded = try? value.decode(Bool.self) { self = .bool(decoded) }
        else if let decoded = try? value.decode(Double.self) { self = .number(decoded) }
        else if let decoded = try? value.decode(String.self) { self = .string(decoded) }
        else if let decoded = try? value.decode([JSONValue].self) { self = .array(decoded) }
        else { self = .object(try value.decode([String: JSONValue].self)) }
    }

    public func encode(to encoder: Encoder) throws {
        var value = encoder.singleValueContainer()
        switch self {
        case .null: try value.encodeNil()
        case .bool(let decoded): try value.encode(decoded)
        case .number(let decoded): try value.encode(decoded)
        case .string(let decoded): try value.encode(decoded)
        case .array(let decoded): try value.encode(decoded)
        case .object(let decoded): try value.encode(decoded)
        }
    }

    public var objectValue: [String: JSONValue]? {
        guard case .object(let value) = self else { return nil }
        return value
    }

    public var stringValue: String? {
        guard case .string(let value) = self else { return nil }
        return value
    }
}

public struct ControlRequest: Codable, Sendable {
    public let id: UInt64
    public let method: String
    public let params: JSONValue

    public init(id: UInt64, method: String, params: JSONValue = .object([:])) {
        self.id = id
        self.method = method
        self.params = params
    }
}

public struct ControlError: Codable, Error, Equatable, Sendable {
    public let code: String
    public let message: String

    public init(code: String, message: String) {
        self.code = code
        self.message = message
    }
}

public struct ControlResponse: Codable, Sendable {
    public let id: UInt64
    public let result: JSONValue?
    public let error: ControlError?

    public init(id: UInt64, result: JSONValue) {
        self.id = id
        self.result = result
        self.error = nil
    }

    public init(id: UInt64, error: ControlError) {
        self.id = id
        self.result = nil
        self.error = error
    }
}

public struct Capability: Codable, Equatable, Sendable {
    public let name: String
    public let description: String
    public let domain: String
    public let runtime: String
    public let driver: String
    public let support: String
    public let lifetime: String
    public let persistence: String
    public let effect: String
    public let inputSchema: JSONValue

    public init(
        name: String,
        description: String,
        driver: String,
        effect: String,
        required: [String],
        additionalProperties: Bool = false,
        runtime: String = "macos-app"
    ) {
        self.name = name
        self.description = description
        self.domain = "viewer"
        self.runtime = runtime
        self.driver = driver
        self.support = "mapped"
        self.lifetime = "attachment"
        self.persistence = "session"
        self.effect = effect
        self.inputSchema = .object([
            "type": .string("object"),
            "required": .array(required.map(JSONValue.string)),
            "additionalProperties": .bool(additionalProperties),
        ])
    }
}

public struct SemanticEvent: Codable, Equatable, Sendable {
    public let id: String
    public let type: String
    public let occurredAt: String
    public let domain: String
    public let runtime: String
    public let driver: String
    public let data: JSONValue
}

public struct ActionRequest: Codable, Sendable {
    public let name: String
    public let input: JSONValue
}

public enum RuntimeError: Error, Equatable, CustomStringConvertible {
    case invalidRequest(String)
    case denied(String)
    case unavailable(String)
    case staleReference(String)
    case nativeFailure(String)

    public var description: String {
        switch self {
        case .invalidRequest(let message), .denied(let message),
             .unavailable(let message), .staleReference(let message),
             .nativeFailure(let message):
            return message
        }
    }

    public var code: String {
        switch self {
        case .invalidRequest: return "invalid_request"
        case .denied: return "denied"
        case .unavailable: return "unavailable"
        case .staleReference: return "stale_reference"
        case .nativeFailure: return "native_failure"
        }
    }
}

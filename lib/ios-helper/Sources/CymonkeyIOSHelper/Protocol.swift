import Foundation

public let cymonkeyMobileProtocolVersion = "cymonkey/v1alpha1"

public enum JSONValue: Codable, Equatable, Sendable {
    case null, bool(Bool), number(Double), string(String), array([JSONValue]), object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null }
        else if let value = try? container.decode(Bool.self) { self = .bool(value) }
        else if let value = try? container.decode(Double.self) { self = .number(value) }
        else if let value = try? container.decode(String.self) { self = .string(value) }
        else if let value = try? container.decode([JSONValue].self) { self = .array(value) }
        else { self = .object(try container.decode([String: JSONValue].self)) }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null: try container.encodeNil()
        case .bool(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .string(let value): try container.encode(value)
        case .array(let value): try container.encode(value)
        case .object(let value): try container.encode(value)
        }
    }

    var object: [String: JSONValue]? {
        guard case .object(let value) = self else { return nil }
        return value
    }

    var string: String? {
        guard case .string(let value) = self else { return nil }
        return value
    }

    var strings: [String]? {
        guard case .array(let values) = self else { return nil }
        return values.compactMap(\.string).count == values.count ? values.compactMap(\.string) : nil
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

public struct ControlError: Codable, Error, Sendable {
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
        self.id = id; self.result = result; self.error = nil
    }

    public init(id: UInt64, error: ControlError) {
        self.id = id; self.result = nil; self.error = error
    }
}

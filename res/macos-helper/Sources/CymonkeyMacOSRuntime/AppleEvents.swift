import ExtSystem
import Foundation

public protocol AppleEventSending: Sendable {
    func invoke(command: AppleEventCommand, input: [String: JSONValue]) throws -> JSONValue
}

/// Maps Cymonkey's reviewed inputs into ext's typed native parameters.
func mappedAppleEventParameters(
    command: AppleEventCommand,
    input: [String: JSONValue]
) throws -> [ExtSystem.AppleEventParameter] {
    let knownInputs = Set(command.parameters.map(\.input))
    guard Set(input.keys).subtracting(["surfaceId", "command"]).isSubset(of: knownInputs) else {
        throw RuntimeError.invalidRequest("Apple Event input contains an unmapped parameter")
    }
    return try command.parameters.compactMap { parameter in
        guard let value = input[parameter.input] else {
            if parameter.required {
                throw RuntimeError.invalidRequest("required Apple Event parameter is missing")
            }
            return nil
        }
        let mapped: AppleEventValue
        switch (parameter.type, value) {
        case (.string, .string(let decoded)):
            mapped = .string(decoded)
        case (.boolean, .bool(let decoded)):
            mapped = .boolean(decoded)
        case (.integer, .number(let decoded))
            where decoded.isFinite && decoded.rounded() == decoded &&
                  decoded >= Double(Int32.min) && decoded <= Double(Int32.max):
            mapped = .integer(Int32(decoded))
        case (.number, .number(let decoded)) where decoded.isFinite:
            mapped = .number(decoded)
        default:
            throw RuntimeError.invalidRequest("Apple Event parameter has the wrong type")
        }
        return ExtSystem.AppleEventParameter(keyword: parameter.keyword, value: mapped)
    }
}

public final class SystemAppleEventSender: AppleEventSending, @unchecked Sendable {
    public init() {}

    public func invoke(command: AppleEventCommand, input: [String: JSONValue]) throws -> JSONValue {
        let parameters = try mappedAppleEventParameters(command: command, input: input)
        do {
            let result = try SystemAppleEvent.send(
                bundleIdentifier: command.bundleId,
                eventClass: command.eventClass,
                eventID: command.eventId,
                parameters: parameters
            )
            switch result {
            case .string(let value): return .string(value)
            case .boolean(let value): return .bool(value)
            case .integer(let value): return .number(Double(value))
            case .number(let value): return .number(value)
            case .none: return .null
            }
        } catch let error as SystemAppleEventError {
            switch error {
            case .invalidInput: throw RuntimeError.invalidRequest("Apple Event input is invalid")
            case .targetUnavailable: throw RuntimeError.unavailable("target application is not running")
            case .nativeFailure: throw RuntimeError.nativeFailure("Apple Event was rejected or failed")
            }
        }
    }
}

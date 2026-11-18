import AppKit
import Foundation

public protocol AppleEventSending: Sendable {
    func invoke(command: AppleEventCommand, input: [String: JSONValue]) throws -> JSONValue
}

public final class SystemAppleEventSender: AppleEventSending, @unchecked Sendable {
    public init() {}

    public func invoke(command: AppleEventCommand, input: [String: JSONValue]) throws -> JSONValue {
        guard !NSRunningApplication.runningApplications(withBundleIdentifier: command.bundleId).isEmpty else {
            throw RuntimeError.unavailable("target application is not running")
        }
        let target = NSAppleEventDescriptor(bundleIdentifier: command.bundleId)
        let event = NSAppleEventDescriptor(
            eventClass: try fourCharacterCode(command.eventClass),
            eventID: try fourCharacterCode(command.eventId),
            targetDescriptor: target,
            returnID: AEReturnID(kAutoGenerateReturnID),
            transactionID: AETransactionID(kAnyTransactionID)
        )
        let knownInputs = Set(command.parameters.map(\.input))
        let suppliedInputs = Set(input.keys).subtracting(["surfaceId", "command"])
        guard suppliedInputs.isSubset(of: knownInputs) else {
            throw RuntimeError.invalidRequest("Apple Event input contains an unmapped parameter")
        }
        for parameter in command.parameters {
            guard let value = input[parameter.input] else {
                if parameter.required {
                    throw RuntimeError.invalidRequest("required Apple Event parameter is missing")
                }
                continue
            }
            event.setParam(
                try descriptor(for: value, type: parameter.type),
                forKeyword: try fourCharacterCode(parameter.keyword)
            )
        }
        do {
            let reply = try event.sendEvent(
                options: [.waitForReply, .canInteract],
                timeout: 30
            )
            return Self.jsonValue(from: reply.paramDescriptor(forKeyword: keyDirectObject))
        } catch {
            throw RuntimeError.nativeFailure("Apple Event was rejected or failed")
        }
    }

    private func descriptor(for value: JSONValue, type: AppleEventParameter.ValueType) throws -> NSAppleEventDescriptor {
        switch (type, value) {
        case (.string, .string(let decoded)):
            return NSAppleEventDescriptor(string: decoded)
        case (.boolean, .bool(let decoded)):
            return NSAppleEventDescriptor(boolean: decoded)
        case (.integer, .number(let decoded)) where decoded.rounded() == decoded:
            return NSAppleEventDescriptor(int32: Int32(decoded))
        case (.number, .number(let decoded)):
            return NSAppleEventDescriptor(double: decoded)
        default:
            throw RuntimeError.invalidRequest("Apple Event parameter has the wrong type")
        }
    }

    private static func jsonValue(from descriptor: NSAppleEventDescriptor?) -> JSONValue {
        guard let descriptor else { return .null }
        switch descriptor.descriptorType {
        case typeBoolean:
            return .bool(descriptor.booleanValue)
        case typeSInt16, typeSInt32, typeSInt64, typeUInt32:
            return .number(Double(descriptor.int32Value))
        case typeIEEE32BitFloatingPoint, typeIEEE64BitFloatingPoint:
            return .number(descriptor.doubleValue)
        default:
            if let value = descriptor.stringValue { return .string(value) }
            return .null
        }
    }
}

func fourCharacterCode(_ value: String) throws -> OSType {
    let bytes = Array(value.utf8)
    guard bytes.count == 4, bytes.allSatisfy({ $0 < 128 }) else {
        throw RuntimeError.invalidRequest("four-character code must contain four ASCII bytes")
    }
    return bytes.reduce(OSType(0)) { ($0 << 8) | OSType($1) }
}

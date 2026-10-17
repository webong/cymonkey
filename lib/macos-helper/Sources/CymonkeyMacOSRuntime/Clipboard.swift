import AppKit
import Foundation

public struct ClipboardPolicy: Codable, Equatable, Sendable {
    public let maxBytes: Int
    public let allowedFormats: [String]
    public let allowRead: Bool
    public let allowWrite: Bool

    public init(maxBytes: Int, allowedFormats: [String], allowRead: Bool = false, allowWrite: Bool = false) {
        self.maxBytes = maxBytes; self.allowedFormats = allowedFormats
        self.allowRead = allowRead; self.allowWrite = allowWrite
    }
    public func validate() throws {
        guard maxBytes > 0, maxBytes <= 2 * 1024 * 1024, !allowedFormats.isEmpty,
              allowedFormats.count <= 32, allowedFormats.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 256 }) else {
            throw RuntimeError.invalidRequest("clipboard needs a byte ceiling and explicit native formats")
        }
    }
}

public struct ClipboardRepresentation: Codable, Equatable, Sendable {
    public let format: String
    public let data: Data
}
public struct ClipboardItem: Codable, Equatable, Sendable {
    public let representations: [ClipboardRepresentation]
}
public struct ClipboardSnapshot: Codable, Equatable, Sendable {
    public let generation: String
    public let items: [ClipboardItem]
}
public struct ClipboardReceipt: Codable, Equatable, Sendable {
    public let generation: String
    public let dispatch: String
}

// Caller-owned runtime binding. All reads and mutations are serialized on the
// main queue. A timed-out transport does not cancel an already dispatched write.
public final class SystemClipboardProvider: @unchecked Sendable {
    private let policy: ClipboardPolicy
    private let producerID = UUID().uuidString.lowercased()
    public init(policy: ClipboardPolicy) throws { try policy.validate(); self.policy = policy }
    private func generation(_ board: NSPasteboard) -> String { "\(producerID):\(board.changeCount)" }
    private func onMain<T>(_ work: () throws -> T) rethrows -> T {
        if Thread.isMainThread { return try work() }
        return try DispatchQueue.main.sync(execute: work)
    }
    public func read(maxBytes: Int) throws -> ClipboardSnapshot {
        guard policy.allowRead, maxBytes > 0, maxBytes <= policy.maxBytes else { throw RuntimeError.denied("clipboard read exceeds policy") }
        return try onMain {
            let board = NSPasteboard.general
            let before = generation(board)
            let nativeItems = board.pasteboardItems ?? []
            guard nativeItems.count <= 64 else { throw RuntimeError.denied("clipboard item limit") }
            var count = 0
            var items: [ClipboardItem] = []
            for item in nativeItems {
                guard item.types.count <= 32 else { throw RuntimeError.denied("clipboard format limit") }
                var values: [ClipboardRepresentation] = []
                for type in item.types {
                    guard policy.allowedFormats.contains(type.rawValue) else { throw RuntimeError.denied("clipboard contains an ungranted format; snapshot would be incomplete") }
                    guard let data = item.data(forType: type) else { throw RuntimeError.nativeFailure("clipboard representation unavailable") }
                    count += data.count
                    guard count <= maxBytes else { throw RuntimeError.denied("clipboard exceeds byte ceiling") }
                    values.append(ClipboardRepresentation(format: type.rawValue, data: data))
                }
                items.append(ClipboardItem(representations: values))
            }
            guard generation(board) == before else { throw RuntimeError.staleReference("clipboard changed during read") }
            return ClipboardSnapshot(generation: before, items: items)
        }
    }
    public func write(expectedGeneration: String, items: [ClipboardItem], maxBytes: Int) throws -> ClipboardReceipt {
        guard policy.allowWrite, maxBytes > 0, maxBytes <= policy.maxBytes, items.count <= 64 else { throw RuntimeError.denied("clipboard write exceeds policy") }
        return try onMain {
        var count = 0
        var native: [NSPasteboardItem] = []
        for item in items {
            guard !item.representations.isEmpty, item.representations.count <= 32 else { throw RuntimeError.invalidRequest("invalid clipboard item") }
            let output = NSPasteboardItem()
            var formats = Set<String>()
            for value in item.representations {
                count += value.data.count
                guard count <= maxBytes, policy.allowedFormats.contains(value.format), formats.insert(value.format).inserted else { throw RuntimeError.denied("clipboard format or byte ceiling") }
                guard output.setData(value.data, forType: NSPasteboard.PasteboardType(value.format)) else { throw RuntimeError.nativeFailure("clipboard representation could not be prepared") }
            }
            native.append(output)
        }
            let board = NSPasteboard.general
            guard !expectedGeneration.isEmpty, generation(board) == expectedGeneration else { throw RuntimeError.staleReference("clipboard changed; conditional write was not dispatched") }
            board.clearContents()
            if !native.isEmpty, !board.writeObjects(native) { throw RuntimeError.nativeFailure("clipboard write dispatched but completion is unknown") }
            return ClipboardReceipt(generation: generation(board), dispatch: "dispatched")
        }
    }
}

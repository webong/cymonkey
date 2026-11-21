import ExtClipboard
import Foundation

public typealias ClipboardPolicy = ExtClipboard.ClipboardPolicy
public typealias ClipboardRepresentation = ExtClipboard.ClipboardRepresentation
public typealias ClipboardItem = ExtClipboard.ClipboardItem
public typealias ClipboardSnapshot = ExtClipboard.ClipboardSnapshot
public typealias ClipboardReceipt = ExtClipboard.ClipboardReceipt

/// Cymonkey's request-bound policy around ext's caller-owned native clipboard.
/// The ext binding stays on the main actor and keeps one generation identity.
public final class SystemClipboardProvider: @unchecked Sendable {
    private let policy: ClipboardPolicy
    private let native: ExtClipboard.SystemClipboard

    public init(policy: ClipboardPolicy) throws {
        try policy.validate()
        guard policy.maxBytes <= 2 * 1024 * 1024 else {
            throw RuntimeError.invalidRequest("clipboard maxBytes exceeds Cymonkey's 2 MiB limit")
        }
        self.policy = policy
        self.native = try Self.onMain { try ExtClipboard.SystemClipboard(policy: policy) }
    }

    public func read(maxBytes: Int) throws -> ClipboardSnapshot {
        guard policy.allowRead, maxBytes > 0, maxBytes <= policy.maxBytes else {
            throw RuntimeError.denied("clipboard read exceeds policy")
        }
        do {
            let snapshot = try Self.onMain { try self.native.read() }
            let bytes = snapshot.items.reduce(0) { total, item in
                total + item.representations.reduce(0) { $0 + $1.data.count }
            }
            guard bytes <= maxBytes else { throw RuntimeError.denied("clipboard exceeds request byte ceiling") }
            return snapshot
        } catch let error as ExtClipboard.ClipboardError {
            throw Self.map(error)
        }
    }

    public func write(expectedGeneration: String, items: [ClipboardItem], maxBytes: Int) throws -> ClipboardReceipt {
        guard policy.allowWrite, maxBytes > 0, maxBytes <= policy.maxBytes else {
            throw RuntimeError.denied("clipboard write exceeds policy")
        }
        do {
            try ExtClipboard.ClipboardValidation.validate(
                items: items, maxBytes: maxBytes, formats: policy.allowedFormats)
            return try Self.onMain { try self.native.write(expectedGeneration: expectedGeneration, items: items) }
        } catch let error as ExtClipboard.ClipboardError {
            throw Self.map(error)
        }
    }

    private static func map(_ error: ExtClipboard.ClipboardError) -> RuntimeError {
        switch error {
        case .invalidValue: return .invalidRequest("clipboard input is invalid")
        case .denied: return .denied("clipboard format is not allowed")
        case .limitExceeded: return .denied("clipboard exceeds policy limits")
        case .changed: return .staleReference("clipboard changed before write")
        case .nativeFailure: return .nativeFailure("clipboard operation failed")
        case .timeout, .canceled: return .unavailable("clipboard operation stopped")
        }
    }

    private static func onMain<T>(_ work: @escaping @MainActor () throws -> T) rethrows -> T {
        if Thread.isMainThread { return try MainActor.assumeIsolated(work) }
        return try DispatchQueue.main.sync { try MainActor.assumeIsolated(work) }
    }
}

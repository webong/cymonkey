import AppKit
import ApplicationServices
import Foundation

public struct NativeTargetReference: Codable, Equatable, Sendable {
    public let surfaceId: String
    public let producerId: String
    public let processGeneration: String
    public let windowId: String?
}

public struct NativeBounds: Codable, Equatable, Sendable {
    public let x: Double
    public let y: Double
    public let width: Double
    public let height: Double
}

public struct NativeObservation: Codable, Equatable, Sendable {
    public let id: String
    public let target: NativeTargetReference
    public let observedAt: String
    public let expiresAt: String
    public let coordinateSpace: String
    public let completeness: String
    public let reason: String?
    public let visited: Int
    public let elements: [AccessibilityElementDescription]
}

// Optional protocol: old query providers remain usable without claiming fresh
// snapshot or receipt support. The system provider implements both surfaces.
public protocol ObservationProviding: AccessibilityProviding {
    func observe(surfaceId: String, selector: [String: JSONValue]) throws -> NativeObservation
    func validateObservation(id: String, target: NativeTargetReference) throws
    func validateElement(observationId: String, elementId: String) throws
}

public protocol AttributeReading: AccessibilityProviding {
    func readAttribute(surfaceId: String, elementId: String, attribute: String) throws -> JSONValue?
}

func nativeProcessGeneration(_ pid: pid_t) throws -> String {
    guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated,
          let launched = app.launchDate else {
        throw RuntimeError.staleReference("application process generation is unavailable")
    }
    return "\(pid):\(String(format: "%.6f", launched.timeIntervalSince1970))"
}

func nativeTimestamp(_ date: Date) -> String {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.string(from: date)
}

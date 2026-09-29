import ApplicationServices
import AppKit
import CoreGraphics
import Foundation

public struct ViewerSurface: Codable, Equatable, Sendable {
    public let id: String
    public let processId: Int32
    public let windowId: UInt32
    public let label: String?
    public let x: Int
    public let y: Int
    public let width: Int
    public let height: Int
}

public struct ViewerCapture: Codable, Equatable, Sendable {
    public let format: String
    public let width: Int
    public let height: Int
    public let base64Data: String
}

public protocol ViewerProviding: Sendable {
    var captureAuthorized: Bool { get }
    var inputAuthorized: Bool { get }
    func surfaces() -> [ViewerSurface]
    func capture(surfaceId: String) throws -> ViewerCapture
    func move(surfaceId: String, x: Int, y: Int) throws
    func click(surfaceId: String, x: Int, y: Int, button: String) throws
    func drag(surfaceId: String, startX: Int, startY: Int, endX: Int, endY: Int) throws
    func scroll(surfaceId: String, x: Int, y: Int, deltaY: Int) throws
    func type(surfaceId: String, text: String) throws
    func press(surfaceId: String, key: String) throws
}

public final class SystemViewerProvider: ViewerProviding, @unchecked Sendable {
    private let allowedBundleIDs: Set<String>
    private let policy: ViewerPolicy

    public init(allowedBundleIDs: [String], policy: ViewerPolicy) {
        self.allowedBundleIDs = Set(allowedBundleIDs)
        self.policy = policy
        if policy.promptForScreenCaptureConsent && policy.allowCapture {
            _ = CGRequestScreenCaptureAccess()
        }
    }

    public var captureAuthorized: Bool {
        policy.enabled && policy.allowCapture && CGPreflightScreenCaptureAccess()
    }

    public var inputAuthorized: Bool {
        policy.enabled && policy.allowInput && AXIsProcessTrusted()
    }

    public func surfaces() -> [ViewerSurface] {
        let allowedPIDs = Set(allowedBundleIDs.flatMap { bundleID in
            NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).map(\.processIdentifier)
        })
        guard !allowedPIDs.isEmpty,
              let values = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] else {
            return []
        }
        return values.compactMap { value in
            guard let pidValue = value[kCGWindowOwnerPID as String] as? NSNumber,
                  let windowValue = value[kCGWindowNumber as String] as? NSNumber,
                  allowedPIDs.contains(pid_t(pidValue.int32Value)),
                  let bounds = value[kCGWindowBounds as String] as? [String: Any],
                  let rect = CGRect(dictionaryRepresentation: bounds as CFDictionary),
                  rect.width > 0, rect.height > 0 else { return nil }
            let pid = pidValue.int32Value
            let windowID = windowValue.uint32Value
            return ViewerSurface(
                id: "macos-viewer:\(pid)-\(windowID)", processId: pid, windowId: windowID,
                label: value[kCGWindowName as String] as? String,
                x: Int(rect.origin.x), y: Int(rect.origin.y), width: Int(rect.width), height: Int(rect.height)
            )
        }.sorted { $0.id < $1.id }
    }

    public func capture(surfaceId: String) throws -> ViewerCapture {
        guard captureAuthorized else { throw RuntimeError.unavailable("Screen Recording consent is not granted") }
        let surface = try requireSurface(surfaceId)
        guard let image = CGWindowListCreateImage(.null, .optionIncludingWindow, CGWindowID(surface.windowId), [.bestResolution]) else {
            throw RuntimeError.nativeFailure("display capture failed")
        }
        let bitmap = NSBitmapImageRep(cgImage: image)
        guard let data = bitmap.representation(using: .png, properties: [:]) else {
            throw RuntimeError.nativeFailure("display capture encoding failed")
        }
        return ViewerCapture(format: "image/png", width: image.width, height: image.height, base64Data: data.base64EncodedString())
    }

    public func move(surfaceId: String, x: Int, y: Int) throws {
        try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        guard let event = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point(surfaceId: surfaceId, x: x, y: y), mouseButton: .left) else {
            throw RuntimeError.nativeFailure("pointer move failed")
        }
        event.post(tap: .cghidEventTap)
    }

    public func click(surfaceId: String, x: Int, y: Int, button: String) throws {
        try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        let resolved: (CGMouseButton, CGEventType, CGEventType)
        switch button.lowercased() {
        case "", "left": resolved = (.left, .leftMouseDown, .leftMouseUp)
        case "right": resolved = (.right, .rightMouseDown, .rightMouseUp)
        case "middle": resolved = (.center, .otherMouseDown, .otherMouseUp)
        default: throw RuntimeError.invalidRequest("unsupported pointer button")
        }
        let location = point(surfaceId: surfaceId, x: x, y: y)
        guard let down = CGEvent(mouseEventSource: nil, mouseType: resolved.1, mouseCursorPosition: location, mouseButton: resolved.0),
              let up = CGEvent(mouseEventSource: nil, mouseType: resolved.2, mouseCursorPosition: location, mouseButton: resolved.0) else {
            throw RuntimeError.nativeFailure("pointer click failed")
        }
        down.post(tap: .cghidEventTap); up.post(tap: .cghidEventTap)
    }

    public func drag(surfaceId: String, startX: Int, startY: Int, endX: Int, endY: Int) throws {
        try requireInputPoint(surfaceId: surfaceId, x: startX, y: startY)
        try requireInputPoint(surfaceId: surfaceId, x: endX, y: endY)
        let start = point(surfaceId: surfaceId, x: startX, y: startY)
        let end = point(surfaceId: surfaceId, x: endX, y: endY)
        guard let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: start, mouseButton: .left),
              let dragged = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged, mouseCursorPosition: end, mouseButton: .left),
              let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: end, mouseButton: .left) else {
            throw RuntimeError.nativeFailure("pointer drag failed")
        }
        down.post(tap: .cghidEventTap); dragged.post(tap: .cghidEventTap); up.post(tap: .cghidEventTap)
    }

    public func scroll(surfaceId: String, x: Int, y: Int, deltaY: Int) throws {
        try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        guard let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: Int32(deltaY), wheel2: 0, wheel3: 0) else {
            throw RuntimeError.nativeFailure("pointer scroll failed")
        }
        event.post(tap: .cghidEventTap)
    }

    public func type(surfaceId: String, text: String) throws {
        _ = try requireSurface(surfaceId)
        try requireInput()
        guard policy.maxTextLength == 0 || text.count <= policy.maxTextLength else { throw RuntimeError.denied("keyboard text exceeds viewer policy") }
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) else {
            throw RuntimeError.nativeFailure("keyboard input failed")
        }
        let characters = Array(text.utf16)
        down.keyboardSetUnicodeString(stringLength: characters.count, unicodeString: characters)
        up.keyboardSetUnicodeString(stringLength: characters.count, unicodeString: characters)
        down.post(tap: .cghidEventTap); up.post(tap: .cghidEventTap)
    }

    public func press(surfaceId: String, key: String) throws {
        _ = try requireSurface(surfaceId)
        try requireInput()
        let normalized = key.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !policy.blockedKeys.map({ $0.lowercased() }).contains(normalized) else { throw RuntimeError.denied("key is blocked by viewer policy") }
        let keys: [String: CGKeyCode] = ["enter": 36, "tab": 48, "escape": 53, "space": 49, "left": 123, "right": 124, "down": 125, "up": 126, "delete": 51]
        guard let code = keys[normalized], let down = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true), let up = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false) else {
            throw RuntimeError.invalidRequest("unsupported keyboard key")
        }
        down.post(tap: .cghidEventTap); up.post(tap: .cghidEventTap)
    }

    private func requireInput() throws { guard inputAuthorized else { throw RuntimeError.unavailable("Accessibility consent is not granted") } }
    private func requireInputPoint(surfaceId: String, x: Int, y: Int) throws { try requireInput(); let surface = try requireSurface(surfaceId); guard x >= 0 && y >= 0 && x < surface.width && y < surface.height else { throw RuntimeError.denied("coordinates exceed viewer surface bounds") } }
    private func point(surfaceId: String, x: Int, y: Int) -> CGPoint { let surface = surfaces().first { $0.id == surfaceId }; return CGPoint(x: (surface?.x ?? 0) + x, y: (surface?.y ?? 0) + y) }
    private func requireSurface(_ surfaceId: String) throws -> ViewerSurface { guard let surface = surfaces().first(where: { $0.id == surfaceId }) else { throw RuntimeError.staleReference("viewer surface is unavailable or stale") }; return surface }
}

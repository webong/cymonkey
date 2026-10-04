import ApplicationServices
import AppKit
import CoreGraphics
import Foundation
import CryptoKit

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
    public let captureId: String?
    public let target: NativeTargetReference?
    public let observedAt: String?
    public let expiresAt: String?
    public let sha256: String?
    public let coordinateSpace: String?
    public init(format: String, width: Int, height: Int, base64Data: String, captureId: String? = nil, target: NativeTargetReference? = nil, observedAt: String? = nil, expiresAt: String? = nil, sha256: String? = nil, coordinateSpace: String? = nil) {
        self.format = format; self.width = width; self.height = height; self.base64Data = base64Data
        self.captureId = captureId; self.target = target; self.observedAt = observedAt; self.expiresAt = expiresAt
        self.sha256 = sha256; self.coordinateSpace = coordinateSpace
    }
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

public protocol SnapshotViewerProviding: ViewerProviding {
    func validateCapture(id: String, surfaceId: String) throws -> NativeTargetReference
    func capturePoint(id: String, surfaceId: String, x: Int, y: Int) throws -> (Int, Int)
}

public final class SystemViewerProvider: SnapshotViewerProviding, @unchecked Sendable {
    private let allowedBundleIDs: Set<String>
    private let policy: ViewerPolicy
    private let producerID = UUID().uuidString.lowercased()
    private let lock = NSRecursiveLock()
    private var captures: [String: (NativeTargetReference, ViewerSurface, UInt64, Int, Int)] = [:]

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
        lock.lock(); defer { lock.unlock() }
        let surface = try requireSurface(surfaceId)
        let generation = try nativeProcessGeneration(surface.processId)
        guard surface.width <= 16_777_216 / surface.height else { throw RuntimeError.denied("capture exceeds pixel ceiling") }
        guard let image = CGWindowListCreateImage(.null, .optionIncludingWindow, CGWindowID(surface.windowId), [.bestResolution, .boundsIgnoreFraming]) else {
            throw RuntimeError.nativeFailure("display capture failed")
        }
        guard image.width <= 16_777_216 / max(1, image.height), try requireSurface(surfaceId) == surface,
              try nativeProcessGeneration(surface.processId) == generation else { throw RuntimeError.staleReference("capture target changed or exceeds pixel ceiling") }
        let bitmap = NSBitmapImageRep(cgImage: image)
        guard let data = bitmap.representation(using: .png, properties: [:]) else {
            throw RuntimeError.nativeFailure("display capture encoding failed")
        }
        guard data.count <= 2 * 1024 * 1024 else { throw RuntimeError.denied("encoded capture exceeds control byte ceiling") }
        let now = Date(); let id = UUID().uuidString.lowercased()
        let target = NativeTargetReference(surfaceId: surfaceId, producerId: producerID, processGeneration: generation, windowId: String(surface.windowId))
        captures = captures.filter { $0.value.2 > DispatchTime.now().uptimeNanoseconds }
        if captures.count >= 128 { captures.removeAll() }
        captures[id] = (target, surface, DispatchTime.now().uptimeNanoseconds + 15_000_000_000, image.width, image.height)
        return ViewerCapture(format: "image/png", width: image.width, height: image.height, base64Data: data.base64EncodedString(), captureId: id, target: target, observedAt: nativeTimestamp(now), expiresAt: nativeTimestamp(now.addingTimeInterval(15)), sha256: SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined(), coordinateSpace: "window-local-pixels")
    }

    public func move(surfaceId: String, x: Int, y: Int) throws {
        let surface = try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        guard let event = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point(surface: surface, x: x, y: y), mouseButton: .left) else {
            throw RuntimeError.nativeFailure("pointer move failed")
        }
        event.postToPid(pid_t(surface.processId))
    }

    public func click(surfaceId: String, x: Int, y: Int, button: String) throws {
        let surface = try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        let resolved: (CGMouseButton, CGEventType, CGEventType)
        switch button.lowercased() {
        case "", "left": resolved = (.left, .leftMouseDown, .leftMouseUp)
        case "right": resolved = (.right, .rightMouseDown, .rightMouseUp)
        case "middle": resolved = (.center, .otherMouseDown, .otherMouseUp)
        default: throw RuntimeError.invalidRequest("unsupported pointer button")
        }
        let location = point(surface: surface, x: x, y: y)
        guard let down = CGEvent(mouseEventSource: nil, mouseType: resolved.1, mouseCursorPosition: location, mouseButton: resolved.0),
              let up = CGEvent(mouseEventSource: nil, mouseType: resolved.2, mouseCursorPosition: location, mouseButton: resolved.0) else {
            throw RuntimeError.nativeFailure("pointer click failed")
        }
        down.postToPid(pid_t(surface.processId)); up.postToPid(pid_t(surface.processId))
    }

    public func drag(surfaceId: String, startX: Int, startY: Int, endX: Int, endY: Int) throws {
        let surface = try requireInputPoint(surfaceId: surfaceId, x: startX, y: startY)
        guard endX >= 0, endY >= 0, endX < surface.width, endY < surface.height else { throw RuntimeError.denied("drag endpoint exceeds surface") }
        let start = point(surface: surface, x: startX, y: startY)
        let end = point(surface: surface, x: endX, y: endY)
        guard let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: start, mouseButton: .left),
              let dragged = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged, mouseCursorPosition: end, mouseButton: .left),
              let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: end, mouseButton: .left) else {
            throw RuntimeError.nativeFailure("pointer drag failed")
        }
        down.postToPid(pid_t(surface.processId)); dragged.postToPid(pid_t(surface.processId)); up.postToPid(pid_t(surface.processId))
    }

    public func scroll(surfaceId: String, x: Int, y: Int, deltaY: Int) throws {
        let surface = try requireInputPoint(surfaceId: surfaceId, x: x, y: y)
        guard deltaY >= Int(Int32.min), deltaY <= Int(Int32.max) else { throw RuntimeError.invalidRequest("scroll delta exceeds integer range") }
        guard let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: Int32(deltaY), wheel2: 0, wheel3: 0) else {
            throw RuntimeError.nativeFailure("pointer scroll failed")
        }
        event.location = point(surface: surface, x: x, y: y)
        event.postToPid(pid_t(surface.processId))
    }

    public func type(surfaceId: String, text: String) throws {
        let surface = try requireSurface(surfaceId)
        try requireFocusedWindow(surface)
        try requireInput()
        guard policy.maxTextLength == 0 || text.count <= policy.maxTextLength else { throw RuntimeError.denied("keyboard text exceeds viewer policy") }
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) else {
            throw RuntimeError.nativeFailure("keyboard input failed")
        }
        let characters = Array(text.utf16)
        down.keyboardSetUnicodeString(stringLength: characters.count, unicodeString: characters)
        up.keyboardSetUnicodeString(stringLength: characters.count, unicodeString: characters)
        down.postToPid(pid_t(surface.processId)); up.postToPid(pid_t(surface.processId))
    }

    public func press(surfaceId: String, key: String) throws {
        let surface = try requireSurface(surfaceId)
        try requireFocusedWindow(surface)
        try requireInput()
        let normalized = key.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !policy.blockedKeys.map({ $0.lowercased() }).contains(normalized) else { throw RuntimeError.denied("key is blocked by viewer policy") }
        let keys: [String: CGKeyCode] = ["enter": 36, "tab": 48, "escape": 53, "space": 49, "left": 123, "right": 124, "down": 125, "up": 126, "delete": 51]
        guard let code = keys[normalized], let down = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true), let up = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false) else {
            throw RuntimeError.invalidRequest("unsupported keyboard key")
        }
        down.postToPid(pid_t(surface.processId)); up.postToPid(pid_t(surface.processId))
    }

    public func validateCapture(id: String, surfaceId: String) throws -> NativeTargetReference {
        lock.lock(); defer { lock.unlock() }
        guard let value = captures[id], value.0.surfaceId == surfaceId,
              value.2 > DispatchTime.now().uptimeNanoseconds,
              try requireSurface(surfaceId) == value.1,
              try nativeProcessGeneration(value.1.processId) == value.0.processGeneration else { throw RuntimeError.staleReference("capture target or geometry is expired or changed") }
        return value.0
    }

    public func capturePoint(id: String, surfaceId: String, x: Int, y: Int) throws -> (Int, Int) {
        lock.lock(); defer { lock.unlock() }
        _ = try validateCapture(id: id, surfaceId: surfaceId)
        guard let value = captures[id], x >= 0, y >= 0, x < value.3, y < value.4 else {
            throw RuntimeError.denied("coordinates exceed captured image")
        }
        // Captures use pixels; CG window geometry and input use desktop points.
        return (Int(floor(Double(x) * Double(value.1.width) / Double(value.3))),
                Int(floor(Double(y) * Double(value.1.height) / Double(value.4))))
    }

    private func requireInput() throws { guard inputAuthorized else { throw RuntimeError.unavailable("Accessibility consent is not granted") } }
    private func requireInputPoint(surfaceId: String, x: Int, y: Int) throws -> ViewerSurface {
        try requireInput(); let surface = try requireSurface(surfaceId)
        guard x >= 0, y >= 0, x < surface.width, y < surface.height else { throw RuntimeError.denied("coordinates exceed viewer surface bounds") }
        return surface
    }
    private func point(surface: ViewerSurface, x: Int, y: Int) -> CGPoint { CGPoint(x: surface.x + x, y: surface.y + y) }
    private func requireSurface(_ surfaceId: String) throws -> ViewerSurface {
        guard let surface = surfaces().first(where: { $0.id == surfaceId }) else { throw RuntimeError.staleReference("viewer surface is unavailable or stale") }; return surface
    }
    private func requireFocusedWindow(_ surface: ViewerSurface) throws {
        let root = AXUIElementCreateApplication(surface.processId)
        AXUIElementSetMessagingTimeout(root, 0.25)
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(root, kAXFocusedWindowAttribute as CFString, &value) == .success,
              let value, CFGetTypeID(value) == AXUIElementGetTypeID() else { throw RuntimeError.unavailable("focused target window cannot be verified") }
        let window = value as! AXUIElement
        var position: CFTypeRef?; var size: CFTypeRef?
        guard AXUIElementCopyAttributeValue(window, kAXPositionAttribute as CFString, &position) == .success,
              AXUIElementCopyAttributeValue(window, kAXSizeAttribute as CFString, &size) == .success,
              let position, let size, CFGetTypeID(position) == AXValueGetTypeID(), CFGetTypeID(size) == AXValueGetTypeID() else { throw RuntimeError.unavailable("focused window bounds unavailable") }
        var point = CGPoint.zero; var dimensions = CGSize.zero
        guard AXValueGetValue(position as! AXValue, .cgPoint, &point), AXValueGetValue(size as! AXValue, .cgSize, &dimensions),
              abs(point.x - Double(surface.x)) < 1, abs(point.y - Double(surface.y)) < 1,
              abs(dimensions.width - Double(surface.width)) < 1, abs(dimensions.height - Double(surface.height)) < 1 else { throw RuntimeError.denied("selected window is not the application's focused window") }
    }
}

import Foundation
import CoreGraphics
import ApplicationServices

enum NativePointer {
    struct Request: Decodable { let operation: String; let x: Double; let y: Double; let button: String; let deltaX: Int32; let deltaY: Int32 }
    static func perform(source: String, json: String) throws {
        guard AXIsProcessTrusted(), let display = UInt32(source), CGDisplayIsActive(display) != 0 else { throw Failure.permissionOrDisplay }
        let request = try JSONDecoder().decode(Request.self, from: Data(json.utf8))
        guard request.x.isFinite, request.y.isFinite, (0...1).contains(request.x), (0...1).contains(request.y), (-100...100).contains(request.deltaX), (-100...100).contains(request.deltaY) else { throw Failure.invalidRequest }
        let bounds = CGDisplayBounds(display)
        let point = CGPoint(x: bounds.minX + request.x * max(0, bounds.width - 1), y: bounds.minY + request.y * max(0, bounds.height - 1))
        guard let move = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left) else { throw Failure.unavailable }
        switch request.operation {
        case "pointer.move": move.post(tap: .cghidEventTap)
        case "pointer.click":
            let button: CGMouseButton; let down: CGEventType; let up: CGEventType
            switch request.button {
            case "left": button = .left; down = .leftMouseDown; up = .leftMouseUp
            case "right": button = .right; down = .rightMouseDown; up = .rightMouseUp
            case "middle": button = .center; down = .otherMouseDown; up = .otherMouseUp
            default: throw Failure.invalidRequest
            }
            guard let press = CGEvent(mouseEventSource: nil, mouseType: down, mouseCursorPosition: point, mouseButton: button),
                  let release = CGEvent(mouseEventSource: nil, mouseType: up, mouseCursorPosition: point, mouseButton: button) else { throw Failure.unavailable }
            press.post(tap: .cghidEventTap); release.post(tap: .cghidEventTap)
        case "pointer.scroll":
            guard let scroll = CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 2, wheel1: request.deltaY, wheel2: request.deltaX, wheel3: 0) else { throw Failure.unavailable }
            move.post(tap: .cghidEventTap); scroll.post(tap: .cghidEventTap)
        default: throw Failure.invalidRequest
        }
    }
    enum Failure: Error { case permissionOrDisplay, invalidRequest, unavailable }
}

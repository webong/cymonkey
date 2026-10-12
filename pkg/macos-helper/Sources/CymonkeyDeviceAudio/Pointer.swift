import Foundation
import CoreGraphics
import ApplicationServices

enum NativePointer {
    struct Request: Decodable { let operation: String; let x: Double; let y: Double; let button: String; let deltaX: Int32; let deltaY: Int32 }
    static func perform(source: String, json: String, sessionOwned: Bool = false) throws {
        guard AXIsProcessTrusted(), let display = UInt32(source), CGDisplayIsActive(display) != 0 else { throw Failure.permissionOrDisplay }
        let request = try JSONDecoder().decode(Request.self, from: Data(json.utf8))
        if ["pointer.down", "pointer.up"].contains(request.operation), !sessionOwned { throw Failure.invalidRequest }
        guard request.x.isFinite, request.y.isFinite, (0...1).contains(request.x), (0...1).contains(request.y), (-100...100).contains(request.deltaX), (-100...100).contains(request.deltaY) else { throw Failure.invalidRequest }
        let bounds = CGDisplayBounds(display)
        let point = CGPoint(x: bounds.minX + request.x * max(0, bounds.width - 1), y: bounds.minY + request.y * max(0, bounds.height - 1))
        guard let move = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left) else { throw Failure.unavailable }
        switch request.operation {
        case "pointer.move":
            if request.button.isEmpty { move.post(tap: .cghidEventTap) }
            else {
                let button: CGMouseButton = request.button == "left" ? .left : request.button == "right" ? .right : .center
                let type: CGEventType = request.button == "left" ? .leftMouseDragged : request.button == "right" ? .rightMouseDragged : .otherMouseDragged
                guard let drag = CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: point, mouseButton: button) else { throw Failure.unavailable }
                drag.post(tap: .cghidEventTap)
            }
        case "pointer.click", "pointer.down", "pointer.up":
            let button: CGMouseButton; let down: CGEventType; let up: CGEventType
            switch request.button {
            case "left": button = .left; down = .leftMouseDown; up = .leftMouseUp
            case "right": button = .right; down = .rightMouseDown; up = .rightMouseUp
            case "middle": button = .center; down = .otherMouseDown; up = .otherMouseUp
            default: throw Failure.invalidRequest
            }
            guard let press = CGEvent(mouseEventSource: nil, mouseType: down, mouseCursorPosition: point, mouseButton: button),
                  let release = CGEvent(mouseEventSource: nil, mouseType: up, mouseCursorPosition: point, mouseButton: button) else { throw Failure.unavailable }
            if request.operation != "pointer.up" { press.post(tap: .cghidEventTap) }
            if request.operation != "pointer.down" { release.post(tap: .cghidEventTap) }
        case "pointer.scroll":
            guard let scroll = CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 2, wheel1: request.deltaY, wheel2: request.deltaX, wheel3: 0) else { throw Failure.unavailable }
            move.post(tap: .cghidEventTap); scroll.post(tap: .cghidEventTap)
        default: throw Failure.invalidRequest
        }
    }
    // Session cleanup uses current cursor coordinates, even if the selected
    // display was unplugged. It only releases this helper's owned button.
    static func release(_ name: String) {
        guard !name.isEmpty else { return }
        let button: CGMouseButton = name == "left" ? .left : name == "right" ? .right : .center
        let type: CGEventType = name == "left" ? .leftMouseUp : name == "right" ? .rightMouseUp : .otherMouseUp
        let point = CGEvent(source: nil)?.location ?? .zero
        CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: point, mouseButton: button)?.post(tap: .cghidEventTap)
    }
    static func session(source: String) throws {
        var held = ""
        defer { release(held) }
        while true {
            let ready = DispatchSemaphore(value: 0)
            // Only this worker reads stdin; at most one read is pending.
            final class Line: @unchecked Sendable { var value: String? }
            let line = Line()
            DispatchQueue.global().async { line.value = readLine(); ready.signal() }
            let deadline: DispatchTime = held.isEmpty ? .distantFuture : .now() + 5
            guard ready.wait(timeout: deadline) == .success else { throw Failure.idleTimeout }
            guard let text = line.value else { return }
            guard text.utf8.count <= 4096,
                  var request = try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any],
                  let operation = request["operation"] as? String else { throw Failure.invalidRequest }
            let button = request["button"] as? String ?? ""
            switch operation {
            case "pointer.release": release(held); held = ""
            case "pointer.down":
                guard held.isEmpty, ["left", "right", "middle"].contains(button) else { throw Failure.invalidRequest }
                held = button // Track before injection, including partial failure.
                try perform(source: source, json: text, sessionOwned: true)
            case "pointer.up":
                guard held.isEmpty || held == button else { throw Failure.invalidRequest }
                if !held.isEmpty { try perform(source: source, json: text, sessionOwned: true); held = "" }
            case "pointer.move":
                request["button"] = held
                let raw = try JSONSerialization.data(withJSONObject: request)
                try perform(source: source, json: String(decoding: raw, as: UTF8.self))
            case "pointer.click", "pointer.scroll":
                guard held.isEmpty else { throw Failure.invalidRequest }
                try perform(source: source, json: text, sessionOwned: true)
            default: throw Failure.invalidRequest
            }
            try FileHandle.standardOutput.write(contentsOf: Data("ok\n".utf8))
        }
    }
    enum Failure: Error { case permissionOrDisplay, invalidRequest, unavailable, idleTimeout }
}

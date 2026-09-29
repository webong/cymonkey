import ApplicationServices
import AppKit
import Foundation

public struct AccessibilitySurface: Codable, Equatable, Sendable {
    public let id: String
    public let bundleId: String
    public let processId: Int32
    public let label: String?
}

public struct AccessibilityElementDescription: Codable, Equatable, Sendable {
    public let id: String
    public let surfaceId: String
    public let role: String?
    public let title: String?
    public let identifier: String?
    public let actions: [String]
    public let settableAttributes: [String]
}

public protocol AccessibilityProviding: Sendable {
    var authorized: Bool { get }
    func surfaces() -> [AccessibilitySurface]
    func query(surfaceId: String, selector: [String: JSONValue]) throws -> [AccessibilityElementDescription]
    func perform(surfaceId: String, elementId: String, action: String) throws
    func setAttribute(surfaceId: String, elementId: String, attribute: String, value: JSONValue) throws
}

public final class SystemAccessibilityProvider: AccessibilityProviding, @unchecked Sendable {
    private struct Reference {
        let surfaceId: String
        let processId: pid_t
        let element: AXUIElement
    }

    private let policy: AccessibilityPolicy
    private let lock = NSLock()
    private var references: [String: Reference] = [:]

    public init(policy: AccessibilityPolicy) {
        self.policy = policy
        if policy.promptForAccessibilityConsent {
            _ = AXIsProcessTrustedWithOptions([
                kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true,
            ] as CFDictionary)
        }
    }

    public var authorized: Bool { AXIsProcessTrusted() }

    public func surfaces() -> [AccessibilitySurface] {
        policy.allowedBundleIds.flatMap { bundleID in
            NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).map { application in
                AccessibilitySurface(
                    id: surfaceID(bundleID: bundleID, processID: application.processIdentifier),
                    bundleId: bundleID,
                    processId: application.processIdentifier,
                    label: application.localizedName
                )
            }
        }.sorted { $0.id < $1.id }
    }

    public func query(surfaceId: String, selector: [String: JSONValue]) throws -> [AccessibilityElementDescription] {
        try requireAuthorization()
        let surface = try requireSurface(surfaceId)
        let requestedLimit = selector["limit"].flatMap(numberValue).map(Int.init) ?? policy.maxResults
        let limit = max(1, min(policy.maxResults, requestedLimit))
        let root = AXUIElementCreateApplication(pid_t(surface.processId))
        var queue: [(AXUIElement, Int)] = [(root, 0)]
        var cursor = 0
        var results: [AccessibilityElementDescription] = []
        while cursor < queue.count, results.count < limit {
            let (element, depth) = queue[cursor]
            cursor += 1
            if matches(element, selector: selector) {
                results.append(describe(element, surface: surface))
            }
            if depth < policy.maxDepth {
                for child in elementArrayAttribute(element, kAXChildrenAttribute as String) {
                    queue.append((child, depth + 1))
                }
            }
        }
        return results
    }

    public func perform(surfaceId: String, elementId: String, action: String) throws {
        try requireAuthorization()
        guard action.hasPrefix("AX"), !action.contains("\n") else {
            throw RuntimeError.invalidRequest("invalid Accessibility action")
        }
        let reference = try resolve(surfaceId: surfaceId, elementId: elementId)
        var names: CFArray?
        guard AXUIElementCopyActionNames(reference.element, &names) == .success,
              let supported = names as? [String], supported.contains(action) else {
            throw RuntimeError.denied("Accessibility action is not supported by the element")
        }
        guard AXUIElementPerformAction(reference.element, action as CFString) == .success else {
            throw RuntimeError.nativeFailure("Accessibility action failed")
        }
    }

    public func setAttribute(surfaceId: String, elementId: String, attribute: String, value: JSONValue) throws {
        try requireAuthorization()
        guard policy.allowedWritableAttributes.contains(attribute) else {
            throw RuntimeError.denied("Accessibility attribute is not allowlisted")
        }
        let reference = try resolve(surfaceId: surfaceId, elementId: elementId)
        var settable = DarwinBoolean(false)
        guard AXUIElementIsAttributeSettable(reference.element, attribute as CFString, &settable) == .success,
              settable.boolValue else {
            throw RuntimeError.denied("Accessibility attribute is not settable")
        }
        let nativeValue: CFTypeRef
        switch value {
        case .string(let decoded): nativeValue = decoded as CFString
        case .bool(let decoded): nativeValue = decoded as CFBoolean
        case .number(let decoded): nativeValue = decoded as CFNumber
        default: throw RuntimeError.invalidRequest("unsupported Accessibility attribute value")
        }
        guard AXUIElementSetAttributeValue(reference.element, attribute as CFString, nativeValue) == .success else {
            throw RuntimeError.nativeFailure("Accessibility attribute update failed")
        }
    }

    private func requireAuthorization() throws {
        guard authorized else {
            throw RuntimeError.unavailable("Accessibility consent is not granted")
        }
    }

    private func requireSurface(_ id: String) throws -> AccessibilitySurface {
        guard let surface = surfaces().first(where: { $0.id == id }) else {
            throw RuntimeError.staleReference("application surface is unavailable or stale")
        }
        return surface
    }

    private func resolve(surfaceId: String, elementId: String) throws -> Reference {
        let surface = try requireSurface(surfaceId)
        lock.lock()
        let reference = references[elementId]
        lock.unlock()
        guard let reference,
              reference.surfaceId == surfaceId,
              reference.processId == pid_t(surface.processId) else {
            throw RuntimeError.staleReference("Accessibility element reference is unavailable or stale")
        }
        return reference
    }

    private func describe(_ element: AXUIElement, surface: AccessibilitySurface) -> AccessibilityElementDescription {
        let id = UUID().uuidString.lowercased()
        lock.lock()
        references[id] = Reference(surfaceId: surface.id, processId: pid_t(surface.processId), element: element)
        if references.count > policy.maxResults * 20 {
            references.removeAll(keepingCapacity: true)
            references[id] = Reference(surfaceId: surface.id, processId: pid_t(surface.processId), element: element)
        }
        lock.unlock()
        let actions = actionNames(element)
        let settable = policy.allowedWritableAttributes.filter { attribute in
            var result = DarwinBoolean(false)
            return AXUIElementIsAttributeSettable(element, attribute as CFString, &result) == .success && result.boolValue
        }
        return AccessibilityElementDescription(
            id: id,
            surfaceId: surface.id,
            role: stringAttribute(element, kAXRoleAttribute as String),
            title: stringAttribute(element, kAXTitleAttribute as String),
            identifier: stringAttribute(element, kAXIdentifierAttribute as String),
            actions: actions,
            settableAttributes: settable.sorted()
        )
    }

    private func matches(_ element: AXUIElement, selector: [String: JSONValue]) -> Bool {
        for (key, attribute) in [
            ("role", kAXRoleAttribute as String),
            ("title", kAXTitleAttribute as String),
            ("identifier", kAXIdentifierAttribute as String),
        ] {
            if let expected = selector[key]?.stringValue,
               stringAttribute(element, attribute) != expected {
                return false
            }
        }
        return true
    }

    private func stringAttribute(_ element: AXUIElement, _ name: String) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
        return value as? String
    }

    private func elementArrayAttribute(_ element: AXUIElement, _ name: String) -> [AXUIElement] {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success,
              let result = value as? [AXUIElement] else { return [] }
        return result
    }

    private func actionNames(_ element: AXUIElement) -> [String] {
        var value: CFArray?
        guard AXUIElementCopyActionNames(element, &value) == .success,
              let result = value as? [String] else { return [] }
        return result.sorted()
    }
}

private func surfaceID(bundleID: String, processID: pid_t) -> String {
    "macos:\(bundleID):\(processID)"
}

private func numberValue(_ value: JSONValue) -> Double? {
    guard case .number(let decoded) = value else { return nil }
    return decoded
}

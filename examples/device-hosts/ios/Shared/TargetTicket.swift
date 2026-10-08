import Foundation

struct TargetTicket: Codable {
    let endpoint: URL
    let token: String
    let audio: Bool
    let microphone: Bool
    let playback: Bool
    let expires: Date
    static func location() throws -> URL {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "BoardAppGroup") as? String,
              let root = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else {
            throw NSError(domain: "BoardReference", code: 1, userInfo: [NSLocalizedDescriptionKey: "Configure the shared App Group"])
        }
        return root.appendingPathComponent("target.json")
    }
    func stage() throws {
        let path = try Self.location()
        try JSONEncoder().encode(self).write(to: path, options: [.atomic, .completeFileProtection])
        var values = URLResourceValues(); values.isExcludedFromBackup = true
        var mutable = path; try mutable.setResourceValues(values)
    }
    static func consume() throws -> TargetTicket {
        let path = try location()
        defer { try? FileManager.default.removeItem(at: path) }
        let ticket = try JSONDecoder().decode(Self.self, from: Data(contentsOf: path))
        guard ticket.expires > Date(), !ticket.playback else {
            throw NSError(domain: "BoardReference", code: 2, userInfo: [NSLocalizedDescriptionKey: "Expired ticket or unsupported broadcast playback"])
        }
        return ticket
    }
    static func clear() { if let path = try? location() { try? FileManager.default.removeItem(at: path) } }
}

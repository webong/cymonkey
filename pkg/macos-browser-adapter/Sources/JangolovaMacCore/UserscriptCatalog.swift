import Foundation

public actor UserscriptCatalog {
    public static let recordKey = "cymonkey.userscripts.catalog.v1"
    private let defaults: UserDefaults

    public init(appGroup: String? = nil) {
        self.defaults = appGroup.flatMap(UserDefaults.init(suiteName:)) ?? .standard
    }

    public func list() throws -> [UserscriptSummary] {
        guard let data = defaults.data(forKey: Self.recordKey) else { return [] }
        do {
            return try JSONDecoder().decode([UserscriptSummary].self, from: data).sorted { $0.id < $1.id }
        } catch {
            throw MacExtensionError.userscriptCatalogInvalid
        }
    }

    public func replace(_ values: [UserscriptSummary]) throws {
        let unique = Dictionary(values.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        defaults.set(try JSONEncoder().encode(unique.values.sorted { $0.id < $1.id }), forKey: Self.recordKey)
    }

    public func setEnabled(id: String, enabled: Bool) throws -> UserscriptSummary {
        var values = try list()
        guard let index = values.firstIndex(where: { $0.id == id }) else {
            throw MacExtensionError.userscriptCatalogInvalid
        }
        values[index].enabled = enabled
        try replace(values)
        return values[index]
    }
}

import Foundation
import SQLite3

// Cymonkey addition: inspect optional browser isolation columns before a
// basic cookie read so partitioned/container cookies are not flattened.
enum SQLiteCookieScope {
    static func columns(in database: OpaquePointer?, table: String) throws -> Set<String> {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(database, "PRAGMA table_info(\(table))", -1, &statement, nil) == SQLITE_OK else {
            throw NSError(domain: "SQLiteCookieScope", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "Cannot inspect cookie store scope"])
        }
        defer { sqlite3_finalize(statement) }
        var names = Set<String>()
        while sqlite3_step(statement) == SQLITE_ROW {
            if let name = sqlite3_column_text(statement, 1) {
                names.insert(String(cString: name))
            }
        }
        guard !names.isEmpty else {
            throw NSError(domain: "SQLiteCookieScope", code: 2,
                          userInfo: [NSLocalizedDescriptionKey: "Cookie store has no readable columns"])
        }
        return names
    }
}

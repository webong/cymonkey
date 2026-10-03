import Foundation
import SQLite3
import XCTest
@testable import MacOSBrowserCookies

final class BrowserCookieReaderTests: XCTestCase {
    func testSelectedFirefoxStoreReadsOnlyExactDomainAndName() throws {
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("macos-browser-cookies-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let profile = home.appendingPathComponent(
            "Library/Application Support/Firefox/Profiles/demo.default-release", isDirectory: true)
        try FileManager.default.createDirectory(at: profile, withIntermediateDirectories: true)
        let database = profile.appendingPathComponent("cookies.sqlite")
        try makeFixtureDatabase(at: database)

        let reader = MacOSBrowserCookieReader(homeDirectories: [home])
        let stores = reader.discover(browsers: [.firefox])
        XCTAssertEqual(stores.count, 1)
        XCTAssertEqual(stores[0].browser, .firefox)
        XCTAssertEqual(stores[0].profileName, "demo.default-release")

        let cookies = try reader.read(from: stores[0], domains: ["example.com"], names: ["session"])
        XCTAssertEqual(cookies.count, 2)
        XCTAssertTrue(cookies.contains { $0.value == "host-value" && $0.hostOnly })
        XCTAssertTrue(cookies.contains { $0.value == "domain-value" && !$0.hostOnly })
        XCTAssertFalse(cookies.contains { $0.value == "wrong-domain" })
        XCTAssertFalse(cookies.contains { $0.value == "container-value" })
        XCTAssertFalse(cookies.contains { $0.name == "other" })

        XCTAssertThrowsError(try reader.read(from: stores[0], domains: []))
        XCTAssertThrowsError(try reader.read(from: stores[0], domains: ["*.example.com"]))
        let explicitReader = MacOSBrowserCookieReader(homeDirectories: [])
        let explicitStore = try explicitReader.select(browser: .firefox, cookieFile: database)
        XCTAssertEqual(try explicitReader.read(
            from: explicitStore, domains: ["example.com"], names: ["session"]).count, 2)
        XCTAssertThrowsError(try explicitReader.select(
            browser: .firefox, cookieFile: profile.appendingPathComponent("wrong-name.sqlite")))
        try FileManager.default.removeItem(at: database)
        XCTAssertThrowsError(try reader.read(from: stores[0], domains: ["example.com"]))
        XCTAssertThrowsError(try explicitReader.read(from: explicitStore, domains: ["example.com"]))
    }

    private func makeFixtureDatabase(at url: URL) throws {
        var database: OpaquePointer?
        guard sqlite3_open(url.path, &database) == SQLITE_OK else {
            throw NSError(domain: "fixture", code: 1)
        }
        defer { sqlite3_close(database) }
        let sql = """
        CREATE TABLE moz_cookies (
          host TEXT, name TEXT, path TEXT, value TEXT,
          expiry INTEGER, isSecure INTEGER, isHttpOnly INTEGER,
          originAttributes TEXT
        );
        INSERT INTO moz_cookies VALUES
          ('example.com', 'session', '/', 'host-value', 4102444800, 1, 1, ''),
          ('.example.com', 'session', '/', 'domain-value', 4102444800, 1, 1, ''),
          ('evil-example.com', 'session', '/', 'wrong-domain', 4102444800, 1, 1, ''),
          ('example.com', 'session', '/', 'container-value', 4102444800, 1, 1, '^userContextId=2'),
          ('example.com', 'other', '/', 'other-value', 4102444800, 0, 0, '');
        """
        guard sqlite3_exec(database, sql, nil, nil, nil) == SQLITE_OK else {
            throw NSError(domain: "fixture", code: 2)
        }
    }
}

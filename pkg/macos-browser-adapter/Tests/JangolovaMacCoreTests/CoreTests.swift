import Foundation
import XCTest
@testable import JangolovaMacCore

final class CoreTests: XCTestCase {
    func testAutoModeUsesManagedRuntimeOnlyWhenLocalRuntimeExists() {
        XCTAssertEqual(HelperMode.auto.resolved(localRuntimeAvailable: true), .managed)
        XCTAssertEqual(HelperMode.auto.resolved(localRuntimeAvailable: false), .external)
        XCTAssertEqual(HelperMode.external.resolved(localRuntimeAvailable: true), .external)
    }

    func testManagedConfigurationRequiresAbsoluteConfigAndExactFields() throws {
        XCTAssertThrowsError(try ManagedRuntimeConfiguration(environment: [:]))
        let value = try ManagedRuntimeConfiguration(environment: [
            "JANGOLOVA_CYMONKEY_CONTROL_URL": "ws://127.0.0.1:7394/bridge",
            "JANGOLOVA_CYMONKEY_CONTROL_TOKEN": "secret",
            "JANGOLOVA_CYMONKEY_PROTOCOL": "cymonkey/v1alpha1",
            "JANGOLOVA_CYMONKEY_CONFIG": "/tmp/cymonkey.json",
        ])
        XCTAssertEqual(value.endpoint.host, "127.0.0.1")
    }

    func testUserscriptCatalogStoresMetadataWithoutSource() async throws {
        let suite = "dev.jangolova.tests.\(UUID().uuidString)"
        let catalog = UserscriptCatalog(appGroup: suite)
        let item = UserscriptSummary(
            id: "example", name: "Example", revision: "sha256:fixture",
            enabled: false, matches: ["https://example.com/*"]
        )
        try await catalog.replace([item])
        let stored = try await catalog.list()
        XCTAssertEqual(stored, [item])
        let enabled = try await catalog.setEnabled(id: "example", enabled: true)
        XCTAssertTrue(enabled.enabled)
        UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite)
    }
}

import Foundation
import WebKit
import XCTest
@testable import MacOSWebViewScripts

@MainActor
final class DocumentScriptsTests: XCTestCase {
    func testInstallsOnlyReviewedOriginAtRequestedStage() throws {
        let controller = WKUserContentController()
        let installer = MacOSWebViewScriptInstaller(contentController: controller)
        let record = try installer.installApprovedScript(
            id: "reading-tools", source: "window.readingTools = true",
            allowedOrigins: [URL(string: "https://example.com")!],
            timing: .documentStart
        )
        XCTAssertEqual(record.origins, ["https://example.com"])
        XCTAssertEqual(record.timing, .documentStart)
        XCTAssertEqual(installer.installedScripts.count, 1)
        XCTAssertEqual(controller.userScripts.count, 1)
        XCTAssertEqual(controller.userScripts[0].injectionTime, .atDocumentStart)
        XCTAssertTrue(controller.userScripts[0].isForMainFrameOnly)
        XCTAssertTrue(controller.userScripts[0].source.contains("location.origin"))
        XCTAssertTrue(controller.userScripts[0].source.contains("window.readingTools = true"))
        try installer.installApprovedScript(
            id: "after-load", source: "window.afterLoad = true",
            allowedOrigins: [URL(string: "https://example.com")!],
            timing: .documentEnd
        )
        XCTAssertEqual(controller.userScripts[1].injectionTime, .atDocumentEnd)
    }

    func testRejectsPathScopedInputAndDoesNotTouchHostScripts() throws {
        let controller = WKUserContentController()
        let hostScript = WKUserScript(source: "window.host = true", injectionTime: .atDocumentEnd, forMainFrameOnly: true)
        controller.addUserScript(hostScript)
        let installer = MacOSWebViewScriptInstaller(contentController: controller)
        XCTAssertThrowsError(try installer.installApprovedScript(
            id: "bad", source: "window.bad = true",
            allowedOrigins: [URL(string: "https://example.com/private")!],
            timing: .documentEnd
        )) { error in
            XCTAssertEqual(error as? DocumentScriptError, .invalidOrigin)
        }
        XCTAssertEqual(controller.userScripts.count, 1)
        XCTAssertEqual(controller.userScripts[0].source, "window.host = true")
    }
}

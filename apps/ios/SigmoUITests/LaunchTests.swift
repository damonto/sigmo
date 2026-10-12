import XCTest

final class LaunchTests: XCTestCase {
    @MainActor
    func testLaunchAndWriteDiagnosticLog() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "empty"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launch()

        XCTAssertTrue(app.buttons["manageBackends"].waitForExistence(timeout: 10))
        app.buttons["manageBackends"].tap()
        let diagnostics = app.buttons["openDiagnostics"]
        XCTAssertTrue(diagnostics.waitForExistence(timeout: 10))
        diagnostics.tap()

        let bundleIdentifier = app.descendants(matching: .any)["bundleIdentifier"].firstMatch
        XCTAssertTrue(bundleIdentifier.waitForExistence(timeout: 5))
        XCTAssertEqual(bundleIdentifier.value as? String, "com.damon.sigmo")
        app.buttons["writeDiagnosticLog"].tap()
        let eventCount = app.descendants(matching: .any)["diagnosticEventCount"].firstMatch
        XCTAssertEqual(eventCount.value as? String, "1")

        let screenshot = XCTAttachment(screenshot: app.screenshot())
        screenshot.lifetime = .keepAlways
        add(screenshot)
    }
}

import XCTest

@MainActor
final class DemoFlowTests: XCTestCase {
    func testPublicEntryResetAndExit() {
        continueAfterFailure = false
        let app = XCUIApplication()
        // Only isolate the real connection store. Enter the production demo
        // using its public button, without a demo-specific launch switch.
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "empty"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchArguments = ["-AppleLanguages", "(en)", "-AppleLocale", "en_US"]
        app.launch()
        XCTAssertTrue(app.buttons["manageBackends"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["tryDemo"].exists)
        app.buttons["manageBackends"].tap()
        XCTAssertTrue(app.buttons["tryDemo"].waitForExistence(timeout: 5))
        app.buttons["tryDemo"].tap()
        XCTAssertTrue(app.buttons["demoOptions"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Demo Home"].waitForExistence(timeout: 10))
        app.buttons["demoOptions"].tap()
        app.buttons["demoReceiveMessage"].tap()
        app.buttons["demoOptions"].tap()
        app.buttons["resetDemo"].tap()
        XCTAssertTrue(app.staticTexts["Demo Home"].waitForExistence(timeout: 10))
        app.buttons["demoOptions"].tap()
        app.buttons["demoReceiveCall"].tap()
        XCTAssertTrue(app.buttons["calls-return"].waitForExistence(timeout: 10))
        app.buttons["calls-return"].tap()
        XCTAssertTrue(app.buttons["calls-answer"].waitForExistence(timeout: 5))
        app.buttons["calls-answer"].tap()
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 5))
        app.buttons["calls-end"].tap()
        XCTAssertTrue(app.buttons["exitDemo"].waitForExistence(timeout: 5))
        app.buttons["exitDemo"].tap()
        XCTAssertTrue(app.buttons["tryDemo"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Demo Home"].exists)
        app.buttons["tryDemo"].tap()
        XCTAssertTrue(app.staticTexts["Demo Home"].waitForExistence(timeout: 10))
        app.buttons["exitDemo"].tap()
    }
}

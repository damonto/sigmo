import XCTest

@MainActor
final class LaunchTests: XCTestCase {
    func testLaunchAndWriteDiagnosticLog() {
        continueAfterFailure = false
        let app = launch()
        openDiagnostics(app)

        let bundleIdentifier = app.descendants(matching: .any)["bundleIdentifier"].firstMatch
        XCTAssertTrue(bundleIdentifier.waitForExistence(timeout: 5))
        XCTAssertEqual(bundleIdentifier.value as? String, "com.damon.sigmo")
        let toggle = app.switches["diagnosticLoggingEnabled"]
        XCTAssertEqual(toggle.value as? String, "0")
        XCTAssertFalse(app.buttons["writeDiagnosticLog"].isEnabled)
        XCTAssertFalse(app.buttons["exportDiagnosticLogs"].isEnabled)
        XCTAssertFalse(app.buttons["clearDiagnosticLogs"].isEnabled)
        toggleLogging(in: app)
        app.buttons["writeDiagnosticLog"].tap()
        let eventCount = app.descendants(matching: .any)["diagnosticEventCount"].firstMatch
        expectValue("1", element: eventCount)

        let screenshot = XCTAttachment(screenshot: app.screenshot())
        screenshot.lifetime = .keepAlways
        add(screenshot)
    }

    func testLoggingSwitchAndSavedLogsSurviveRelaunch() {
        continueAfterFailure = false
        let app = launch()
        openDiagnostics(app)
        toggleLogging(in: app)
        app.buttons["writeDiagnosticLog"].tap()
        expectValue("1", element: app.descendants(matching: .any)["diagnosticEventCount"].firstMatch)
        app.terminate()
        app.launchEnvironment.removeValue(forKey: "SIGMO_UI_RESET")
        app.launch()
        openDiagnostics(app)
        XCTAssertEqual(app.switches["diagnosticLoggingEnabled"].value as? String, "1")
        XCTAssertTrue(app.buttons["exportDiagnosticLogs"].waitForExistence(timeout: 5))
        let saved = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: app.buttons["exportDiagnosticLogs"])
        XCTAssertEqual(XCTWaiter.wait(for: [saved], timeout: 5), .completed)
        toggleLogging(in: app)
        app.terminate()
        app.launch()
        openDiagnostics(app)
        XCTAssertEqual(app.switches["diagnosticLoggingEnabled"].value as? String, "0")
        XCTAssertFalse(app.buttons["writeDiagnosticLog"].isEnabled)
        let export = app.buttons["exportDiagnosticLogs"]
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: export)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
    }

    func testExportCancellationAndClearingInBothLanguages() {
        continueAfterFailure = false
        for language in ["en", "zh-Hans"] {
            let app = launch(language: language)
            openDiagnostics(app)
            XCTAssertTrue(app.navigationBars[language == "en" ? "Development diagnostics" : "开发诊断"].exists)
            toggleLogging(in: app)
            app.buttons["writeDiagnosticLog"].tap()
            expectValue("1", element: app.descendants(matching: .any)["diagnosticEventCount"].firstMatch)
            toggleLogging(in: app)
            tap(app.buttons["exportDiagnosticLogs"], in: app)
            let picker = app.navigationBars["FullDocumentManagerViewControllerNavigationBar"]
            let presented = picker.waitForExistence(timeout: 20)
            if !presented { print(app.debugDescription) }
            XCTAssertTrue(presented)
            let screenshot = XCTAttachment(screenshot: app.screenshot())
            screenshot.name = "diagnostic-export-\(language)"
            screenshot.lifetime = .keepAlways
            add(screenshot)
            let cancel = app.buttons.matching(NSPredicate(format: "label IN %@", ["Cancel", "取消"])).firstMatch
            if cancel.exists { cancel.tap() }
            else { picker.swipeDown() }
            let dismissed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "hittable == true"), object: app.buttons["clearDiagnosticLogs"])
            XCTAssertEqual(XCTWaiter.wait(for: [dismissed], timeout: 5), .completed)
            tap(app.buttons["clearDiagnosticLogs"], in: app)
            let alert = app.alerts.firstMatch
            XCTAssertTrue(alert.waitForExistence(timeout: 5))
            alert.buttons[language == "en" ? "Cancel" : "取消"].tap()
            expectValue("1", element: app.descendants(matching: .any)["diagnosticEventCount"].firstMatch)
            XCTAssertTrue(app.buttons["exportDiagnosticLogs"].isEnabled)
            tap(app.buttons["clearDiagnosticLogs"], in: app)
            XCTAssertTrue(alert.waitForExistence(timeout: 5))
            let confirm = alert.buttons["confirmClearDiagnosticLogs"].firstMatch
            XCTAssertTrue(confirm.waitForExistence(timeout: 5))
            confirm.tap()
            expectValue("0", element: app.descendants(matching: .any)["diagnosticEventCount"].firstMatch)
            XCTAssertEqual(app.switches["diagnosticLoggingEnabled"].value as? String, "0")
            XCTAssertFalse(app.buttons["exportDiagnosticLogs"].isEnabled)
            app.terminate()
        }
    }

    private func launch(language: String = "en") -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "empty"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchArguments = ["-AppleLanguages", "(\(language))", "-AppleLocale", language == "en" ? "en_US" : "zh_CN"]
        app.launch()
        return app
    }

    private func openDiagnostics(_ app: XCUIApplication) {
        XCTAssertTrue(app.buttons["manageBackends"].waitForExistence(timeout: 10))
        app.buttons["manageBackends"].tap()
        let diagnostics = app.buttons["openDiagnostics"]
        XCTAssertTrue(diagnostics.waitForExistence(timeout: 10))
        tap(diagnostics, in: app)
        XCTAssertTrue(app.switches["diagnosticLoggingEnabled"].waitForExistence(timeout: 5))
    }

    private func toggleLogging(in app: XCUIApplication) {
        let row = app.switches["diagnosticLoggingEnabled"]
        let next = (row.value as? String) == "1" ? "0" : "1"
        tap(row.switches.firstMatch, in: app)
        expectValue(next, element: row)
    }

    private func tap(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<4 {
            if element.isHittable { break }
            app.swipeUp()
        }
        element.tap()
    }

    private func expectValue(_ value: String, element: XCUIElement) {
        let match = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: element)
        let result = XCTWaiter.wait(for: [match], timeout: 5)
        if result != .completed { print(XCUIApplication().debugDescription) }
        XCTAssertEqual(result, .completed, "Expected \(value), received \(String(describing: element.value))")
    }
}

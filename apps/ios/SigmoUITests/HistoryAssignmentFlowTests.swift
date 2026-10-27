import XCTest

@MainActor
final class HistoryAssignmentFlowTests: XCTestCase {
    private func launch(candidates: Int, chinese: Bool = false, retry: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = retry ? "communication-assignment-retry" : "communication-assignment-\(candidates)"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = chinese ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US",
                               "-UIPreferredContentSizeCategoryName", chinese ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let messages = app.tabBars.buttons[chinese ? "短信" : "Messages"]
        XCTAssertTrue(messages.waitForExistence(timeout: 10))
        messages.tap()
        let source = app.buttons["historyAssignmentSource-deleted"]
        XCTAssertTrue(source.waitForExistence(timeout: 10), app.debugDescription)
        source.tap()
        XCTAssertTrue(app.navigationBars[chinese ? "接管历史记录" : "Assign history"].waitForExistence(timeout: 5))
        return app
    }

    private func capture(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    func testMultipleCandidatesRequireSelectionAndAllowInactiveTarget() {
        let app = launch(candidates: 2)
        let work = app.buttons["historyAssignmentLine-work"]
        let backup = app.buttons["historyAssignmentLine-backup"]
        let confirm = app.buttons["historyAssignmentConfirm"]
        XCTAssertTrue(work.waitForExistence(timeout: 5))
        XCTAssertTrue(backup.isEnabled)
        XCTAssertTrue(backup.label.contains("Inactive"))
        XCTAssertFalse(app.descendants(matching: .any)["historyAssignmentLine-foreign"].exists)
        XCTAssertFalse(confirm.isEnabled)
        capture(app, name: "History assignment native list")

        backup.tap()
        XCTAssertTrue(confirm.isEnabled)
        XCTAssertTrue(confirm.label.contains("Backup"))
        capture(app, name: "History assignment inactive selection")
        confirm.tap()
        XCTAssertTrue(app.navigationBars["Assign history"].waitForNonExistence(timeout: 10))
        XCTAssertTrue(app.buttons["historyAssignmentSource-deleted"].waitForNonExistence(timeout: 5))
    }

    func testSingleCandidateNeedsOnlyConfirmation() {
        let app = launch(candidates: 1)
        XCTAssertTrue(app.descendants(matching: .any)["historyAssignmentLine-work"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["historyAssignmentLine-work"].exists)
        let confirm = app.buttons["historyAssignmentConfirm"]
        XCTAssertTrue(confirm.isEnabled)
        XCTAssertTrue(confirm.label.contains("Work"))
        app.buttons["historyAssignmentLater"].tap()
        XCTAssertTrue(app.navigationBars["Assign history"].waitForNonExistence(timeout: 5))
    }

    func testNoCandidatesKeepsHistoryAvailableForLater() {
        let app = launch(candidates: 0)
        XCTAssertTrue(app.descendants(matching: .any)["historyAssignmentEmpty"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.descendants(matching: .any)["historyAssignmentLine-foreign"].exists)
        XCTAssertFalse(app.buttons["historyAssignmentConfirm"].isEnabled)
        XCTAssertTrue(app.buttons["historyAssignmentLater"].isEnabled)
        capture(app, name: "History assignment no local candidates")
        app.buttons["historyAssignmentLater"].tap()
        XCTAssertTrue(app.navigationBars["Assign history"].waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["historyAssignmentSource-deleted"].waitForExistence(timeout: 5))
    }

    func testChineseDarkLargeTextKeepsSelectionAndConfirmationReachable() {
        let app = launch(candidates: 2, chinese: true)
        let backup = app.buttons["historyAssignmentLine-backup"]
        tapLine(backup, in: app)
        XCTAssertTrue(backup.label.contains("未启用"))
        let confirm = app.buttons["historyAssignmentConfirm"]
        let enabled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: confirm)
        XCTAssertEqual(XCTWaiter.wait(for: [enabled], timeout: 5), .completed, app.debugDescription)
        XCTAssertTrue(confirm.isHittable)
        XCTAssertTrue(confirm.label.contains("接管到「Backup」"))
        XCTAssertFalse(app.descendants(matching: .any)["historyAssignmentLine-foreign"].exists)
        capture(app, name: "History assignment Chinese dark large text")
        app.buttons["historyAssignmentLater"].tap()
        XCTAssertTrue(app.navigationBars["接管历史记录"].waitForNonExistence(timeout: 5))
    }

    private func tapLine(_ line: XCUIElement, in app: XCUIApplication) {
        let list = app.collectionViews["history-assignment-deleted"]
        let note = app.staticTexts["historyAssignmentPreserved"]
        for _ in 0..<8 {
            // List's frame extends under the fixed footer; keep drags in its visible content.
            let top = list.frame.minY + 12
            let bottom = note.frame.minY - 12
            let frame = app.frame
            let viewport = CGRect(x: frame.minX, y: top, width: frame.width, height: bottom - top)
            if line.exists, line.isHittable {
                let visible = line.frame.intersection(viewport)
                if !visible.isNull, visible.height >= 44 {
                    app.coordinate(withNormalizedOffset: CGVector(dx: (visible.midX - frame.minX) / frame.width,
                                                                 dy: (visible.midY - frame.minY) / frame.height)).tap()
                    return
                }
            }
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (top + (bottom - top) * 0.75 - frame.minY) / frame.height))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (top + (bottom - top) * 0.25 - frame.minY) / frame.height))
            start.press(forDuration: 0.1, thenDragTo: end)
        }
        XCTFail("Cannot reach line: \(line)\n\(app.debugDescription)")
    }

    func testReloadAfterFailureCompletesAndRestoresConfirmation() {
        let app = launch(candidates: 1, retry: true)
        let reload = app.buttons["historyAssignmentRetry"]
        XCTAssertTrue(reload.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["historyAssignmentConfirm"].isEnabled)
        XCTAssertFalse(app.descendants(matching: .any)["historyAssignmentEmpty"].exists)
        reload.tap()
        XCTAssertTrue(app.descendants(matching: .any)["historyAssignmentLine-work"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["historyAssignmentConfirm"].isEnabled)
        app.buttons["historyAssignmentLater"].tap()
        XCTAssertTrue(app.navigationBars["Assign history"].waitForNonExistence(timeout: 5))
    }
}

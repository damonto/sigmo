import XCTest

@MainActor
final class SettingsFlowTests: XCTestCase {
    private func app(chinese: Bool = false, largeText: Bool = false, dark: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "settings"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        if dark { app.launchEnvironment["SIGMO_UI_APPEARANCE"] = "dark" }
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", largeText ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        openSettings(app)
        screenshot(app)
        return app
    }

    private func openSettings(_ app: XCUIApplication) {
        // Returning from Settings now leaves us in the connection list.
        let row = app.buttons["backend-00000000-0000-0000-0000-000000000001"]
        if !row.exists {
            let menu = app.buttons["manageBackends"]
            XCTAssertTrue(menu.waitForExistence(timeout: 10))
            menu.tap()
        }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.swipeLeft()
        app.buttons["settingsBackend-00000000-0000-0000-0000-000000000001"].tap()
        XCTAssertTrue(app.buttons["settings-auth"].waitForExistence(timeout: 5))
    }

    private func tap(_ element: XCUIElement, app: XCUIApplication) {
        scrollTo(element, app: app)
        element.tap()
    }
    private func scrollTo(_ element: XCUIElement, app: XCUIApplication) {
        for _ in 0..<10 {
            if element.exists && element.isHittable { return }
            app.swipeUp()
        }
        screenshot(app)
        XCTAssertTrue(element.isHittable, app.debugDescription)
    }
    private func replace(_ field: XCUIElement, text: String) {
        field.tap()
        // A trailing-aligned field can place the caret before its number.
        field.doubleTap()
        field.typeText(text)
        XCTAssertEqual(field.value as? String, text)
    }
    private func back(_ app: XCUIApplication) { app.navigationBars.buttons.element(boundBy: 0).tap() }
    private func screenshot(_ app: XCUIApplication) {
        let capture = app.screenshot()
        let attachment = XCTAttachment(screenshot: capture); attachment.lifetime = .keepAlways; add(attachment)
        let path = FileManager.default.temporaryDirectory.appendingPathComponent("settings-\(UUID().uuidString).png")
        do {
            try capture.pngRepresentation.write(to: path)
            print("SETTINGS_SCREENSHOT \(path.path)")
        } catch { XCTFail("Save screenshot: \(error)") }
    }

    func testAuthenticationAndProxySave() {
        let app = app()
        app.buttons["settings-auth"].tap()
        XCTAssertTrue(app.textFields["tokenValidityDays"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["tokenValidityDays"].label, "Token validity (days)")
        replace(app.textFields["tokenValidityDays"], text: "45")
        XCTAssertEqual(app.textFields["tokenValidityDays"].value as? String, "45")
        tap(app.buttons["test-auth"], app: app)
        tap(app.buttons["save-settings"], app: app)
        scrollTo(app.staticTexts["Settings saved."], app: app)
        screenshot(app)
        back(app)
        back(app)
        openSettings(app)
        app.buttons["settings-auth"].tap()
        XCTAssertTrue(app.textFields["tokenValidityDays"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["tokenValidityDays"].value as? String, "45")
        back(app)
        app.buttons["settings-proxy"].tap()
        XCTAssertTrue(app.textFields["httpPort"].waitForExistence(timeout: 5))
        replace(app.textFields["httpPort"], text: "9090")
        tap(app.buttons["save-settings"], app: app)
        back(app)
        app.buttons["settings-proxy"].tap()
        XCTAssertEqual(app.textFields["httpPort"].value as? String, "9090")
        screenshot(app)
    }

    func testNotificationChannelsAndHeaders() {
        let app = app()
        app.buttons["settings-notifications"].tap()
        XCTAssertTrue(app.buttons["channel-telegram"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Web Push"].exists)
        tap(app.buttons["channel-http"], app: app)
        XCTAssertTrue(app.textFields["endpoint"].waitForExistence(timeout: 5))
        app.switches["channel-enabled"].switches.firstMatch.tap()
        app.textFields["endpoint"].tap()
        app.textFields["endpoint"].typeText("https://notify.example.test")
        tap(app.buttons["add-header"], app: app)
        app.textFields["header-name"].tap(); app.textFields["header-name"].typeText("X-Sigmo")
        app.textFields["header-value"].tap(); app.textFields["header-value"].typeText("fixture")
        app.buttons["save-header"].tap()
        tap(app.buttons["save-settings"], app: app)
        screenshot(app)
        back(app)
        tap(app.buttons["channel-http"], app: app)
        XCTAssertEqual(app.textFields["endpoint"].value as? String, "https://notify.example.test")
        XCTAssertTrue(app.buttons.containing(.staticText, identifier: "X-Sigmo").firstMatch.exists)
    }

    func testDeletingRecipientPreservesRemainingRow() {
        let app = app()
        app.buttons["settings-notifications"].tap()
        tap(app.buttons["channel-telegram"], app: app)
        let recipients = app.textFields.matching(NSPredicate(format: "identifier BEGINSWITH %@", "recipients-"))
        XCTAssertTrue(recipients.firstMatch.waitForExistence(timeout: 5))
        let originalID = recipients.firstMatch.identifier
        tap(app.buttons["add-recipients"], app: app)
        let added = recipients.element(boundBy: 1)
        XCTAssertTrue(added.waitForExistence(timeout: 5))
        let addedID = added.identifier
        added.tap()
        added.typeText("654321")
        tap(app.buttons["delete-" + originalID], app: app)
        XCTAssertEqual(recipients.count, 1)
        XCTAssertEqual(recipients.firstMatch.identifier, addedID)
        replace(recipients.firstMatch, text: "987654")
        tap(app.buttons["save-settings"], app: app)
        back(app)
        tap(app.buttons["channel-telegram"], app: app)
        XCTAssertEqual(recipients.firstMatch.value as? String, "987654")
    }

    func testMCPKeyCreateCopyRevokeAndExport() {
        let app = app()
        app.buttons["settings-mcp"].tap()
        XCTAssertTrue(app.switches["mcp-enabled"].waitForExistence(timeout: 5))
        app.switches["mcp-enabled"].switches.firstMatch.tap()
        tap(app.buttons["create-mcp-key"], app: app)
        app.textFields["mcp-key-name"].tap(); app.textFields["mcp-key-name"].typeText("Test Agent")
        tap(app.buttons["mcp-all-permissions"], app: app)
        tap(app.buttons["submit-mcp-key"], app: app)
        XCTAssertTrue(app.staticTexts["mcp-secret"].waitForExistence(timeout: 5))
        tap(app.buttons["settings.native.copySecret"], app: app)
        screenshot(app)
        app.buttons["Done"].tap()
        XCTAssertFalse(app.staticTexts["mcp-secret"].exists)
        tap(app.buttons["key-fixture-key-1"], app: app)
        tap(app.buttons["revoke-fixture-key-1"], app: app)
        app.buttons["confirm-revoke-key"].firstMatch.tap()
        let status = app.descendants(matching: .any)["mcp-key-status"].firstMatch
        XCTAssertTrue(status.waitForExistence(timeout: 5))
        XCTAssertTrue(status.label.contains("Revoked") || (status.value as? String) == "Revoked")
        back(app)
        for _ in 0..<8 where !app.buttons["download-skill"].isHittable { app.swipeDown() }
        tap(app.buttons["download-skill"], app: app)
        screenshot(app)
        let exported = app.buttons["Save"].waitForExistence(timeout: 20)
        screenshot(app)
        XCTAssertTrue(exported)
        app.navigationBars.firstMatch.swipeDown()
        XCTAssertTrue(app.buttons["download-skill"].waitForExistence(timeout: 5))
    }

    func testUpdatesCheckInstallAndRestart() {
        let app = app()
        app.buttons["settings-updates"].tap()
        XCTAssertTrue(app.buttons["check-updates"].waitForExistence(timeout: 5))
        tap(app.buttons["check-updates"], app: app)
        let version = app.descendants(matching: .any)["latest-update-version"].firstMatch
        XCTAssertTrue(version.waitForExistence(timeout: 5))
        XCTAssertTrue(version.label.contains("1.1.0") || (version.value as? String) == "1.1.0")
        tap(app.buttons["install-update"], app: app)
        XCTAssertTrue(app.descendants(matching: .any)["up-to-date"].firstMatch.waitForExistence(timeout: 20))
        screenshot(app)
    }

    func testChineseLargeTextAndDarkSettings() throws {
        try auditLargeTextSettings(chinese: true)
    }

    func testEnglishLargeTextAndDarkSettings() throws {
        try auditLargeTextSettings(chinese: false)
    }

    private func auditLargeTextSettings(chinese: Bool) throws {
        let language = chinese ? "zh-Hans" : "en"
        for page in ["settings-auth", "settings-proxy", "settings-notifications", "settings-mcp", "settings-updates"] {
            // Give each audit fresh fixture and navigation state.
            print("SETTINGS_AUDIT_BEGIN \(language) \(page)")
            let app = app(chinese: chinese, largeText: true, dark: true)
            tap(app.buttons[page], app: app)
            let ready: XCUIElement
            switch page {
            case "settings-auth": ready = app.textFields["tokenValidityDays"]
            case "settings-proxy": ready = app.textFields["listenAddress"]
            case "settings-notifications": ready = app.buttons["channel-telegram"]
            case "settings-mcp": ready = app.switches["mcp-enabled"]
            default: ready = app.switches["automatic-updates"]
            }
            XCTAssertTrue(ready.waitForExistence(timeout: 5), "\(language) \(page) did not load")
            if page == "settings-auth" {
                let label = chinese ? "登录有效天数" : "Token validity (days)"
                XCTAssertEqual(ready.label, label)
                XCTAssertEqual(app.staticTexts["label-tokenValidityDays"].label, label)
            }
            screenshot(app)
            try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
                print("SETTINGS_AUDIT \(language) \(page) \(issue.detailedDescription) \(issue.element?.debugDescription ?? "No element")")
                return false
            }
            print("SETTINGS_AUDIT_PASS \(language) \(page)")
            app.terminate()
        }
    }
}

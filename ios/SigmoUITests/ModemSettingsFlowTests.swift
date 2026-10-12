import XCTest

@MainActor
final class ModemSettingsFlowTests: XCTestCase {
    private func launch(_ scenario: String = "detail-settings", chinese: Bool = false,
                        large: Bool = false, dark: Bool = false, waitForLoad: Bool = true) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = scenario
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = dark ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US",
                               "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 10))
        modem.tap()
        let settings = app.tabBars.buttons[chinese ? "设置" : "Settings"]
        XCTAssertTrue(settings.waitForExistence(timeout: 10))
        settings.tap()
        if waitForLoad { waitEnabled(app.buttons["modemDeviceSettings"]) }
        return app
    }

    private func waitEnabled(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND enabled == true"), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, file: file, line: line)
    }

    private func tap(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<10 {
            if element.waitForExistence(timeout: 1), element.isHittable { break }
            app.swipeUp()
        }
        XCTAssertTrue(element.isHittable)
        element.tap()
    }

    private func back(_ app: XCUIApplication) {
        app.navigationBars.buttons.element(boundBy: 0).tap()
        for _ in 0..<10 {
            if app.switches["modemAirplaneMode"].waitForExistence(timeout: 1) { break }
            app.swipeDown()
        }
        XCTAssertTrue(app.switches["modemAirplaneMode"].waitForExistence(timeout: 5))
    }

    private func replace(_ field: XCUIElement, with value: String) {
        waitEnabled(field)
        field.tap()
        // Select all so replacement does not depend on the tap's caret position.
        field.typeKey("a", modifierFlags: .command)
        field.typeText(XCUIKeyboardKey.delete.rawValue + value)
        if !value.isEmpty { expectValue(field, value) }
    }

    private func expectValue(_ element: XCUIElement, _ value: String) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed)
    }

    private func expectTitle(_ app: XCUIApplication, _ title: String) {
        // Native toolbar menus can merge their text into one accessibility element.
        XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 10), app.debugDescription)
    }

    private func expectSelected(_ element: XCUIElement, _ selected: Bool) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == %@", NSNumber(value: selected)), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed)
    }

    private func screenshot(_ app: XCUIApplication, name: String) {
        let image = app.screenshot()
        let attachment = XCTAttachment(screenshot: image)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        let file = URL.temporaryDirectory.appending(path: "ModemSettings-" + name.replacingOccurrences(of: " ", with: "-") + ".png")
        do { try image.pngRepresentation.write(to: file); print("MODEM_SETTINGS_SCREENSHOT " + file.path) }
        catch { XCTFail("Save settings screenshot: \(error)") }
    }

    func testAirplaneModeUpdatesImmediatelyAndDisablesNetworkRows() {
        let app = launch()
        let airplane = app.switches["modemAirplaneMode"].switches.firstMatch
        waitEnabled(airplane)
        screenshot(app, name: "Settings overview")
        airplane.tap()
        expectValue(airplane, "1")
        waitEnabled(airplane)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["modemNetworkMode"].isEnabled)
        XCTAssertFalse(app.buttons["modemFrequencyBands"].isEnabled)
        XCTAssertTrue(app.buttons["modemDeviceSettings"].isEnabled)
        screenshot(app, name: "Airplane mode on")
        airplane.tap()
        expectValue(airplane, "0")
        waitEnabled(app.buttons["modemNetworkMode"])
        XCTAssertTrue(app.buttons["modemFrequencyBands"].isEnabled)
    }

    func testNetworkModeAppliesOnSelectionAndKeepsPageOpen() {
        let app = launch()
        tap(app.buttons["modemNetworkMode"], in: app)
        let mode = app.buttons["modemMode-4-0"]
        waitEnabled(mode)
        mode.tap()
        expectSelected(mode, true)
        XCTAssertFalse(mode.isEnabled)
        XCTAssertTrue(app.navigationBars["Network Mode"].exists)
        screenshot(app, name: "Network mode")
        back(app)
        tap(app.buttons["modemNetworkMode"], in: app)
        expectSelected(app.buttons["modemMode-4-0"], true)
    }

    func testBandsDiscardDraftAndApplyOnlyWhenDone() {
        let app = launch()
        tap(app.buttons["modemFrequencyBands"], in: app)
        let lte = app.buttons["modemBand-4-1"]
        waitEnabled(lte)
        XCTAssertFalse(app.buttons["saveModemBands"].isEnabled)
        lte.tap()
        expectSelected(lte, false)
        back(app)
        tap(app.buttons["modemFrequencyBands"], in: app)
        waitEnabled(app.buttons["modemBand-4-1"])
        expectSelected(app.buttons["modemBand-4-1"], true)
        for id in ["modemBand-4-1", "modemBand-4-3", "modemBand-64-78"] { tap(app.buttons[id], in: app) }
        expectSelected(app.buttons["modemBand-64-1"], true)
        screenshot(app, name: "Frequency bands")
        tap(app.buttons["saveModemBands"], in: app)
        XCTAssertTrue(app.staticTexts["1 selected"].waitForExistence(timeout: 10))
        tap(app.buttons["modemFrequencyBands"], in: app)
        waitEnabled(app.buttons["modemBand-64-1"])
        expectSelected(app.buttons["modemBand-4-1"], false)
        expectSelected(app.buttons["modemBand-64-1"], true)
        app.buttons["modemBand-64-1"].tap()
        XCTAssertFalse(app.buttons["saveModemBands"].isEnabled)
        XCTAssertTrue(app.staticTexts["Select at least one frequency band."].exists)
    }

    func testAliasAndMSSSaveTogetherAndUpdateDeviceTitle() {
        let app = launch()
        XCTAssertTrue(app.buttons["modemDeviceSettings"].label.contains("General"))
        tap(app.buttons["modemDeviceSettings"], in: app)
        XCTAssertTrue(app.navigationBars["General"].waitForExistence(timeout: 5))
        replace(app.textFields["modemAlias"], with: "Office Router")
        replace(app.textFields["modemMSS"], with: "63")
        expectValue(app.textFields["modemMSS"], "63")
        XCTAssertFalse(app.buttons["saveModemDevice"].isEnabled)
        screenshot(app, name: "Invalid MSS")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(
            format: "label CONTAINS %@", "Enter a whole number from 64 to 254."
        )).firstMatch.exists)
        replace(app.textFields["modemMSS"], with: "128")
        screenshot(app, name: "Device settings")
        tap(app.buttons["saveModemDevice"], in: app)
        expectTitle(app, "Office Router")
        tap(app.buttons["modemDeviceSettings"], in: app)
        expectValue(app.textFields["modemAlias"], "Office Router")
        expectValue(app.textFields["modemMSS"], "128")
        replace(app.textFields["modemAlias"], with: "")
        tap(app.buttons["saveModemDevice"], in: app)
        expectTitle(app, "Home Modem")
    }

    func testFailedSavePreservesInputForExplicitRetry() {
        let app = launch("detail-settings-write-error")
        tap(app.buttons["modemDeviceSettings"], in: app)
        replace(app.textFields["modemAlias"], with: "Retry Name")
        replace(app.textFields["modemMSS"], with: "128")
        tap(app.buttons["saveModemDevice"], in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        screenshot(app, name: "Device settings error")
        app.alerts.buttons.firstMatch.tap()
        expectValue(app.textFields["modemAlias"], "Retry Name")
        expectValue(app.textFields["modemMSS"], "128")
        tap(app.buttons["saveModemDevice"], in: app)
        expectTitle(app, "Retry Name")
    }

    func testDeviceDraftSurvivesForegroundRefreshAndBackDiscardsIt() {
        let app = launch()
        tap(app.buttons["modemDeviceSettings"], in: app)
        replace(app.textFields["modemAlias"], with: "Unsaved Name")
        replace(app.textFields["modemMSS"], with: "128")
        XCUIDevice.shared.press(.home)
        app.activate()
        waitEnabled(app.textFields["modemAlias"])
        expectValue(app.textFields["modemAlias"], "Unsaved Name")
        expectValue(app.textFields["modemMSS"], "128")
        back(app)
        tap(app.buttons["modemDeviceSettings"], in: app)
        waitEnabled(app.textFields["modemAlias"])
        XCTAssertNotEqual(app.textFields["modemAlias"].value as? String, "Unsaved Name")
        expectValue(app.textFields["modemMSS"], "240")
    }

    func testFailedAirplaneSwitchRestoresConfirmedValue() {
        let app = launch("detail-settings-write-error")
        let airplane = app.switches["modemAirplaneMode"].switches.firstMatch
        waitEnabled(airplane)
        airplane.tap()
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        app.alerts.buttons.firstMatch.tap()
        expectValue(airplane, "0")
        waitEnabled(airplane)
        airplane.tap()
        expectValue(airplane, "1")
        waitEnabled(airplane)
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }

    func testPartialFailureAndUnsupportedControlsRemainUsable() {
        var app = launch("detail-settings-unavailable")
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 10))
        XCTAssertEqual(alert.buttons.count, 1)
        XCTAssertFalse(alert.buttons["Retry"].exists)
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["modemNetworkMode"].isEnabled)
        XCTAssertFalse(app.buttons["modemFrequencyBands"].isEnabled)
        XCTAssertFalse(app.buttons["retryModemSettings-bands"].exists)
        screenshot(app, name: "Partial settings failure")
        tap(app.buttons["modemDeviceSettings"], in: app)
        XCTAssertTrue(alert.waitForExistence(timeout: 10))
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        waitEnabled(app.textFields["modemAlias"])
        app.terminate()
        app = launch("detail-settings-unsupported")
        XCTAssertFalse(app.switches["modemAirplaneMode"].isEnabled)
        XCTAssertTrue(app.staticTexts["This modem does not support Airplane Mode control."].exists)
        tap(app.buttons["modemNetworkMode"], in: app)
        XCTAssertTrue(app.buttons["modemMode-4-0"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["modemMode-4-0"].isEnabled)
        expectSelected(app.buttons["modemMode-4-0"], true)
    }

    func testLoadingPlaceholdersAreReplacedBySettings() {
        let app = launch("detail-settings-loading", waitForLoad: false)
        let loading = app.descendants(matching: .any)["Loading modem settings"].firstMatch
        XCTAssertTrue(loading.waitForExistence(timeout: 2))
        screenshot(app, name: "Loading settings")
        waitEnabled(app.buttons["modemDeviceSettings"])
        XCTAssertFalse(loading.exists)
        waitEnabled(app.switches["modemAirplaneMode"])
    }

    func testChineseDarkAppearanceAndAccessibilityText() {
        let app = launch(chinese: true, large: true, dark: true, waitForLoad: false)
        waitEnabled(app.switches["modemAirplaneMode"])
        XCTAssertTrue(app.switches["modemAirplaneMode"].exists)
        screenshot(app, name: "Chinese large dark overview")
        tap(app.buttons["modemNetworkMode"], in: app)
        waitEnabled(app.buttons["modemMode-4-0"])
        screenshot(app, name: "Chinese large network mode")
        back(app)
        tap(app.buttons["modemFrequencyBands"], in: app)
        waitEnabled(app.buttons["modemBand-4-1"])
        screenshot(app, name: "Chinese large frequency bands")
        back(app)
        tap(app.buttons["modemDeviceSettings"], in: app)
        waitEnabled(app.textFields["modemAlias"])
        screenshot(app, name: "Chinese large device settings")
        XCTAssertTrue(app.buttons["saveModemDevice"].exists)
    }
}

import XCTest

@MainActor
final class ConnectionFlowTests: XCTestCase {
    private func launch(_ scenario: String = "detail", chinese: Bool = false, large: Bool = false,
                        profile: String = "se-a:8986000000000000001") -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment = ["SIGMO_UI_SCENARIO": scenario, "SIGMO_UI_RESET": "1"]
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US",
                               "-AppleInterfaceStyle", large ? "Dark" : "Light",
                               "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        tap(app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch, in: app)
        tap(app.buttons["profileActions-" + profile], in: app)
        return app
    }

    private func tap(_ element: XCUIElement, in app: XCUIApplication) {
        reveal(element, in: app)
        element.tap()
    }

    private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<12 {
            if element.waitForExistence(timeout: 1), element.isHittable { break }
            app.swipeUp()
        }
        XCTAssertTrue(element.isHittable, app.debugDescription)
    }

    private func scrollToTop(_ app: XCUIApplication) {
        for _ in 0..<6 {
            if app.switches["internetConnected"].isHittable { break }
            app.swipeDown()
        }
    }

    private func toggle(_ id: String, in app: XCUIApplication) {
        let row = app.switches[id]
        reveal(row, in: app)
        let control = row.switches.firstMatch.exists ? row.switches.firstMatch : row
        tap(control, in: app)
    }

    private func expectToggle(_ id: String, value: String, in app: XCUIApplication) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: app.switches[id])
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, app.debugDescription)
    }

    private func back(_ app: XCUIApplication) { app.navigationBars.buttons.element(boundBy: 0).tap() }

    private func replace(_ field: XCUIElement, with text: String, in app: XCUIApplication) {
        tap(field, in: app)
        // Trailing-aligned fields can put the caret before the text on a center tap.
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.98, dy: 0.5)).tap()
        let current = field.value as? String ?? ""
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: current.count) + text)
        XCTAssertEqual(field.value as? String, text)
    }

    private func capture(_ app: XCUIApplication, name: String) {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        #if targetEnvironment(simulator)
        let directory = URL(fileURLWithPath: "/tmp/sigmo-profile-connection-screenshots", isDirectory: true)
        #else
        let directory = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
        #endif
        let file = directory.appendingPathComponent(name + ".png")
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try screenshot.pngRepresentation.write(to: file)
            print("CONNECTION_SCREENSHOT: " + file.path)
        } catch { XCTFail("Save connection screenshot: \(error)") }
    }

    private func audit(_ app: XCUIApplication, name: String) throws {
        capture(app, name: name)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            print("CONNECTION_AUDIT \(name): \(issue.compactDescription): \(issue.detailedDescription)")
            print(issue.element?.debugDescription ?? "No element identified")
            return false
        }
    }

    func testNativeNetworkSelectionAndUncommittedReturn() {
        let app = launch("detail-connection-scan")
        XCTAssertTrue(app.buttons["profileNetworkSelection"].label.contains("Example Mobile"))
        tap(app.buttons["profileNetworkSelection"], in: app)
        expectToggle("networkAutomatic", value: "1", in: app)
        toggle("networkAutomatic", in: app)
        XCTAssertTrue(app.buttons["networkOperator-23410"].waitForExistence(timeout: 10))
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'networkOperator-'" )).count, 2)
        capture(app, name: "Network selection list")
        back(app)
        tap(app.buttons["profileNetworkSelection"], in: app)
        expectToggle("networkAutomatic", value: "1", in: app)
        toggle("networkAutomatic", in: app)
        tap(app.buttons["networkOperator-23410"], in: app)
        expectToggle("networkAutomatic", value: "0", in: app)
        let selected = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true"), object: app.buttons["networkOperator-23410"])
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 10), .completed)
        back(app)
        let summary = app.buttons["profileNetworkSelection"]
        let updated = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label CONTAINS 'Travel Mobile'"), object: summary)
        XCTAssertEqual(XCTWaiter.wait(for: [updated], timeout: 10), .completed)
        tap(summary, in: app)
        toggle("networkAutomatic", in: app)
        expectToggle("networkAutomatic", value: "1", in: app)
    }

    func testInternetDraftOptionsConnectionAndProxyDetails() {
        let app = launch()
        tap(app.buttons["profileInternet"], in: app)
        toggle("internetConnected", in: app)
        expectToggle("internetConnected", value: "0", in: app)
        XCTAssertFalse(app.descendants(matching: .any)["internetInterface"].exists)
        tap(app.buttons["internetAPN"], in: app)
        replace(app.textFields["internetAPNField"], with: "native.apn", in: app)
        replace(app.textFields["internetUsername"], with: "alice", in: app)
        back(app)
        XCTAssertFalse(app.buttons["internetOptions"].exists)
        XCTAssertFalse(app.buttons["internetDetails"].exists)
        toggle("internetDefaultRoute", in: app)
        expectToggle("internetDefaultRoute", value: "1", in: app)
        toggle("internetAlwaysOn", in: app)
        expectToggle("internetAlwaysOn", value: "0", in: app)
        capture(app, name: "Internet main page connection options")
        scrollToTop(app)
        tap(app.buttons["internetAPN"], in: app)
        XCTAssertEqual(app.textFields["internetAPNField"].value as? String, "native.apn")
        XCTAssertEqual(app.textFields["internetUsername"].value as? String, "alice")
        back(app)
        toggle("internetConnected", in: app)
        expectToggle("internetConnected", value: "1", in: app)
        tap(app.buttons["internetAPN"], in: app)
        XCTAssertFalse(app.textFields["internetAPNField"].isEnabled)
        back(app)
        reveal(app.descendants(matching: .any)["internetInterface"].firstMatch, in: app)
        XCTAssertEqual(app.descendants(matching: .any)["internetInterface"].firstMatch.value as? String, "wwan0")
        capture(app, name: "Internet main page connection details")
        tap(app.buttons["copyHTTPProxy"], in: app)
        XCTAssertTrue(app.buttons["copySOCKSProxy"].exists)
        capture(app, name: "Internet proxy details")
    }

    func testConnectedPreferenceFailureKeepsConfirmedSwitch() {
        let app = launch("detail-connection-write-error")
        tap(app.buttons["profileInternet"], in: app)
        toggle("internetDefaultRoute", in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        app.alerts.buttons["OK"].tap()
        expectToggle("internetDefaultRoute", value: "0", in: app)
        capture(app, name: "Connection preference rollback")
    }

    func testWiFiCallingConfirmationCarrierPagesAndSessions() {
        let app = launch("detail-connection-setup")
        tap(app.buttons["profileWiFiCalling"], in: app)
        toggle("wifiCallingEnabled", in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        app.alerts.buttons["Cancel"].tap()
        expectToggle("wifiCallingEnabled", value: "0", in: app)
        toggle("wifiCallingEnabled", in: app)
        tap(app.alerts.buttons["confirmWiFiCalling"].firstMatch, in: app)
        let web = app.webViews.firstMatch
        XCTAssertTrue(web.waitForExistence(timeout: 10))
        tap(web.buttons["Continue Setup"], in: app)
        XCTAssertTrue(web.exists)
        capture(app, name: "Native carrier setup sheet")
        tap(web.buttons["Complete Setup"], in: app)
        XCTAssertTrue(web.waitForNonExistence(timeout: 10))
        expectToggle("wifiCallingEnabled", value: "1", in: app)
        tap(app.buttons["wifiCallingDisconnect"], in: app)
        expectToggle("wifiCallingEnabled", value: "1", in: app)
        XCTAssertTrue(app.staticTexts["Disconnected"].waitForExistence(timeout: 10))
        tap(app.buttons["wifiCallingReconnect"], in: app)
        XCTAssertTrue(app.staticTexts["Connected"].waitForExistence(timeout: 10))
        tap(app.buttons["wifiCallingUplink"], in: app)
        tap(app.buttons["wifiUplink-system"], in: app)
        back(app)
        tap(app.buttons["wifiCallingEmergencyAddress"], in: app)
        XCTAssertTrue(web.waitForExistence(timeout: 10))
        tap(app.buttons["closeWiFiWebsheet"], in: app)
        XCTAssertTrue(web.waitForNonExistence(timeout: 10))
        expectToggle("wifiCallingEnabled", value: "1", in: app)
    }

    func testVoLTEControlsAndDisabledDataPathEditing() {
        let app = launch()
        tap(app.buttons["profileVoLTE"], in: app)
        expectToggle("volteEnabled", value: "1", in: app)
        toggle("volteEnabled", in: app)
        expectToggle("volteEnabled", value: "0", in: app)
        tap(app.buttons["volteDataPath"], in: app)
        tap(app.buttons["voltePath-legacy_bam_dmux"], in: app)
        let selected = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true"), object: app.buttons["voltePath-legacy_bam_dmux"])
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 10), .completed)
        capture(app, name: "VoLTE data path")
        back(app)
        toggle("volteEnabled", in: app)
        expectToggle("volteEnabled", value: "1", in: app)
        tap(app.buttons["volteDataPath"], in: app)
        XCTAssertFalse(app.buttons["voltePath-qmap"].isEnabled)
        XCTAssertFalse(app.buttons["voltePath-qualcomm_410"].isEnabled)
    }

    func testMBIMShowsFixedDataPath() {
        let app = launch("detail-connection-mbim")
        tap(app.buttons["profileVoLTE"], in: app)
        tap(app.buttons["volteDataPath"], in: app)
        let channel = app.descendants(matching: .any)["volteFixedDataPath"].firstMatch
        XCTAssertTrue(channel.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(channel.label.contains("MBIM"), app.debugDescription)
        XCTAssertFalse(app.buttons["voltePath-qmap"].exists)
        capture(app, name: "Fixed MBIM data path")
    }

    func testCapabilitiesAndInactiveProfilesHideUnavailableEntries() {
        let app = launch("detail-connection-community")
        XCTAssertTrue(app.buttons["profileInternet"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["profileWiFiCalling"].exists)
        XCTAssertFalse(app.buttons["profileVoLTE"].exists)
        app.terminate()
        let inactive = launch(profile: "se-a:8986000000000000002")
        XCTAssertTrue(inactive.buttons["profileManagementInfo"].waitForExistence(timeout: 10))
        XCTAssertFalse(inactive.buttons["profileNetworkSelection"].exists)
        XCTAssertFalse(inactive.buttons["profileInternet"].exists)
    }

    func testReadFailuresUseDismissOnlyAlerts() {
        let app = launch("detail-connection-read-error")
        for (entry, control) in [("profileInternet", "internetConnected"), ("profileNetworkSelection", "networkAutomatic"),
                                 ("profileWiFiCalling", "wifiCallingEnabled"), ("profileVoLTE", "volteEnabled")] {
            tap(app.buttons[entry], in: app)
            let alert = app.alerts.firstMatch
            XCTAssertTrue(alert.waitForExistence(timeout: 10))
            XCTAssertEqual(alert.buttons.count, 1)
            XCTAssertFalse(alert.buttons["Retry"].exists)
            alert.buttons["OK"].tap()
            XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
            XCTAssertFalse(app.buttons["retryConnectionRead"].exists)
            XCTAssertFalse(app.switches[control].exists)
            back(app)
        }
    }

    func testScanFailureUsesDismissOnlyAlert() {
        let app = launch("detail-connection-scan-error")
        tap(app.buttons["profileNetworkSelection"], in: app)
        toggle("networkAutomatic", in: app)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 10))
        XCTAssertEqual(alert.buttons.count, 1)
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Retry"].exists)
        XCTAssertFalse(app.buttons["retryNetworkScan"].exists)
        XCTAssertFalse(app.buttons["rescanNetworks"].exists)
        XCTAssertFalse(app.staticTexts["No Networks Found"].exists)
        XCTAssertTrue(app.switches["networkAutomatic"].isEnabled)
    }

    func testEmptyScanKeepsSearchAvailable() {
        let empty = launch("detail-connection-empty")
        tap(empty.buttons["profileNetworkSelection"], in: empty)
        toggle("networkAutomatic", in: empty)
        XCTAssertTrue(empty.staticTexts["No Networks Found"].waitForExistence(timeout: 10))
        XCTAssertTrue(empty.buttons["rescanNetworks"].isEnabled)
        capture(empty, name: "Empty network search")
    }

    func testChineseLargeTextDarkConnectionPages() throws {
        let app = launch(chinese: true, large: true)
        capture(app, name: "Chinese profile connection entries")
        for (entry, control) in [("profileNetworkSelection", "networkAutomatic"), ("profileInternet", "internetConnected"),
                                 ("profileWiFiCalling", "wifiCallingEnabled"), ("profileVoLTE", "volteEnabled")] {
            tap(app.buttons[entry], in: app)
            XCTAssertTrue(app.switches[control].waitForExistence(timeout: 10))
            XCTAssertGreaterThanOrEqual(app.switches[control].frame.height, 44)
            try audit(app, name: "Chinese large text " + entry)
            if entry == "profileInternet" {
                reveal(app.switches["internetAlwaysOn"], in: app)
                try audit(app, name: "Chinese large text Internet options")
                reveal(app.descendants(matching: .any)["internetInterface"].firstMatch, in: app)
                try audit(app, name: "Chinese large text Internet details")
            }
            back(app)
        }
    }

    func testBackgroundDoesNotReplayUncommittedNetworkSelection() {
        let app = launch("detail-connection-scan")
        tap(app.buttons["profileNetworkSelection"], in: app)
        toggle("networkAutomatic", in: app)
        XCUIDevice.shared.press(.home)
        app.activate()
        expectToggle("networkAutomatic", value: "1", in: app)
        XCTAssertFalse(app.buttons["networkOperator-23410"].exists)
    }
}

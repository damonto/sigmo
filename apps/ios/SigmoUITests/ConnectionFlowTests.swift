import XCTest

@MainActor
final class ConnectionFlowTests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    private func launch(_ scenario: String = "detail", chinese: Bool = false, large: Bool = false, dark: Bool = false,
                        profile: String = "se-a:8986000000000000001") -> XCUIApplication {
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment = ["SIGMO_UI_SCENARIO": scenario, "SIGMO_UI_RESET": "1", "SIGMO_UI_APPEARANCE": dark ? "dark" : "light"]
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US",
                               "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let modem = app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"]
        XCTAssertTrue(modem.waitForExistence(timeout: 10), "Device card did not load")
        tap(modem, in: app)
        XCTAssertTrue(app.tabBars.buttons["simcard"].waitForExistence(timeout: 5), "Device details did not open")
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

    private func revealFullText(_ element: XCUIElement, in app: XCUIApplication) {
        let top = app.navigationBars.firstMatch.frame.maxY + 8
        let bottom = app.tabBars.firstMatch.frame.minY - 8
        let viewport = CGRect(x: app.frame.minX, y: top, width: app.frame.width, height: bottom - top)
        for _ in 0..<12 {
            guard element.waitForExistence(timeout: 1) else {
                app.swipeUp()
                continue
            }
            if element.isHittable, viewport.contains(element.frame) { return }
            XCTAssertLessThanOrEqual(element.frame.height, viewport.height)
            let distance = min(max(element.frame.midY - viewport.midY, -viewport.height / 3), viewport.height / 3)
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: viewport.midY / app.frame.height))
            start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: -distance)))
        }
        XCTAssertTrue(viewport.contains(element.frame), "Text is obscured by navigation or tab bars: \(element.debugDescription)")
        XCTAssertTrue(element.isHittable)
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

    private func audit(_ app: XCUIApplication, name: String,
                       types: XCUIAccessibilityAuditType = [.sufficientElementDescription, .textClipped]) throws {
        print("CONNECTION_AUDIT_BEGIN \(name)")
        capture(app, name: name)
        try app.performAccessibilityAudit(for: types) { issue in
            let element = issue.element.map { "\($0.identifier) \($0.label)" } ?? ""
            XCTFail("\(name): \(issue.compactDescription): \(issue.detailedDescription) \(element)")
            return true
        }
        print("CONNECTION_AUDIT_PASS \(name)")
    }

    func testNativeNetworkSelectionAndUncommittedReturn() {
        let app = launch("detail-connection-scan")
        XCTAssertTrue(app.buttons["profileNetworkSelection"].label.contains("Example Mobile"))
        tap(app.buttons["profileNetworkSelection"], in: app)
        expectToggle("networkAutomatic", value: "1", in: app)
        toggle("networkAutomatic", in: app)
        let search = app.buttons["rescanNetworks"]
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        let searching = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", "Searching for networks…"), object: search)
        XCTAssertEqual(XCTWaiter.wait(for: [searching], timeout: 5), .completed, search.debugDescription)
        XCTAssertFalse(search.isEnabled)
        XCTAssertFalse(app.staticTexts["Searching for networks…"].exists)
        XCTAssertTrue(app.buttons["networkOperator-23410"].waitForExistence(timeout: 10))
        XCTAssertTrue(search.isEnabled)
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
        XCTAssertTrue(app.descendants(matching: .any)["internetTrafficHeading"].firstMatch.waitForExistence(timeout: 10))
        toggle("internetConnected", in: app)
        expectToggle("internetConnected", value: "0", in: app)
        XCTAssertFalse(app.descendants(matching: .any)["internetInterface"].exists)
        let apn = app.buttons["internetAPN"]
        tap(apn, in: app)
        XCTAssertTrue(app.navigationBars["APN"].exists)
        replace(app.textFields["internetAPNField"], with: "native.apn", in: app)
        replace(app.textFields["internetUsername"], with: "alice", in: app)
        back(app)
        XCTAssertTrue(app.navigationBars["Internet"].exists)
        scrollToTop(app)
        toggle("internetDefaultRoute", in: app)
        expectToggle("internetDefaultRoute", value: "1", in: app)
        toggle("internetAlwaysOn", in: app)
        expectToggle("internetAlwaysOn", value: "0", in: app)
        capture(app, name: "Internet main page connection options")
        tap(apn, in: app)
        reveal(app.textFields["internetAPNField"], in: app)
        XCTAssertEqual(app.textFields["internetAPNField"].value as? String, "native.apn")
        XCTAssertEqual(app.textFields["internetUsername"].value as? String, "alice")
        back(app)
        scrollToTop(app)
        toggle("internetConnected", in: app)
        expectToggle("internetConnected", value: "1", in: app)
        tap(apn, in: app)
        XCTAssertTrue(app.textFields["internetAPNField"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.textFields["internetAPNField"].isEnabled)
        back(app)
        reveal(app.descendants(matching: .any)["internetInterface"].firstMatch, in: app)
        XCTAssertEqual(app.descendants(matching: .any)["internetInterface"].firstMatch.value as? String, "wwan0")
        capture(app, name: "Internet main page connection details")
        reveal(app.staticTexts.matching(NSPredicate(format: "label ==[c] %@", "IP Information")).firstMatch, in: app)
        reveal(app.staticTexts.matching(NSPredicate(format: "label ==[c] %@", "Public Network")).firstMatch, in: app)
        let publicIP = app.descendants(matching: .any)["internetPublicIP"].firstMatch
        let loaded = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", "203.0.113.2"), object: publicIP)
        XCTAssertEqual(XCTWaiter.wait(for: [loaded], timeout: 10), .completed)
        tap(app.buttons["copyHTTPProxy"], in: app)
        XCTAssertTrue(app.buttons["copySOCKSProxy"].exists)
        capture(app, name: "Internet proxy details")
    }

    func testInternetSpeedTestRestoresResultWhileDisconnected() {
        let app = launch()
        tap(app.buttons["profileInternet"], in: app)
        let button = app.buttons["internetSpeedTestButton"]
        let size = app.descendants(matching: .any)["internetSpeedTestSize"].firstMatch
        let defaultUsage = app.staticTexts["Uses about 10 MB of mobile data per test."]
        XCTAssertTrue(button.waitForExistence(timeout: 5))
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: button)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
        XCTAssertEqual(button.label, "Start Test")
        XCTAssertTrue(defaultUsage.exists)
        XCTAssertTrue(size.waitForExistence(timeout: 5), app.debugDescription)
        size.tap()
        let largerSize = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "50 MB")).firstMatch
        XCTAssertTrue(largerSize.waitForExistence(timeout: 5), app.debugDescription)
        largerSize.tap()
        XCTAssertTrue(app.staticTexts["Uses about 50 MB of mobile data per test."].exists)
        tap(button, in: app)
        XCTAssertTrue(app.staticTexts["92.4 Mbps"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["15.2 Mbps"].exists)
        XCTAssertEqual(button.label, "Test Again")
        toggle("internetConnected", in: app)
        expectToggle("internetConnected", value: "0", in: app)
        back(app)
        tap(app.buttons["profileInternet"], in: app)
        expectToggle("internetConnected", value: "0", in: app)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertTrue(app.staticTexts["92.4 Mbps"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["15.2 Mbps"].exists)
        XCTAssertTrue(defaultUsage.exists)
        XCTAssertFalse(button.isEnabled)
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

    func testIMSConnectionProgress() {
        let app = launch("detail-connection-progress")
        tap(app.buttons["profileWiFiCalling"], in: app)
        let aka = app.descendants(matching: .any)["imsProgressNode-AKA"].firstMatch
        XCTAssertTrue(aka.waitForExistence(timeout: 10))
        XCTAssertEqual(aka.value as? String, "In progress")
        let epdg = app.descendants(matching: .any)["imsProgressNode-ePDG"].firstMatch
        XCTAssertEqual(epdg.value as? String, "Completed")
        let ipsec = app.descendants(matching: .any)["imsProgressNode-IPsec"].firstMatch
        XCTAssertEqual(ipsec.value as? String, "Not reached")
        XCTAssertEqual(epdg.frame.midY, ipsec.frame.midY, accuracy: 1)
        back(app)

        tap(app.buttons["profileVoLTE"], in: app)
        let pdn = app.descendants(matching: .any)["imsProgressNode-IMS PDN"].firstMatch
        XCTAssertTrue(pdn.waitForExistence(timeout: 10))
        XCTAssertEqual(pdn.value as? String, "Failed")
        XCTAssertEqual(aka.value as? String, "Not reached")
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
        XCTAssertTrue(app.navigationBars["Wi-Fi Calling"].exists)
        app.swipeDown()
        tap(app.buttons["wifiCallingEmergencyAddress"], in: app)
        XCTAssertTrue(web.waitForExistence(timeout: 10))
        tap(app.buttons["closeWiFiWebsheet"], in: app)
        XCTAssertTrue(web.waitForNonExistence(timeout: 10))
        expectToggle("wifiCallingEnabled", value: "1", in: app)
    }

    func testWiFiCallingProxyHostAcceptsCopiedSOCKS5URL() {
        let app = launch("detail-connection-proxy-check")
        tap(app.buttons["profileInternet"], in: app)
        tap(app.buttons["copySOCKSProxy"], in: app)
        back(app)
        tap(app.buttons["profileWiFiCalling"], in: app)
        tap(app.buttons["wifiProxySettings"], in: app)
        XCTAssertTrue(app.navigationBars["SOCKS5 Proxy"].exists)
        tap(app.navigationBars["SOCKS5 Proxy"].buttons["Add Proxy"], in: app)

        let host = app.textFields["wifiProxyHost"]
        replace(host, with: "draft.test", in: app)
        host.typeKey("v", modifierFlags: .command)
        let hostFilled = NSPredicate(format: "value == %@", "2001:db8::1")
        let shortcut = XCTNSPredicateExpectation(predicate: hostFilled, object: host)
        if XCTWaiter.wait(for: [shortcut], timeout: 3) != .completed {
            // The simulator can deliver clipboard actions through the text editing menu.
            host.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).press(forDuration: 1)
            let paste = app.descendants(matching: .any)["Paste"].firstMatch
            XCTAssertTrue(paste.waitForExistence(timeout: 5), app.debugDescription)
            paste.tap()
            let filled = XCTNSPredicateExpectation(predicate: hostFilled, object: host)
            XCTAssertEqual(XCTWaiter.wait(for: [filled], timeout: 5), .completed)
        }
        XCTAssertEqual(app.textFields["wifiProxyName"].value as? String, "2001:db8::1")
        XCTAssertEqual(app.textFields["wifiProxyPort"].value as? String, "1080")
        XCTAssertEqual(app.textFields["wifiProxyUsername"].value as? String, "proxy")
        XCTAssertTrue(app.navigationBars["Add Proxy"].buttons["Save"].isEnabled)

        tap(app.buttons["Cancel"], in: app)
        expectToggle("wifiProxyEnabled", value: "0", in: app)
        XCTAssertFalse(app.buttons["Edit Proxy"].exists)
    }

    func testWiFiCallingProxySettingsOpenWhenLineSettingsFail() {
        let app = launch("detail-connection-read-error")
        tap(app.buttons["profileWiFiCalling"], in: app)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 10))
        alert.buttons["OK"].tap()
        tap(app.buttons["wifiProxySettings"], in: app)
        XCTAssertTrue(app.navigationBars["SOCKS5 Proxy"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.navigationBars["SOCKS5 Proxy"].buttons["Add Proxy"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertFalse(app.switches["wifiProxyEnabled"].exists)
    }

    func testWiFiCallingSOCKS5ProxyCanBeTestedBeforeApplying() {
        let app = launch("detail-connection-proxy-check")
        tap(app.buttons["profileWiFiCalling"], in: app)
        let uplink = app.buttons["wifiCallingUplink"]
        reveal(uplink, in: app)
        let originalUplink = uplink.value as? String
        XCTAssertNotNil(originalUplink)
        let proxySettings = app.buttons["wifiProxySettings"]
        tap(proxySettings, in: app)
        tap(app.navigationBars["SOCKS5 Proxy"].buttons["Add Proxy"], in: app)

        let name = app.textFields["wifiProxyName"]
        XCTAssertTrue(name.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(name.placeholderValue, "Office proxy")
        tap(name, in: app)
        name.typeText("Office")
        let host = app.textFields["wifiProxyHost"]
        XCTAssertEqual(host.placeholderValue, "proxy.example.com")
        XCTAssertEqual(app.textFields["wifiProxyPort"].placeholderValue, "1080")
        tap(host, in: app)
        host.typeText("proxy.example")
        tap(app.navigationBars["Add Proxy"].buttons["Save"], in: app)

        expectToggle("wifiProxyEnabled", value: "0", in: app)
        let check = app.buttons["Test Connection"]
        tap(check, in: app)
        let checking = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == false AND value == %@", "Testing Connection…"),
                                                  object: check)
        XCTAssertEqual(XCTWaiter.wait(for: [checking], timeout: 4), .completed)
        XCTAssertFalse(app.staticTexts["Testing Connection…"].exists)
        let tcp = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@",
                                                                      "203.0.113.4", "203.0.113.4")).firstMatch
        XCTAssertTrue(tcp.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(check.isEnabled)
        let udp = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@ OR value CONTAINS %@",
                                                                      "[2001:db8::4]:40300", "[2001:db8::4]:40300")).firstMatch
        reveal(udp, in: app)
        app.swipeDown()
        toggle("wifiProxyEnabled", in: app)
        expectToggle("wifiProxyEnabled", value: "1", in: app)
        back(app)
        reveal(proxySettings, in: app)
        XCTAssertEqual(proxySettings.value as? String, "On")
        tap(proxySettings, in: app)
        toggle("wifiProxyEnabled", in: app)
        expectToggle("wifiProxyEnabled", value: "0", in: app)
        back(app)
        for _ in 0..<4 where !uplink.isHittable { app.swipeDown() }
        let restored = XCTNSPredicateExpectation(
            predicate: NSPredicate(format: "value == %@ AND enabled == true", originalUplink ?? ""),
            object: uplink
        )
        XCTAssertEqual(XCTWaiter.wait(for: [restored], timeout: 10), .completed)
    }

    func testVoLTEControlsAndDisabledDataPathEditing() {
        let app = launch()
        tap(app.buttons["profileVoLTE"], in: app)
        expectToggle("volteEnabled", value: "1", in: app)
        toggle("volteEnabled", in: app)
        expectToggle("volteEnabled", value: "0", in: app)
        let path = app.buttons["volteDataPath"]
        tap(path, in: app)
        tap(app.buttons["Legacy BAM-DMUX"], in: app)
        let selected = XCTNSPredicateExpectation(
            predicate: NSPredicate(format: "value == %@ OR label CONTAINS %@", "Legacy BAM-DMUX", "Legacy BAM-DMUX"),
            object: path
        )
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 10), .completed)
        XCTAssertTrue(app.navigationBars["VoLTE"].exists)
        capture(app, name: "VoLTE data path")
        app.swipeDown()
        toggle("volteEnabled", in: app)
        expectToggle("volteEnabled", value: "1", in: app)
        reveal(path, in: app)
        XCTAssertFalse(path.isEnabled)
    }

    func testMBIMShowsFixedDataPath() {
        let app = launch("detail-connection-mbim")
        tap(app.buttons["profileVoLTE"], in: app)
        let channel = app.descendants(matching: .any)["volteFixedDataPath"].firstMatch
        reveal(channel, in: app)
        XCTAssertTrue(channel.label.contains("MBIM") || channel.value as? String == "MBIM", app.debugDescription)
        XCTAssertFalse(app.buttons["volteDataPath"].exists)
        capture(app, name: "Fixed MBIM data path")
    }

    func testCapabilitiesAndInactiveProfilesHideUnavailableEntries() {
        let app = launch("detail-connection-unlicensed")
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

    func testChineseDarkConnectionPagesDynamicTypeSweep() throws {
        // textClipped cycles through Dynamic Type sizes. Start that sweep at the
        // default size so shrinking a scrolled Form does not recycle its target rows.
        try auditConnectionPages(large: false)
    }

    func testChineseLargeTextDarkConnectionPages() throws {
        try auditConnectionPages(large: true)
    }

    private func auditConnectionPages(large: Bool) throws {
        let app = launch(chinese: true, large: large, dark: true)
        defer { app.terminate() }
        let variant = large ? "Chinese maximum text" : "Chinese Dynamic Type sweep"
        let types: XCUIAccessibilityAuditType = large ? [.sufficientElementDescription] : [.sufficientElementDescription, .textClipped]
        let progressNodes = ["profileWiFiCalling": "IPsec", "profileVoLTE": "IMS PDN"]
        capture(app, name: variant + " profile connection entries")
        for (entry, control) in [("profileNetworkSelection", "networkAutomatic"), ("profileInternet", "internetConnected"),
                                 ("profileWiFiCalling", "wifiCallingEnabled"), ("profileVoLTE", "volteEnabled")] {
            tap(app.buttons[entry], in: app)
            reveal(app.switches[control], in: app)
            XCTAssertGreaterThanOrEqual(app.switches[control].frame.height, 44)
            if entry == "profileWiFiCalling" {
                toggle(control, in: app)
                tap(app.alerts.buttons["confirmWiFiCalling"].firstMatch, in: app)
                // At accessibility sizes, the newly visible progress can move the switch below the viewport.
                reveal(app.switches[control], in: app)
                expectToggle(control, value: "1", in: app)
            }
            if let label = progressNodes[entry] {
                let node = app.descendants(matching: .any)["imsProgressNode-" + label].firstMatch
                for _ in 0..<6 where !node.exists { app.swipeDown() }
                XCTAssertTrue(node.waitForExistence(timeout: 5))
                XCTAssertEqual(node.value as? String, "已完成")
                if large { revealFullText(node, in: app) }
            }
            try audit(app, name: variant + " " + entry, types: types)
            if entry == "profileInternet" {
                if large {
                    for option in ["internetDefaultRoute", "internetProxyEnabled", "internetAlwaysOn"] {
                        revealFullText(app.switches[option], in: app)
                        try audit(app, name: variant + " " + option, types: types)
                    }
                } else {
                    reveal(app.switches["internetAlwaysOn"], in: app)
                    try audit(app, name: variant + " Internet options", types: types)
                }
                reveal(app.descendants(matching: .any)["internetInterface"].firstMatch, in: app)
                try audit(app, name: variant + " Internet details", types: types)
            }
            back(app)
        }
    }

    func testBackgroundPreservesUncommittedNetworkBrowsing() {
        let app = launch("detail-connection-scan")
        tap(app.buttons["profileNetworkSelection"], in: app)
        toggle("networkAutomatic", in: app)
        XCTAssertTrue(app.buttons["networkOperator-23410"].waitForExistence(timeout: 10))
        XCUIDevice.shared.press(.home)
        app.activate()
        expectToggle("networkAutomatic", value: "0", in: app)
        XCTAssertTrue(app.buttons["networkOperator-23410"].exists)
        XCTAssertFalse(app.buttons["networkOperator-23410"].isSelected)
        back(app)
        XCTAssertTrue(app.buttons["profileNetworkSelection"].label.contains("Example Mobile"))
        tap(app.buttons["profileNetworkSelection"], in: app)
        expectToggle("networkAutomatic", value: "1", in: app)
        XCTAssertFalse(app.buttons["networkOperator-23410"].exists)
    }
}

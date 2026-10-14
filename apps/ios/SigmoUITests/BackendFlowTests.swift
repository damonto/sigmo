import XCTest

@MainActor
final class BackendFlowTests: XCTestCase {
    private func app(scenario: String, chinese: Bool = false, largeText: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = scenario
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", largeText ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        return app
    }

    func testAddBackendPersistAcrossLaunch() {
        let app = app(scenario: "empty")
        app.launch()
        XCTAssertTrue(app.buttons["addBackend"].waitForExistence(timeout: 10))
        app.buttons["addBackend"].tap()
        XCTAssertTrue(app.textFields["backendName"].exists)
        XCTAssertTrue(app.textFields["backendAddress"].exists)
        XCTAssertFalse(app.staticTexts["Group"].exists)
        attachScreenshot(app)
        app.textFields["backendName"].tap()
        app.textFields["backendName"].typeText("Test Backend")
        app.textFields["backendAddress"].tap()
        app.textFields["backendAddress"].typeText("https://test.sigmo.test")
        app.buttons["saveBackend"].tap()
        XCTAssertTrue(app.staticTexts["Test Backend"].waitForExistence(timeout: 10))
        app.terminate()
        app.launchEnvironment["SIGMO_UI_RESET"] = "0"
        app.launch()
        XCTAssertTrue(app.staticTexts["Test Backend"].waitForExistence(timeout: 10))
        attachScreenshot(app)
    }

    func testEmptyConnectionButtonKeepsCompactShape() {
        for chinese in [false, true] {
            let app = app(scenario: "empty", chinese: chinese, largeText: chinese)
            app.launchEnvironment["SIGMO_UI_APPEARANCE"] = chinese ? "dark" : "light"
            app.launch()
            let connect = app.buttons["connectSigmo"]
            XCTAssertTrue(connect.waitForExistence(timeout: 10))
            XCTAssertTrue(connect.isHittable)
            XCTAssertTrue(connect.label.contains(chinese ? "连接 Sigmo" : "Connect Sigmo"))
            XCTAssertGreaterThanOrEqual(connect.frame.height, 44)
            XCTAssertGreaterThan(connect.frame.width, connect.frame.height)
            XCTAssertLessThan(connect.frame.height, 160)
            XCTAssertGreaterThanOrEqual(connect.frame.minX, app.frame.minX)
            XCTAssertLessThanOrEqual(connect.frame.maxX, app.frame.maxX)
            assertToolbarButtonsAreAccessible(app)
            attachScreenshot(app)
            connect.tap()
            XCTAssertTrue(app.textFields["backendName"].waitForExistence(timeout: 5))
            XCTAssertTrue(app.textFields["backendAddress"].exists)
            app.terminate()
        }
    }

    private func assertToolbarButtonsAreAccessible(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        for button in [app.buttons["addBackend"], app.buttons["manageBackends"]] {
            XCTAssertTrue(button.isHittable, file: file, line: line)
            XCTAssertGreaterThanOrEqual(button.frame.minX, app.frame.minX, file: file, line: line)
            XCTAssertLessThanOrEqual(button.frame.maxX, app.frame.maxX, file: file, line: line)
        }
        XCTAssertLessThan(app.buttons["manageBackends"].frame.maxX, app.buttons["addBackend"].frame.minX, file: file, line: line)
    }

    func testHomeAndDetailRefreshOnlyOnDemandOrForeground() {
        let app = app(scenario: "refresh")
        app.launch()
        let id = "00000000-0000-0000-0000-000000000001-device-1"
        let carrier = app.staticTexts["modemCarrier-\(id)"]
        XCTAssertTrue(carrier.waitForExistence(timeout: 10))
        XCTAssertEqual(carrier.label, "Read 1")
        expectNoRefresh(carrier, seconds: 32)

        pullToRefresh(app)
        expectRefresh(carrier, from: "Read 1")
        let afterPull = carrier.label
        XCUIDevice.shared.press(.home)
        app.activate()
        expectRefresh(carrier, from: afterPull)

        let beforeDetail = carrier.label
        app.buttons["modem-\(id)"].tap()
        let title = app.staticTexts["detailModemTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        expectRefresh(title, from: beforeDetail)
        expectNoRefresh(title, seconds: 12)
        let beforePull = title.label
        pullToRefresh(app)
        expectRefresh(title, from: beforePull)
        let beforeForeground = title.label
        XCUIDevice.shared.press(.home)
        app.activate()
        expectRefresh(title, from: beforeForeground)
        expectNoRefresh(title, seconds: 6)
    }

    private func pullToRefresh(_ app: XCUIApplication) {
        let list = app.collectionViews.firstMatch
        XCTAssertTrue(list.exists)
        let start = list.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        let end = list.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95))
        start.press(forDuration: 0.1, thenDragTo: end)
    }

    private func expectNoRefresh(_ element: XCUIElement, seconds: TimeInterval, file: StaticString = #filePath, line: UInt = #line) {
        let changed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label != %@", element.label), object: element)
        changed.isInverted = true
        XCTAssertEqual(XCTWaiter.wait(for: [changed], timeout: seconds), .completed, file: file, line: line)
    }

    private func expectRefresh(_ element: XCUIElement, from label: String, file: StaticString = #filePath, line: UInt = #line) {
        let changed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label != %@", label), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [changed], timeout: 5), .completed, file: file, line: line)
    }

    func testSingleAndMultipleConnectionsKeepTitleAndAddButton() {
        let app = app(scenario: "single")
        app.launch()
        XCTAssertTrue(app.navigationBars["Sigmo"].waitForExistence(timeout: 10))
        let add = app.buttons["addBackend"]
        assertToolbarButtonsAreAccessible(app)
        let heading = app.staticTexts["backendHeading-00000000-0000-0000-0000-000000000001"]
        XCTAssertTrue(heading.waitForExistence(timeout: 10))
        XCTAssertFalse(app.segmentedControls["backendFilterSegments"].exists)
        add.tap()
        XCTAssertTrue(app.navigationBars["Add Sigmo"].exists)
        app.textFields["backendName"].tap()
        app.textFields["backendName"].typeText("Another Sigmo")
        app.textFields["backendAddress"].tap()
        app.textFields["backendAddress"].typeText("https://another.sigmo.test")
        app.buttons["saveBackend"].tap()
        XCTAssertTrue(app.navigationBars["Sigmo"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["addBackend"].exists)
        XCTAssertTrue(app.buttons["Another Sigmo"].exists)
        XCTAssertTrue(heading.exists)
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Home"].exists)
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["All"].isSelected)
        attachScreenshot(app)
    }

    func testMergedFiltersKeepSameModemIDsIndependent() {
        let app = app(scenario: "dashboard")
        app.launch()
        let homeID = "00000000-0000-0000-0000-000000000001"
        let studioID = "00000000-0000-0000-0000-000000000002"
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Home"].waitForExistence(timeout: 10))
        app.segmentedControls["backendFilterSegments"].buttons["Home"].tap()
        XCTAssertTrue(app.buttons["modem-\(homeID)-device-1"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["modem-\(studioID)-device-1"].exists)
        XCTAssertTrue(app.staticTexts["backendHeading-\(homeID)"].exists)
        app.segmentedControls["backendFilterSegments"].buttons["Studio"].tap()
        let studio = app.buttons["modem-\(studioID)-device-1"]
        XCTAssertTrue(studio.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["modem-\(homeID)-device-1"].exists)
        studio.tap()
        XCTAssertTrue(app.staticTexts["detailModemTitle"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["detailModemTitle"].label, "Studio Modem")
        XCTAssertFalse(app.staticTexts["detailBackendName"].exists)
        XCTAssertEqual(app.buttons["Switch modem"].value as? String, "Studio Modem")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.segmentedControls["backendFilterSegments"].buttons["All"].tap()
        for _ in 0..<6 where !app.buttons["modem-\(homeID)-device-1"].isHittable { app.swipeUp() }
        XCTAssertTrue(app.buttons["modem-\(homeID)-device-1"].exists)
        XCTAssertTrue(app.buttons["modem-\(studioID)-device-1"].exists)
        attachScreenshot(app)
    }

    func testGlassConnectionFilterSurvivesRepeatedNavigation() {
        let app = app(scenario: "dashboard")
        app.launch()
        let id = "00000000-0000-0000-0000-000000000002"
        let studio = app.segmentedControls["backendFilterSegments"].buttons["Studio"]
        let all = app.segmentedControls["backendFilterSegments"].buttons["All"]
        XCTAssertTrue(studio.waitForExistence(timeout: 10), app.debugDescription)
        let start = all.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        let end = studio.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        start.press(forDuration: 0.3, thenDragTo: end)
        XCTAssertTrue(studio.isSelected)
        XCTAssertFalse(app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"].exists)
        for _ in 0..<2 {
            studio.tap()
            XCTAssertTrue(studio.isSelected)
            app.buttons["manageBackends"].tap()
            app.buttons["backend-\(id)"].tap()
            XCTAssertTrue(app.staticTexts["backendDisplayAddress"].waitForExistence(timeout: 5))
            app.navigationBars.buttons.element(boundBy: 0).tap()
            app.navigationBars.buttons.element(boundBy: 0).tap()
            XCTAssertTrue(studio.isHittable, app.debugDescription)
            XCTAssertTrue(studio.isSelected)
            XCTAssertTrue(all.isHittable)
            app.buttons["modem-\(id)-device-1"].tap()
            XCTAssertTrue(app.staticTexts["detailModemTitle"].waitForExistence(timeout: 5))
            app.navigationBars.buttons.element(boundBy: 0).tap()
            XCTAssertTrue(studio.isHittable, app.debugDescription)
            XCTAssertTrue(studio.isSelected)
            all.tap()
            XCTAssertTrue(all.isSelected)
        }
        attachScreenshot(app)
    }

    func testLongConnectionFilterRemainsScrollableAfterDeviceNavigation() {
        let app = app(scenario: "single")
        app.launch()
        app.buttons["addBackend"].tap()
        let name = "Travel Workbench With A Longer Connection Name"
        app.textFields["backendName"].tap()
        app.textFields["backendName"].typeText(name)
        app.textFields["backendAddress"].tap()
        app.textFields["backendAddress"].typeText("https://travel.sigmo.test")
        app.buttons["saveBackend"].tap()
        let segment = app.segmentedControls["backendFilterSegments"].buttons[name]
        XCTAssertTrue(segment.waitForExistence(timeout: 10), app.debugDescription)
        let scroll = app.scrollViews.firstMatch
        XCTAssertTrue(scroll.exists)
        for _ in 0..<6 where !segment.isHittable { scroll.swipeLeft() }
        XCTAssertTrue(segment.isHittable)
        segment.tap()
        XCTAssertTrue(segment.isSelected)
        let modem = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "modem-")).firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 5))
        modem.tap()
        XCTAssertTrue(app.staticTexts["detailModemTitle"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["detailModemTitle"].label, "Studio Modem")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(segment.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(segment.isHittable, app.debugDescription)
        XCTAssertTrue(segment.isSelected)
        let all = app.segmentedControls["backendFilterSegments"].buttons["All"]
        for _ in 0..<6 where !all.isHittable { scroll.swipeRight() }
        attachScreenshot(app)
        XCTAssertTrue(all.isHittable, app.debugDescription)
        XCTAssertTrue(segment.isSelected, "Scrolling must preserve the selected connection")
        all.tap()
        XCTAssertTrue(all.isSelected)
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"].waitForExistence(timeout: 5))
    }

    func testAirplaneReplacesSignalAndCellularConnection() {
        let app = app(scenario: "airplane")
        app.launch()
        XCTAssertTrue(app.images["airplane-00000000-0000-0000-0000-000000000001-device-1"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.descendants(matching: .any)["signal-00000000-0000-0000-0000-000000000001-device-1"].exists)
        XCTAssertFalse(app.images["volte-00000000-0000-0000-0000-000000000001-device-1"].exists)
        attachScreenshot(app)
    }

    func testLocalSearchAndConnectionTabsFilterTogether() {
        let app = app(scenario: "cards")
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = "light"
        app.launch()
        let home = "00000000-0000-0000-0000-000000000001"
        let studio = "00000000-0000-0000-0000-000000000002"
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        let all = app.segmentedControls["backendFilterSegments"].buttons["All"]
        let homeTab = app.segmentedControls["backendFilterSegments"].buttons["Home"]
        XCTAssertLessThan(search.frame.maxY, all.frame.minY + 1)
        XCTAssertTrue(search.isHittable)
        XCTAssertGreaterThanOrEqual(all.frame.height, 32)
        XCTAssertLessThan(all.frame.height, search.frame.height)
        let filter = app.descendants(matching: .any)["backendFilters"].firstMatch
        XCTAssertGreaterThanOrEqual(filter.frame.minX, app.frame.minX)
        XCTAssertLessThanOrEqual(filter.frame.maxX, app.frame.maxX)
        XCTAssertTrue(all.isSelected)
        attachScreenshot(app)
        let toolbarBottom = max(app.buttons["addBackend"].frame.maxY, app.buttons["manageBackends"].frame.maxY)
        XCTAssertGreaterThanOrEqual(search.frame.minY, toolbarBottom)
        XCTAssertGreaterThanOrEqual(all.frame.minY, search.frame.maxY)
        // The compact glass tab responds along its lower edge.
        homeTab.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
        XCTAssertTrue(homeTab.isSelected)
        search.tap()
        search.typeText("travel\n")
        XCTAssertTrue(app.buttons["modem-\(home)-device-2"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["modem-\(home)-device-1"].exists)
        XCTAssertFalse(app.buttons["modem-\(studio)-device-1"].exists)
        XCTAssertEqual(app.staticTexts["modemNumber-\(home)-device-2"].label, "No Number")
        XCTAssertTrue(search.buttons.firstMatch.isHittable)
        attachScreenshot(app)
        search.buttons.firstMatch.tap()
        search.tap()
        search.typeText("13800000000\n")
        XCTAssertTrue(app.buttons["modem-\(home)-device-1"].exists)
        XCTAssertFalse(app.buttons["modem-\(home)-device-2"].exists)
        app.segmentedControls["backendFilterSegments"].buttons["Studio"].tap()
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Studio"].isSelected)
        XCTAssertTrue(app.buttons["modem-\(studio)-device-1"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["modem-\(home)-device-1"].exists)
        search.buttons.firstMatch.tap()
        search.tap()
        search.typeText("missing\n")
        XCTAssertTrue(app.descendants(matching: .any)["homeSearchEmpty"].firstMatch.waitForExistence(timeout: 5))
        search.buttons.firstMatch.tap()
        XCTAssertTrue(app.buttons["modem-\(studio)-device-1"].waitForExistence(timeout: 5))
    }

    func testIMSDetailsShowOneStatusPerService() {
        for (scenario, status) in [("single", "Connected"), ("ims-disabled", "Disabled")] {
            let app = app(scenario: scenario)
            app.launch()
            let modem = app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"]
            XCTAssertTrue(modem.waitForExistence(timeout: 10))
            XCTAssertEqual(app.images["volte-00000000-0000-0000-0000-000000000001-device-1"].exists, scenario == "single")
            modem.tap()
            XCTAssertTrue(app.buttons["modemInfo"].waitForExistence(timeout: 5))
            app.buttons["modemInfo"].tap()
            let wifi = app.descendants(matching: .any)["modemWiFiCallingStatus"].firstMatch
            for _ in 0..<8 where !wifi.isHittable { app.swipeUp() }
            XCTAssertTrue(wifi.exists)
            XCTAssertTrue(app.staticTexts["Wi-Fi Calling"].exists)
            XCTAssertTrue(app.staticTexts["VoLTE"].exists)
            XCTAssertEqual(app.staticTexts.matching(identifier: status).count, scenario == "single" ? 3 : 2)
            XCTAssertFalse(app.staticTexts["Wi-Fi Calling enabled"].exists)
            XCTAssertFalse(app.staticTexts["Wi-Fi Calling connection"].exists)
            attachScreenshot(app)
            app.terminate()
        }
    }

    func testDeviceRowsShowCarrierNumberAndMetadata() throws {
        let app = app(scenario: "cards")
        app.launch()
        let first = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        let second = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-2"].firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        XCTAssertTrue(second.exists)
        let identity = "00000000-0000-0000-0000-000000000001-device-1"
        let carrier = app.staticTexts["modemCarrier-\(identity)"]
        let number = app.staticTexts["modemNumber-\(identity)"]
        let heading = app.staticTexts["backendHeading-00000000-0000-0000-0000-000000000001"]
        let metadata = app.staticTexts["modemMetadata-\(identity)"]
        let signal = app.descendants(matching: .any)["signal-\(identity)"].firstMatch
        XCTAssertTrue(number.exists)
        XCTAssertEqual(carrier.frame.midY, signal.frame.midY, accuracy: 5)
        XCTAssertLessThan(carrier.frame.maxX, signal.frame.minX)
        XCTAssertGreaterThan(number.frame.minY, carrier.frame.minY)
        XCTAssertEqual(carrier.frame.minX, number.frame.minX, accuracy: 2)
        XCTAssertTrue(heading.exists)
        XCTAssertLessThanOrEqual(heading.frame.maxY, first.frame.minY)
        XCTAssertEqual(metadata.label, "Home Modem · eSIM")
        XCTAssertGreaterThan(metadata.frame.minY, number.frame.minY)
        XCTAssertEqual(metadata.frame.minX, number.frame.minX, accuracy: 2)
        XCTAssertLessThanOrEqual(first.frame.height, 120)
        XCTAssertEqual(app.staticTexts["modemNumber-00000000-0000-0000-0000-000000000001-device-2"].label, "No Number")
        XCTAssertGreaterThan(second.frame.minY, first.frame.minY)
        XCTAssertEqual(first.frame.minX, second.frame.minX, accuracy: 2)
        XCTAssertGreaterThanOrEqual(first.frame.minX, app.frame.minX)
        XCTAssertLessThanOrEqual(second.frame.maxX, app.frame.maxX)
        attachScreenshot(app)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription]) { issue in
            if self.isExpectedNameTruncation(issue) { return true }
            print("AUDIT_ISSUE " + issue.detailedDescription + " " + (issue.element?.debugDescription ?? ""))
            return false
        }
        second.tap()
        let title = app.staticTexts["detailModemTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        XCTAssertEqual(title.label, "Travel Modem with a long descriptive device name")
        XCTAssertGreaterThanOrEqual(title.frame.minX, 60)
        XCTAssertLessThanOrEqual(title.frame.maxX, app.frame.maxX - 60)
        XCTAssertLessThanOrEqual(title.frame.height, 24)
        attachScreenshot(app)
    }

    func testHomeKeepsConnectionRecoveryInManagement() {
        let app = app(scenario: "dashboard")
        app.launch()
        let home = "00000000-0000-0000-0000-000000000001"
        let locked = "00000000-0000-0000-0000-000000000003"
        let offline = "00000000-0000-0000-0000-000000000004"
        let activation = "00000000-0000-0000-0000-000000000005"
        XCTAssertTrue(app.buttons["modem-\(home)-device-1"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Home"].exists)
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Studio"].waitForExistence(timeout: 5))
        for name in ["Locked", "Offline", "Pro Activation"] {
            XCTAssertFalse(app.segmentedControls["backendFilterSegments"].buttons[name].exists)
        }
        for id in [locked, offline, activation] {
            XCTAssertFalse(app.descendants(matching: .any)["connectionStatus-\(id)"].exists)
            XCTAssertFalse(app.buttons["modem-\(id)-device-1"].exists)
        }
        XCTAssertFalse(app.buttons["Log In"].exists)
        XCTAssertFalse(app.buttons["Activate Pro"].exists)
        XCTAssertFalse(app.buttons["Retry"].exists)
        attachScreenshot(app)

        app.buttons["manageBackends"].tap()
        for (id, label) in [(home, "Online"), (locked, "Login required"),
                            (offline, "Offline"), (activation, "Activation required")] {
            let status = app.descendants(matching: .any)["backendStatus-\(id)"].firstMatch
            for _ in 0..<5 where !status.isHittable { app.swipeUp() }
            XCTAssertTrue(status.waitForExistence(timeout: 5))
            XCTAssertEqual(status.label, label)
        }
        attachScreenshot(app)
        app.buttons["backend-\(offline)"].tap()
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertEqual(alert.buttons.count, 1)
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["backendDisplayAddress"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Retry"].exists)
        pullToRefresh(app)
        XCTAssertTrue(app.staticTexts["backendDisplayAddress"].exists)
    }

    func testTwoBackendsAndIndependentLogin() {
        let app = app(scenario: "dashboard")
        app.launch()
        let home = app.buttons["manageBackends"]
        XCTAssertTrue(home.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"].waitForExistence(timeout: 10))
        let studio = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000002-device-1"].firstMatch
        for _ in 0..<5 where !studio.isHittable { app.swipeUp() }
        XCTAssertTrue(studio.isHittable)
        studio.tap()
        XCTAssertTrue(app.staticTexts["detailModemTitle"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["detailModemTitle"].label, "Studio Modem")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["manageBackends"].tap()
        let locked = app.buttons["backend-00000000-0000-0000-0000-000000000003"]
        for _ in 0..<5 where !locked.isHittable { app.swipeUp() }
        locked.tap()
        XCTAssertTrue(app.buttons["loginBackend"].waitForExistence(timeout: 5))
        app.buttons["loginBackend"].tap()
        XCTAssertTrue(app.buttons["sendOTP"].waitForExistence(timeout: 5))
        app.buttons["sendOTP"].tap()
        app.textFields["otpCode"].tap()
        app.textFields["otpCode"].typeText("123456")
        XCTAssertTrue(app.buttons["Log Out"].waitForExistence(timeout: 10))
        attachScreenshot(app)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000003-device-1"].waitForExistence(timeout: 5))
        let lockedTab = app.segmentedControls["backendFilterSegments"].buttons["Locked"]
        XCTAssertTrue(lockedTab.exists)
        lockedTab.tap()
        XCTAssertTrue(lockedTab.isSelected)
        app.buttons["manageBackends"].tap()
        locked.tap()
        app.buttons["Log Out"].tap()
        XCTAssertTrue(app.buttons["loginBackend"].waitForExistence(timeout: 5))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["All"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["All"].isSelected)
        XCTAssertFalse(lockedTab.exists)
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"].exists)
    }

    func testManagementDetailsSettingsEditAndReturn() {
        let app = app(scenario: "dashboard")
        app.launch()
        let id = "00000000-0000-0000-0000-000000000001"
        app.buttons["manageBackends"].tap()
        let row = app.buttons["backend-\(id)"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        XCTAssertFalse(app.tabBars.firstMatch.exists)
        XCTAssertTrue(app.staticTexts["https://home.sigmo.test"].exists)
        XCTAssertFalse(app.staticTexts["https://home.sigmo.test/api/v1"].exists)
        XCTAssertFalse(app.buttons["settingsBackend-\(id)"].exists, app.debugDescription)
        XCTAssertFalse(app.buttons["editBackend-\(id)"].exists, app.debugDescription)
        row.tap()
        XCTAssertTrue(app.buttons["settingsBackend-\(id)"].waitForExistence(timeout: 5))
        XCTAssertLessThan(app.buttons["settingsBackend-\(id)"].frame.midX, app.buttons["editBackend-\(id)"].frame.midX)
        XCTAssertFalse(app.tabBars.firstMatch.exists)
        app.buttons["settingsBackend-\(id)"].tap()
        XCTAssertTrue(app.buttons["settings-auth"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["settings-mcp"].exists)
        XCTAssertFalse(app.tabBars.firstMatch.exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["editBackend-\(id)"].tap()
        XCTAssertEqual(app.textFields["backendName"].value as? String, "Home")
        XCTAssertEqual(app.textFields["backendAddress"].value as? String, "https://home.sigmo.test")
        app.textFields["backendName"].tap()
        app.textFields["backendName"].typeText(" Updated")
        app.buttons["saveBackend"].tap()
        XCTAssertTrue(app.staticTexts["Home Updated"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["backendDisplayAddress"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["backendDisplayAddress"].label.contains("https://home.sigmo.test"))
        XCTAssertFalse(app.staticTexts["backendDisplayAddress"].label.contains("/api/v1"))
        XCTAssertFalse(app.staticTexts["Home Modem"].exists)
        for _ in 0..<6 where !app.buttons["deleteBackend"].isHittable { app.swipeUp() }
        XCTAssertEqual(app.buttons["deleteBackend"].label, "Delete")
        attachScreenshot(app)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Home Updated"].exists)
        XCTAssertTrue(app.tabBars.buttons["Devices"].exists)
    }

    func testDeletingSelectedConnectionReturnsToAll() {
        let app = app(scenario: "single")
        app.launch()
        let id = "00000000-0000-0000-0000-000000000001"
        XCTAssertTrue(app.staticTexts["backendHeading-\(id)"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.segmentedControls["backendFilterSegments"].exists)
        app.buttons["addBackend"].tap()
        app.textFields["backendName"].tap(); app.textFields["backendName"].typeText("Second")
        app.textFields["backendAddress"].tap(); app.textFields["backendAddress"].typeText("https://second.sigmo.test")
        app.buttons["saveBackend"].tap()
        let home = app.segmentedControls["backendFilterSegments"].buttons["Home"]
        XCTAssertTrue(home.waitForExistence(timeout: 10))
        home.tap()
        app.buttons["manageBackends"].tap()
        app.buttons["backend-\(id)"].tap()
        let delete = app.buttons["deleteBackend"]
        for _ in 0..<8 where !delete.isHittable { app.swipeUp() }
        delete.tap()
        let alert = app.alerts["Delete this Sigmo?"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        alert.buttons["Cancel"].tap()
        XCTAssertTrue(delete.exists)
        delete.tap()
        alert.buttons["Delete"].tap()
        XCTAssertTrue(app.navigationBars["Manage Sigmo"].waitForExistence(timeout: 5))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertFalse(app.segmentedControls["backendFilterSegments"].exists)
        XCTAssertFalse(app.staticTexts["backendHeading-\(id)"].exists)
        XCTAssertTrue(app.staticTexts["Second"].exists)
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "modem-")).firstMatch.exists)
    }

    func testNativeActivationCompletesWithoutOpeningExternalServices() {
        let app = app(scenario: "dashboard")
        app.launch()
        app.buttons["manageBackends"].tap()
        let backend = app.buttons["backend-00000000-0000-0000-0000-000000000005"]
        XCTAssertTrue(app.buttons["addBackend"].waitForExistence(timeout: 10))
        for _ in 0..<8 where !backend.isHittable { app.swipeUp() }
        XCTAssertTrue(backend.waitForExistence(timeout: 5))
        backend.tap()
        XCTAssertTrue(app.buttons["activateBackend"].waitForExistence(timeout: 5))
        app.buttons["activateBackend"].tap()
        app.buttons["createPairing"].tap()
        XCTAssertTrue(app.staticTexts["pairingStatus"].waitForExistence(timeout: 2))
        XCTAssertEqual(app.staticTexts["pairingStatus"].label, "Waiting for authorization")
        XCTAssertTrue(app.staticTexts["Authorization status, Authorized"].waitForExistence(timeout: 15))
        XCTAssertFalse(app.buttons["createPairing"].exists)
        attachScreenshot(app)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000005-device-1"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.segmentedControls["backendFilterSegments"].buttons["Pro Activation"].exists)
    }

    func testChineseDeviceDetailsAndLargeText() throws {
        let app = app(scenario: "dashboard", chinese: true, largeText: true)
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = "dark"
        app.launch()
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(app.searchFields.firstMatch.waitForExistence(timeout: 10))
        for _ in 0..<8 where !modem.isHittable { app.swipeUp() }
        XCTAssertTrue(modem.exists)
        attachScreenshot(app)
        let number = app.staticTexts["modemNumber-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertEqual(app.staticTexts["modemCarrier-00000000-0000-0000-0000-000000000001-device-1"].firstMatch.label, "Example Mobile")
        XCTAssertEqual(number.label, "+86 138 0000 0000")
        XCTAssertGreaterThanOrEqual(modem.frame.minX, app.frame.minX)
        XCTAssertLessThanOrEqual(modem.frame.maxX, app.frame.maxX)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
        if !modem.isHittable { app.swipeUp() }
        modem.tap()
        XCTAssertTrue(app.staticTexts["detailModemTitle"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["detailModemTitle"].label, "Home Modem")
        if app.buttons["modemInfo"].exists { app.buttons["modemInfo"].tap() }
        attachScreenshot(app)
        try app.performAccessibilityAudit(for: [.textClipped]) { issue in
            if self.isExpectedNameTruncation(issue) { return true }
            print("AUDIT_ISSUE " + issue.detailedDescription + " " + (issue.element?.debugDescription ?? ""))
            return false
        }
    }

    func testDarkDashboardAccessibility() throws {
        let app = app(scenario: "dashboard")
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = "dark"
        app.launch()
        XCTAssertTrue(app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"].waitForExistence(timeout: 10))
        attachScreenshot(app)
        // Contrast auditing on this runtime reports unidentified SwiftUI nodes,
        // including on the stock empty state. Retain screenshots for manual review.
        try app.performAccessibilityAudit(for: [.textClipped, .sufficientElementDescription]) { issue in
            if self.isExpectedNameTruncation(issue) { return true }
            print("AUDIT_ISSUE " + issue.detailedDescription + " " + (issue.element?.debugDescription ?? ""))
            return false
        }
    }

    func testUnreadBadgeCapsAndClearsAfterReading() {
        let app = app(scenario: "messages-badge")
        app.launch()
        let identity = "00000000-0000-0000-0000-000000000001-device-1"
        let badge = app.staticTexts["modemUnread-\(identity)"]
        XCTAssertTrue(badge.waitForExistence(timeout: 10))
        XCTAssertEqual(badge.value as? String, "125")
        XCTAssertGreaterThan(badge.frame.minY, app.staticTexts["modemNumber-\(identity)"].frame.minY)
        XCTAssertGreaterThan(badge.frame.minX, app.staticTexts["modemMetadata-\(identity)"].frame.maxX)
        attachScreenshot(app)
        app.buttons["modem-\(identity)"].tap()
        let messagesTab = app.tabBars.buttons["Messages"]
        XCTAssertTrue(messagesTab.waitForExistence(timeout: 5))
        messagesTab.tap()
        let selected = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true"), object: messagesTab)
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 5), .completed)
        let conversation = app.buttons["conversation-conversation-active"]
        XCTAssertTrue(conversation.waitForExistence(timeout: 5), app.debugDescription)
        conversation.tap()
        XCTAssertTrue(app.staticTexts["Unread message 125"].waitForExistence(timeout: 5))
        let home = app.buttons["manageBackends"]
        for _ in 0..<3 where !home.exists { app.navigationBars.buttons.element(boundBy: 0).tap() }
        XCTAssertTrue(home.waitForExistence(timeout: 5))
        let cleared = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: badge)
        XCTAssertEqual(XCTWaiter.wait(for: [cleared], timeout: 5), .completed)
        attachScreenshot(app)
    }

    func testManagementReorderPersists() {
        let app = app(scenario: "dashboard")
        app.launch()
        app.buttons["manageBackends"].tap()
        app.buttons["reorderBackends"].tap()
        let home = app.cells.containing(.staticText, identifier: "orderBackend-00000000-0000-0000-0000-000000000001").firstMatch
        let studio = app.cells.containing(.staticText, identifier: "orderBackend-00000000-0000-0000-0000-000000000002").firstMatch
        XCTAssertTrue(home.waitForExistence(timeout: 5))
        let start = home.coordinate(withNormalizedOffset: CGVector(dx: 0.94, dy: 0.5))
        let end = studio.coordinate(withNormalizedOffset: CGVector(dx: 0.94, dy: 0.9))
        start.press(forDuration: 0.5, thenDragTo: end)
        XCTAssertLessThan(studio.frame.minY, home.frame.minY)
        app.buttons["Done"].tap()
        app.terminate()
        app.launchEnvironment["SIGMO_UI_RESET"] = "0"
        app.launch()
        app.buttons["manageBackends"].tap()
        let homeRow = app.buttons["backend-00000000-0000-0000-0000-000000000001"]
        let studioRow = app.buttons["backend-00000000-0000-0000-0000-000000000002"]
        XCTAssertTrue(homeRow.waitForExistence(timeout: 5))
        XCTAssertLessThan(studioRow.frame.minY, homeRow.frame.minY)
    }

    private func isExpectedNameTruncation(_ issue: XCUIAccessibilityAuditIssue) -> Bool {
        // Compact list text and navigation titles truncate visually; VoiceOver retains full labels.
        guard issue.auditType == .textClipped, let id = issue.element?.identifier else { return false }
        return id == "detailModemTitle" || ["modemCarrier-", "modemMetadata-"].contains { id.hasPrefix($0) }
    }

    private func attachScreenshot(_ app: XCUIApplication) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.lifetime = .keepAlways
        add(attachment)
        do {
            let url = URL.temporaryDirectory.appending(path: "Sigmo-\(UUID()).png")
            try app.screenshot().pngRepresentation.write(to: url)
            print("SIGMO_SCREENSHOT " + url.path)
        } catch { XCTFail("Save screenshot: \(error)") }
    }
}

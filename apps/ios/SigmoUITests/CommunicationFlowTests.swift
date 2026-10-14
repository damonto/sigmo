import XCTest

@MainActor
final class CommunicationFlowTests: XCTestCase {
    private func launch(multiple: Bool = false, chinese: Bool = false, history: Bool = false, modem: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = multiple ? "communication-multiple" : (history ? "communication-history" : "communication")
        if modem { app.launchEnvironment["SIGMO_UI_SCENARIO"] = "communication-modem" }
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = chinese ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", chinese ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        return app
    }

    private func capture(_ app: XCUIApplication, name: String) {
        let screenshot = app.screenshot()
        let image = XCTAttachment(screenshot: screenshot)
        image.name = name
        image.lifetime = .keepAlways
        add(image)
        let path = FileManager.default.temporaryDirectory.appendingPathComponent("sigmo-\(name).png")
        do {
            try screenshot.pngRepresentation.write(to: path)
            print("SIGMO_REVIEW_SCREENSHOT: \(path.path)")
        } catch { print("Screenshot export: \(error)") }
    }

    private func chooseStudioModem(_ app: XCUIApplication) {
        let picker = app.buttons["communication-modem-picker"]
        XCTAssertTrue(picker.waitForExistence(timeout: 10))
        picker.tap()
        let choices = app.collectionViews.buttons
        let modem = choices["Studio Modem"]
        XCTAssertTrue(modem.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(choices["Studio Source Modem"].exists)
        XCTAssertFalse(choices["Home Modem"].exists)
        XCTAssertFalse(choices["Source Modem"].exists)
        modem.tap()
        XCTAssertEqual(picker.label, "Studio Modem")
    }

    func testRootTabsContactDetailsAndCancelEdit() {
        let app = launch()
        let contacts = app.tabBars.buttons["Contacts"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 10)); contacts.tap()
        XCTAssertTrue(app.navigationBars["Contacts"].waitForExistence(timeout: 5))
        let alice = app.staticTexts["Alice"].firstMatch
        XCTAssertTrue(alice.waitForExistence(timeout: 10)); alice.tap()
        XCTAssertTrue(app.buttons["Edit Contact"].waitForExistence(timeout: 5))
        capture(app, name: "Contact details")
        let message = app.buttons["Message"].firstMatch
        XCTAssertTrue(message.isEnabled)
        message.tap()
        capture(app, name: "Contact number menu")
        let number = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "+14155552671")).firstMatch
        XCTAssertTrue(number.waitForExistence(timeout: 5), app.debugDescription); number.tap()
        capture(app, name: "Contact message composer")
        XCTAssertTrue(app.navigationBars["New message"].waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(app.textFields["Phone number"].value as? String, "+14155552671")
        app.buttons["Cancel"].tap()
        app.buttons["Call"].firstMatch.tap()
        let workNumber = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "+442079460018")).firstMatch
        XCTAssertTrue(workNumber.waitForExistence(timeout: 5)); workNumber.tap()
        XCTAssertTrue(app.textFields["calls-number"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["calls-number"].value as? String, "+442079460018")
        app.buttons["Cancel"].tap()
        app.buttons["Edit Contact"].tap()
        let name = app.textFields["Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5)); name.tap(); name.typeText(" changed")
        app.buttons["Cancel"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Alice changed"].exists)
        app.tabBars.buttons["Messages"].tap()
        XCTAssertTrue(app.navigationBars["Messages"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["No messages yet."].waitForExistence(timeout: 5))
        app.tabBars.buttons["Calls"].tap()
        XCTAssertTrue(app.navigationBars["Calls"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["No calls yet."].waitForExistence(timeout: 5))
        app.tabBars.buttons["Devices"].tap()
        XCTAssertTrue(app.navigationBars["Sigmo"].waitForExistence(timeout: 5))
    }

    func testBackendMenuSharesAndRemembersCommunicationSelection() {
        let app = launch(multiple: true)
        let contacts = app.tabBars.buttons["Contacts"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 10)); contacts.tap()
        XCTAssertTrue(app.staticTexts["Alice"].waitForExistence(timeout: 10))
        capture(app, name: "Initial all contacts")
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.buttons["communication-backend-filter"].label.contains("All"))
        let segments = app
        XCTAssertFalse(segments.buttons["All"].exists)
        app.buttons["communication-backend-filter"].tap()
        XCTAssertTrue(app.buttons["communication-backend-all"].exists)
        segments.buttons["communication-backend-Studio"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Alice"].exists)
        for tab in ["Messages", "Calls", "Contacts"] {
            app.tabBars.buttons[tab].tap()
            XCTAssertTrue(app.buttons["communication-backend-filter"].label.contains("Studio"))
        }
        capture(app, name: "Shared backend selection")
        app.terminate()
        app.launchEnvironment["SIGMO_UI_RESET"] = "0"
        app.launch()
        app.tabBars.buttons["Contacts"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["communication-backend-filter"].label.contains("Studio"))
        app.buttons["communication-backend-filter"].tap()
        segments.buttons["communication-backend-Home"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Studio Contact"].exists)
        app.buttons["communication-backend-filter"].tap()
        app.buttons["communication-backend-all"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Alice"].exists)
    }

    func testAllBackendsKeepHistoryAndContactActionsOnTheirSource() {
        let app = launch(multiple: true)
        XCTAssertTrue(app.tabBars.buttons["Messages"].waitForExistence(timeout: 10))
        app.tabBars.buttons["Messages"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Studio Contact"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "conversation-archive-thread").count, 2)
        app.staticTexts["Studio Contact"].firstMatch.tap()
        let modem = app.buttons["communication-modem-picker"]
        XCTAssertTrue(modem.waitForExistence(timeout: 10))
        XCTAssertEqual(modem.label, "Studio Modem")
        chooseStudioModem(app)
        capture(app, name: "All backends message source")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.tabBars.buttons["Calls"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertEqual(app.buttons.matching(identifier: "call-record-archive-call").count, 2)
        app.staticTexts["Studio Contact"].firstMatch.tap()
        app.buttons["call-detail-call"].tap()
        XCTAssertTrue(modem.waitForExistence(timeout: 10))
        XCTAssertEqual(modem.label, "Studio Modem")
        chooseStudioModem(app)
        app.buttons["Cancel"].tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.tabBars.buttons["Contacts"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.exists)
        app.staticTexts["Studio Contact"].firstMatch.tap()
        app.buttons["Edit Contact"].tap()
        XCTAssertTrue(app.textFields["Name"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["Name"].value as? String, "Studio Contact")
        capture(app, name: "Editing Studio contact")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label == %@ OR label == %@", "Studio", "Backend, Studio")).firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertFalse(app.buttons["contact-backend-picker"].exists)
        capture(app, name: "All backends contact source")
    }

    func testAllBackendsChooseDestinationOnlyWhenCreating() {
        let app = launch(multiple: true)
        XCTAssertTrue(app.tabBars.buttons["Contacts"].waitForExistence(timeout: 10))
        app.tabBars.buttons["Contacts"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10))
        app.buttons["modem-contact-actions"].tap()
        app.buttons["New contact"].tap()
        XCTAssertTrue(app.buttons.matching(identifier: "communication-create-Studio").firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.textFields["Name"].exists)
        capture(app, name: "Backend selection before cancel")
        if app.buttons["Cancel"].exists {
            app.buttons["Cancel"].tap()
        } else {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.8)).tap()
        }
        XCTAssertTrue(app.buttons.matching(identifier: "communication-create-Studio").firstMatch.waitForNonExistence(timeout: 5))
        XCTAssertFalse(app.textFields["Name"].exists)
        app.buttons["modem-contact-actions"].tap()
        app.buttons["New contact"].tap()
        app.buttons.matching(identifier: "communication-create-Studio").firstMatch.tap()
        let name = app.textFields["Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap(); name.typeText("Created from All")
        app.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts["Created from All"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["communication-backend-filter"].label.contains("All"))
        for (tab, compose) in [("Messages", "New message"), ("Calls", "Call")] {
            app.tabBars.buttons[tab].tap()
            let button = app.buttons[compose].firstMatch
            XCTAssertTrue(button.waitForExistence(timeout: 5)); button.tap()
            XCTAssertTrue(app.buttons.matching(identifier: "communication-create-Studio").firstMatch.waitForExistence(timeout: 5))
            capture(app, name: "All backends choose destination \(tab)")
            app.buttons.matching(identifier: "communication-create-Studio").firstMatch.tap()
            chooseStudioModem(app)
            app.buttons["Cancel"].tap()
        }
        app.tabBars.buttons["Contacts"].tap()
        app.buttons["communication-backend-filter"].tap()
        app.buttons["communication-backend-Home"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Created from All"].exists)
    }

    func testCommunicationFiltersUseDeviceSearchSpacing() {
        let app = launch(multiple: true)
        let devicesFilter = app.segmentedControls["backendFilterSegments"]
        XCTAssertTrue(devicesFilter.waitForExistence(timeout: 10))
        let deviceGap = devicesFilter.frame.minY - app.searchFields.firstMatch.frame.maxY
        for (tab, identifier) in [("Messages", "messages-unread-filter"), ("Calls", "calls-filter")] {
            app.tabBars.buttons[tab].tap()
            let filter = app.segmentedControls[identifier]
            XCTAssertTrue(filter.waitForExistence(timeout: 5))
            let gap = filter.frame.minY - app.searchFields.firstMatch.frame.maxY
            XCTAssertGreaterThanOrEqual(gap, 0)
            XCTAssertLessThanOrEqual(gap, deviceGap + 4)
            capture(app, name: "Search spacing \(tab)")
        }
    }

    func testChineseTitlesAndBackendControlsWithLargeText() {
        let app = launch(multiple: true, chinese: true)
        let contacts = app.tabBars.buttons["联系人"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 10)); contacts.tap()
        app.buttons["communication-backend-filter"].tap()
        let home = app.buttons["communication-backend-Home"]
        XCTAssertTrue(home.waitForExistence(timeout: 5)); home.tap()
        for title in ["联系人", "短信", "通话"] {
            app.tabBars.buttons[title].tap()
            XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 5))
            XCTAssertTrue(app.buttons["communication-backend-filter"].label.contains("Home"))
            XCTAssertTrue(app.buttons["communication-backend-filter"].isHittable)
            capture(app, name: "Chinese dark large text \(title)")
        }
        app.tabBars.buttons["联系人"].tap()
        let alice = app.staticTexts["Alice"].firstMatch
        XCTAssertTrue(alice.waitForExistence(timeout: 10)); alice.tap()
        let message = app.buttons["短信"].firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 5))
        XCTAssertTrue(message.isHittable)
        capture(app, name: "Chinese dark large contact")
    }
    func testAggregateHistoryUsesDeviceMessageAndCallComponents() {
        let app = launch(history: true)
        let messages = app.tabBars.buttons["Messages"]
        XCTAssertTrue(messages.waitForExistence(timeout: 10)); messages.tap()
        let thread = app.descendants(matching: .any)["conversation-archive-thread"].firstMatch
        XCTAssertTrue(thread.waitForExistence(timeout: 10))
        let messageFilter = app.segmentedControls["messages-unread-filter"]
        XCTAssertTrue(messageFilter.exists)
        XCTAssertGreaterThan(messageFilter.frame.minY, app.searchFields.firstMatch.frame.maxY)
        messageFilter.buttons["Unread"].tap()
        XCTAssertTrue(thread.waitForNonExistence(timeout: 5))
        XCTAssertTrue(messageFilter.isHittable)
        messageFilter.buttons["All"].tap()
        XCTAssertTrue(thread.waitForExistence(timeout: 5))
        capture(app, name: "Aggregate message filter")
        thread.tap()
        XCTAssertTrue(app.descendants(matching: .any)["message-1"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.textFields["message-input"].exists)
        XCTAssertFalse(app.buttons["message-send"].isEnabled)
        let selector = app.buttons["communication-modem-picker"]
        XCTAssertTrue(selector.exists)
        XCTAssertLessThan(selector.frame.maxY, 180)
        capture(app, name: "Shared message timeline")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.tabBars.buttons["Calls"].tap()
        let record = app.buttons["call-record-archive-call"]
        XCTAssertTrue(record.waitForExistence(timeout: 10))
        let callFilter = app.segmentedControls["calls-filter"]
        XCTAssertTrue(callFilter.exists)
        XCTAssertGreaterThan(callFilter.frame.minY, app.searchFields.firstMatch.frame.maxY)
        callFilter.buttons["Missed"].tap()
        XCTAssertTrue(record.waitForNonExistence(timeout: 5))
        XCTAssertTrue(callFilter.isHittable)
        callFilter.buttons["All"].tap()
        XCTAssertTrue(record.waitForExistence(timeout: 5))
        XCTAssertTrue(record.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Wi-Fi Calling ·")).firstMatch.exists)
        capture(app, name: "Aligned aggregate call history")
        record.tap()
        XCTAssertTrue(app.buttons["call-detail-call"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["call-detail-message"].exists)
        let callAction = app.buttons["call-detail-call"]
        let messageAction = app.buttons["call-detail-message"]
        XCTAssertEqual(callAction.frame.width, messageAction.frame.width, accuracy: 1)
        XCTAssertEqual(callAction.frame.height, messageAction.frame.height, accuracy: 1)
        XCTAssertEqual(callAction.frame.minX - messageAction.frame.maxX, 16, accuracy: 2)
        XCTAssertTrue(app.descendants(matching: .any)["call-detail-event"].firstMatch.exists)
        XCTAssertTrue(app.staticTexts["Line"].exists)
        XCTAssertFalse(app.staticTexts["Device"].exists)
        capture(app, name: "Shared call details")
    }

    func testNewContactUsesFilteredBackend() {
        let app = launch(multiple: true)
        XCTAssertTrue(app.tabBars.buttons["Contacts"].waitForExistence(timeout: 10))
        app.tabBars.buttons["Contacts"].tap()
        app.buttons["communication-backend-filter"].tap()
        app.buttons["communication-backend-Studio"].tap()
        XCTAssertTrue(app.staticTexts["Studio Contact"].waitForExistence(timeout: 10))
        app.buttons["modem-contact-actions"].tap()
        app.buttons["New contact"].tap()
        let name = app.textFields["Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["contact-backend-picker"].exists)
        XCTAssertFalse(app.staticTexts["Choose a Sigmo backend"].exists)
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label == %@ OR label == %@", "Studio", "Backend, Studio")).firstMatch.exists)
        name.tap(); name.typeText("Created in Studio")
        capture(app, name: "New contact follows backend filter")
        app.buttons["Save"].tap()
        XCTAssertTrue(name.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Created in Studio"].waitForExistence(timeout: 10))
        app.buttons["communication-backend-filter"].tap()
        app.buttons["communication-backend-Home"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Created in Studio"].exists)
        app.buttons["communication-backend-filter"].tap()
        app.buttons["communication-backend-Studio"].tap()
        XCTAssertTrue(app.staticTexts["Created in Studio"].waitForExistence(timeout: 10))
    }

    func testNewContactSheetAndSharedDialpad() {
        let app = launch()
        XCTAssertTrue(app.tabBars.buttons["Contacts"].waitForExistence(timeout: 10))
        app.tabBars.buttons["Contacts"].tap()
        app.buttons["modem-contact-actions"].tap()
        let add = app.buttons["New contact"]
        XCTAssertTrue(add.waitForExistence(timeout: 10)); add.tap()
        XCTAssertTrue(app.textFields["Name"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Add Photo"].exists)
        XCTAssertFalse(app.staticTexts["Backend"].exists)
        XCTAssertFalse(app.staticTexts["Backend, Home"].exists)
        XCTAssertFalse(app.buttons["Save"].isEnabled)
        app.buttons["Add phone number"].tap()
        XCTAssertTrue(app.textFields["Phone number"].exists)
        XCTAssertFalse(app.textFields["Country code for local numbers"].exists)
        capture(app, name: "New contact native sheet")
        app.buttons["Cancel"].tap()
        app.tabBars.buttons["Calls"].tap()
        let dial = app.buttons["Call"].firstMatch
        XCTAssertTrue(dial.waitForExistence(timeout: 5)); dial.tap()
        XCTAssertTrue(app.buttons["communication-modem-picker"].waitForExistence(timeout: 5))
        for digit in ["1", "2", "3"] { app.buttons["call-key-\(digit)"].tap() }
        app.buttons["calls-delete-digit"].tap()
        XCTAssertEqual(app.textFields["calls-number"].value as? String, "12")
        for id in ["call-key-1", "call-key-5", "call-key-0", "calls-dial"] {
            let button = app.buttons[id]
            XCTAssertTrue(button.isHittable)
            XCTAssertEqual(button.frame.width, button.frame.height, accuracy: 1)
        }
        capture(app, name: "Shared native dialpad")
    }

    func testModemContactsShareBackendAndSelectCurrentLine() {
        let app = openModemContacts()
        app.staticTexts["Alice"].firstMatch.tap()
        for calling in [false, true] {
            app.buttons[calling ? "Call" : "Message"].firstMatch.tap()
            let number = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "+14155552671")).firstMatch
            XCTAssertTrue(number.waitForExistence(timeout: 5)); number.tap()
            XCTAssertTrue(app.staticTexts["Example Mobile"].firstMatch.waitForExistence(timeout: 5))
            XCTAssertFalse(app.buttons["communication-modem-picker"].exists)
            capture(app, name: calling ? "Modem contact call" : "Modem contact message")
            app.buttons["Cancel"].tap()
        }
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["modem-contact-actions"].tap()
        app.buttons["New contact"].tap()
        let name = app.textFields["Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5)); name.tap(); name.typeText("Modem Contact")
        app.buttons["Save"].tap()
        XCTAssertTrue(name.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Modem Contact"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["backToDevices"].tap()
        app.tabBars.buttons["Contacts"].tap()
        XCTAssertTrue(app.staticTexts["Modem Contact"].firstMatch.waitForExistence(timeout: 5))
    }

    func testModemContactsChineseLargeText() throws {
        let app = openModemContacts(chinese: true)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            print("MODEM_CONTACTS_AUDIT \(issue.compactDescription): \(issue.element?.debugDescription ?? issue.detailedDescription)")
            return false
        }
        app.buttons["modem-contact-actions"].tap()
        app.buttons["新建联系人"].tap()
        XCTAssertTrue(app.textFields["姓名"].waitForExistence(timeout: 5))
        app.buttons["取消"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.waitForExistence(timeout: 5))
    }

    private func openModemContacts(chinese: Bool = false) -> XCUIApplication {
        let app = launch(chinese: chinese, modem: true)
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 10)); modem.tap()
        for title in chinese ? ["SIM", "短信", "通话", "联系人"] : ["SIM", "Messages", "Calls", "Contacts"] {
            XCTAssertTrue(app.tabBars.buttons[title].waitForExistence(timeout: 5))
        }
        XCTAssertFalse(app.tabBars.buttons[chinese ? "设置" : "Settings"].exists)
        XCTAssertTrue(app.buttons["modemSettings"].exists)
        XCTAssertLessThan(app.buttons["modemSettings"].frame.midX, app.buttons["modemInfo"].frame.midX)
        app.tabBars.buttons[chinese ? "联系人" : "Contacts"].tap()
        XCTAssertTrue(app.staticTexts["Alice"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertEqual(app.staticTexts["detailModemTitle"].label, "Home Modem")
        XCTAssertFalse(app.staticTexts["detailBackendName"].exists)
        capture(app, name: chinese ? "Modem contacts Chinese large text" : "Modem contacts")
        return app
    }

}

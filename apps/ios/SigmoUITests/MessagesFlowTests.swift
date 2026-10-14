import XCTest

@MainActor
final class MessagesFlowTests: XCTestCase {
    private func launch(chinese: Bool = false, large: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "messages"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = chinese ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 10)); modem.tap()
        selectMessages(in: app, chinese: chinese)
        return app
    }

    private func selectMessages(in app: XCUIApplication, chinese: Bool = false) {
        let tab = app.tabBars.buttons[chinese ? "短信" : "Messages"]
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND hittable == true"), object: tab)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed, app.debugDescription)
        tab.tap()
        let selected = XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true"), object: tab)
        XCTAssertEqual(XCTWaiter.wait(for: [selected], timeout: 5), .completed, "Messages tab should become selected")
        XCTAssertTrue(app.descendants(matching: .any)["messages-list"].firstMatch.waitForExistence(timeout: 5), app.debugDescription)
    }

    private func capture(_ app: XCUIApplication, _ name: String) {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name; attachment.lifetime = .keepAlways; add(attachment)
        let file = URL.temporaryDirectory.appending(path: "Messages-\(name).png")
        do {
            try screenshot.pngRepresentation.write(to: file)
            print("MESSAGES_SCREENSHOT " + file.path)
        } catch { XCTFail("Save Messages screenshot: \(error)") }
    }

    func testSearchIsVisibleMatchesOlderTextAndCancels() {
        let app = launch()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(search.isHittable, "Search must be discoverable without scrolling")
        XCTAssertEqual(search.placeholderValue, "Search numbers or messages")
        capture(app, "Messages native search")
        search.tap()
        search.typeText("Welcome")
        let active = app.buttons["conversation-conversation-active"]
        let inactive = app.buttons["conversation-conversation-inactive"]
        XCTAssertTrue(inactive.waitForNonExistence(timeout: 5))
        XCTAssertTrue(active.exists, "Search includes older messages beyond the row preview")
        capture(app, "Messages search results")
        search.buttons["Clear text"].tap()
        XCTAssertTrue(inactive.waitForExistence(timeout: 5))
        search.typeText("Welcome")
        XCTAssertTrue(inactive.waitForNonExistence(timeout: 5))
        cancelSearch(app)
        XCTAssertTrue(inactive.waitForExistence(timeout: 5))
        XCTAssertEqual(search.value as? String, search.placeholderValue)
        active.tap()
        XCTAssertTrue(app.staticTexts["Welcome to Sigmo / 欢迎使用 Sigmo"].waitForExistence(timeout: 5))
    }

    func testSearchStaysScopedAfterNavigationAndTabChanges() {
        let app = launch()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5), app.debugDescription)
        search.tap()
        search.typeText("Welcome")
        let active = app.buttons["conversation-conversation-active"]
        let inactive = app.buttons["conversation-conversation-inactive"]
        XCTAssertTrue(inactive.waitForNonExistence(timeout: 5))
        active.tap()
        XCTAssertTrue(app.staticTexts["Welcome to Sigmo / 欢迎使用 Sigmo"].waitForExistence(timeout: 5))
        if !active.isHittable {
            app.navigationBars.buttons.element(boundBy: 0).tap()
        }
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        XCTAssertEqual(search.value as? String, "Welcome")
        search.tap()
        cancelSearch(app)
        XCTAssertTrue(inactive.waitForExistence(timeout: 5))

        for tab in ["SIM", "Contacts", "Calls"] {
            app.tabBars.buttons[tab].tap()
            XCTAssertTrue(app.searchFields.allElementsBoundByIndex.allSatisfy {
                $0.placeholderValue != "Search numbers or messages"
            }, "Messages search must stay inside its own tab")
            app.tabBars.buttons["Messages"].tap()
            XCTAssertTrue(search.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertEqual(search.placeholderValue, "Search numbers or messages")
        }
        search.tap()
        search.typeText("Welcome")
        XCTAssertTrue(inactive.waitForNonExistence(timeout: 5))
        XCTAssertTrue(active.exists)
    }

    func testReturningHomePreservesSearch() {
        let app = launch()
        app.buttons["backToDevices"].tap()
        let homeSearch = app.searchFields["Search"]
        XCTAssertTrue(homeSearch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.segmentedControls["backendFilterSegments"].exists)
        homeSearch.tap()
        homeSearch.typeText("Home\n")
        let modem = app.buttons["modem-00000000-0000-0000-0000-000000000001-device-1"]
        XCTAssertTrue(modem.waitForExistence(timeout: 5))
        modem.tap()
        selectMessages(in: app)
        let messageSearch = app.searchFields["Search numbers or messages"]
        XCTAssertTrue(messageSearch.waitForExistence(timeout: 5), app.debugDescription)
        messageSearch.tap()
        messageSearch.typeText("Welcome\n")
        XCTAssertTrue(app.buttons["conversation-conversation-inactive"].waitForNonExistence(timeout: 5))
        app.tabBars.buttons["SIM"].tap()
        app.buttons["modemSettings"].tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["backToDevices"].tap()
        XCTAssertTrue(homeSearch.waitForExistence(timeout: 5))
        XCTAssertEqual(homeSearch.value as? String, "Home")
        XCTAssertFalse(app.segmentedControls["backendFilterSegments"].exists)
        XCTAssertTrue(modem.exists)
        XCTAssertFalse(messageSearch.exists)
    }

    func testSearchCombinesUnreadAndHistoryFilters() {
        let app = launch()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        search.tap()
        search.typeText("10086")
        app.segmentedControls["messages-unread-filter"].buttons["Unread"].tap()
        XCTAssertTrue(app.buttons["conversation-conversation-inactive"].waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["conversation-conversation-active"].exists)
        app.segmentedControls["messages-unread-filter"].buttons["All"].tap()
        XCTAssertTrue(app.buttons["conversation-conversation-inactive"].waitForExistence(timeout: 5))
        app.buttons["messages-history-filter"].tap()
        app.buttons["Unassigned historical lines"].tap()
        XCTAssertTrue(app.buttons["conversation-conversation-history"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["conversation-conversation-active"].exists)
        capture(app, "Messages search with line history")
    }

    func testSearchNoResultsChineseDarkAndLargeText() throws {
        let app = launch(chinese: true, large: true)
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        XCTAssertTrue(search.isHittable)
        XCTAssertEqual(search.placeholderValue, "搜索号码或信息")
        search.tap()
        search.typeText("no-match-123")
        let empty = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "no-match-123")).firstMatch
        XCTAssertTrue(empty.waitForExistence(timeout: 5), app.debugDescription)
        capture(app, "Messages Chinese search empty state")
        cancelSearch(app)
        XCTAssertTrue(app.buttons["conversation-conversation-active"].waitForExistence(timeout: 5))
        XCTAssertFalse(empty.exists)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
    }

    private func cancelSearch(_ app: XCUIApplication) {
        // Newer systems expose the native cancel control as a Close symbol.
        let cancel = app.buttons.matching(NSPredicate(format: "label IN %@", ["Close", "Cancel", "关闭", "取消"])).firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 5), app.debugDescription)
        cancel.tap()
    }

    func testMessageTabReadReplyAndLineHistory() {
        let app = launch()
        let active = app.buttons["conversation-conversation-active"]
        XCTAssertTrue(active.waitForExistence(timeout: 5)); active.tap()
        let input = app.descendants(matching: .any)["message-input"].firstMatch
        XCTAssertTrue(input.waitForExistence(timeout: 5), app.debugDescription)
        assertConversationTitle(app, line: "Home / 主线路")
        capture(app, "Messages glass header and composer")
        input.tap(); input.typeText("Message from fixture UI")
        XCTAssertTrue(app.buttons["message-send"].isHittable)
        XCTAssertLessThanOrEqual(input.frame.maxY, app.keyboards.firstMatch.frame.minY)
        app.buttons["message-send"].tap()
        XCTAssertTrue(app.staticTexts["Message from fixture UI"].waitForExistence(timeout: 5))
        capture(app, "Messages reply")
        if app.buttons["conversation-conversation-inactive"].isHittable {
            app.buttons["conversation-conversation-inactive"].tap()
        } else {
            app.navigationBars.buttons.element(boundBy: 0).tap()
            app.buttons["conversation-conversation-inactive"].tap()
        }
        XCTAssertTrue(app.staticTexts["History on your Travel line / 旅行线路历史"].waitForExistence(timeout: 5))
        assertConversationTitle(app, line: "Travel / 旅行")
        XCTAssertFalse(app.buttons["message-send"].isEnabled)
        capture(app, "Inactive line history")
    }

    func testComposeAndCancelRetainsDraft() {
        let app = launch()
        app.buttons["new-message"].tap()
        let recipient = app.textFields["message-recipient"]
        XCTAssertTrue(recipient.waitForExistence(timeout: 5)); recipient.typeText("10086")
        let input = app.descendants(matching: .any)["message-input"].firstMatch
        input.tap(); input.typeText("Unsent fixture draft")
        app.buttons["Cancel"].tap()
        app.buttons["new-message"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["message-input"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertEqual(app.descendants(matching: .any)["message-input"].firstMatch.value as? String, "Unsent fixture draft")
        app.buttons["message-send"].tap()
        XCTAssertTrue(app.staticTexts["Unsent fixture draft"].waitForExistence(timeout: 5))
    }

    func testChineseDarkModeAndDynamicType() throws {
        let app = launch(chinese: true, large: true)
        let active = app.buttons["conversation-conversation-active"]
        XCTAssertTrue(active.waitForExistence(timeout: 5)); active.tap()
        XCTAssertTrue(app.buttons["message-send"].waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertFalse(app.buttons["message-send"].label.isEmpty)
        assertConversationTitle(app, line: "Home / 主线路")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
        capture(app, "Messages Chinese dark accessibility size")
    }

    private func assertConversationTitle(_ app: XCUIApplication, line expectedLine: String) {
        let inline = app.staticTexts["message-participant"].firstMatch.exists
        let labels = inline ? app.staticTexts : app.navigationBars.staticTexts
        let participant = labels[inline ? "message-participant" : "10086"].firstMatch
        let line = labels[inline ? "message-line" : expectedLine].firstMatch
        XCTAssertTrue(participant.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(participant.label, "10086")
        XCTAssertTrue(line.exists, app.debugDescription)
        XCTAssertEqual(line.label, expectedLine)
        // Native title accessibility frames can overlap slightly due to pixel rounding.
        XCTAssertGreaterThanOrEqual(line.frame.minY, participant.frame.maxY - 1)
        if !inline {
            XCTAssertTrue(app.navigationBars.firstMatch.frame.contains(line.frame))
        }
        let send = app.buttons["message-send"]
        XCTAssertGreaterThanOrEqual(send.frame.width, 44)
        XCTAssertGreaterThanOrEqual(send.frame.height, 44)
    }
}

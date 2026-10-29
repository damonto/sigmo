import XCTest

@MainActor
final class CallsFlowTests: XCTestCase {
    private func launch(chinese: Bool = false, large: Bool = false, recordings: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "calls"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        if recordings { app.launchEnvironment["SIGMO_UI_RECORDINGS"] = "1" }
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = chinese ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 10)); modem.tap()
        let tab = app.tabBars.buttons[chinese ? "通话" : "Calls"]
        XCTAssertTrue(tab.waitForExistence(timeout: 5), app.debugDescription); tab.tap()
        XCTAssertTrue(app.buttons["call-record-past-call"].waitForExistence(timeout: 5), app.debugDescription)
        return app
    }

    private func launchCallback(scenario: String, mode: String) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = scenario
        app.launchEnvironment["SIGMO_UI_CALLBACK"] = mode
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = "light"
        app.launchArguments = ["-AppleLanguages", "(en)", "-AppleLocale", "en_US",
                               "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryL"]
        app.launch()
        return app
    }

    private func capture(_ app: XCUIApplication, _ name: String) {
        let image = XCTAttachment(screenshot: app.screenshot())
        image.name = name; image.lifetime = .keepAlways; add(image)
        let file = URL.temporaryDirectory.appending(path: "Calls-\(name).png")
        do {
            try app.screenshot().pngRepresentation.write(to: file)
            print("CALLS_SCREENSHOT " + file.path)
        } catch { XCTFail("Save Calls screenshot: \(error)") }
    }

    private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<12 {
            if element.exists && element.isHittable { return }
            let scroll = app.scrollViews.firstMatch.exists ? app.scrollViews.firstMatch : app.collectionViews.firstMatch
            if element.exists && element.frame.maxY < scroll.frame.midY { scroll.swipeDown() }
            else { scroll.swipeUp() }
        }
        XCTAssertTrue(element.exists && element.isHittable, app.debugDescription)
    }

    private func saveRecording(_ app: XCUIApplication, chinese: Bool = false, count: Int = 1) {
        app.buttons["call-record-past-call"].tap()
        reveal(app.buttons["call-detail-call"], in: app)
        app.buttons["call-detail-call"].tap()
        let record = app.buttons["calls-record"]
        XCTAssertTrue(app.buttons["calls-minimize"].waitForExistence(timeout: 5), app.debugDescription)
        reveal(record, in: app)
        for index in 0..<count {
            let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: record)
            XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
            record.tap()
            XCTAssertTrue(app.buttons[chinese ? "停止录音" : "Stop Recording"].waitForExistence(timeout: 5))
            if index < count - 1 {
                record.tap()
                XCTAssertTrue(app.buttons[chinese ? "停止录音" : "Stop Recording"].waitForNonExistence(timeout: 5))
            }
        }
        if chinese { capture(app, "Call controls Chinese large text") }
        reveal(app.buttons["calls-end"], in: app)
        app.buttons["calls-end"].tap()
        XCTAssertTrue(app.buttons["calls-end"].waitForNonExistence(timeout: 5))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.buttons["call-record-dialed-call"].waitForExistence(timeout: 5))
    }

    func testRecordingLibraryPlaybackAndBackgroundCleanup() {
        let app = launch(recordings: true)
        saveRecording(app)
        app.buttons["Recordings"].tap()
        let toggle = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "recording-toggle-")).firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertFalse(app.staticTexts["Recording saved"].exists)
        XCTAssertFalse(app.buttons["Recover Recording"].exists)
        XCTAssertFalse(app.buttons["Stop"].exists)
        XCTAssertFalse(app.buttons["Share"].exists)
        let id = String(toggle.identifier.dropFirst("recording-toggle-".count))
        let slider = app.sliders["recording-progress-\(id)"]
        let elapsed = app.staticTexts["recording-elapsed-\(id)"]
        let duration = app.staticTexts["recording-duration-\(id)"]
        XCTAssertTrue(slider.waitForExistence(timeout: 5))
        XCTAssertFalse(slider.isEnabled)
        XCTAssertEqual(elapsed.label, "0:00")
        XCTAssertNotEqual(duration.label, "0:00")
        XCTAssertGreaterThanOrEqual(toggle.frame.width, 44)
        XCTAssertGreaterThanOrEqual(toggle.frame.height, 44)
        XCTAssertEqual(toggle.frame.width, toggle.frame.height, accuracy: 1)
        XCTAssertGreaterThan(toggle.frame.minX, app.staticTexts["recording-number-\(id)"].frame.maxX)
        XCTAssertGreaterThanOrEqual(slider.frame.minY, toggle.frame.maxY)
        XCTAssertGreaterThanOrEqual(elapsed.frame.minY, slider.frame.maxY)
        capture(app, "Recording ready")
        toggle.tap()
        XCTAssertTrue(slider.isEnabled)
        XCTAssertEqual(toggle.label, "Pause")
        toggle.tap()
        XCTAssertEqual(toggle.label, "Resume Playback")
        slider.adjust(toNormalizedSliderPosition: 0.5)
        XCTAssertEqual(toggle.label, "Resume Playback")
        XCTAssertNotEqual(elapsed.label, "0:00")
        toggle.tap()
        XCTAssertEqual(toggle.label, "Pause")
        capture(app, "Recording player")
        XCUIDevice.shared.press(.home)
        app.activate()
        XCTAssertEqual(toggle.label, "Play")
        XCTAssertTrue(slider.exists)
        XCTAssertFalse(slider.isEnabled)
        XCTAssertEqual(elapsed.label, "0:00")
        toggle.tap()
        app.buttons["Done"].tap()
        app.buttons["Recordings"].tap()
        XCTAssertEqual(toggle.label, "Play")
        XCTAssertTrue(slider.exists)
        XCTAssertFalse(slider.isEnabled)
        XCTAssertEqual(elapsed.label, "0:00")
    }

    func testRecordingDetailsChineseLargeTextPlayback() throws {
        let app = launch(chinese: true, large: true, recordings: true)
        saveRecording(app, chinese: true)
        app.buttons["call-record-dialed-call"].tap()
        let toggle = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "recording-toggle-")).firstMatch
        reveal(toggle, in: app)
        XCTAssertEqual(toggle.label, "播放")
        XCTAssertFalse(app.staticTexts["录音已中断"].exists)
        let id = String(toggle.identifier.dropFirst("recording-toggle-".count))
        let slider = app.sliders["recording-progress-\(id)"]
        reveal(slider, in: app)
        XCTAssertFalse(slider.isEnabled)
        XCTAssertEqual(slider.label, "播放进度")
        reveal(toggle, in: app)
        XCTAssertGreaterThanOrEqual(toggle.frame.width, 44)
        XCTAssertEqual(toggle.frame.width, toggle.frame.height, accuracy: 1)
        XCTAssertGreaterThan(toggle.frame.minX, app.staticTexts["recording-number-\(id)"].frame.maxX)
        toggle.tap()
        XCTAssertEqual(toggle.label, "暂停")
        toggle.tap()
        XCTAssertEqual(toggle.label, "继续播放")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
        capture(app, "Recording details Chinese large text")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["call-record-dialed-call"].tap()
        reveal(toggle, in: app)
        XCTAssertEqual(toggle.label, "播放")
        reveal(slider, in: app)
        XCTAssertFalse(slider.isEnabled)
        XCTAssertEqual(app.staticTexts["recording-elapsed-\(id)"].label, "0:00")
    }

    func testRecordingDeletionRequiresConfirmationWhilePlaying() {
        let app = launch(recordings: true)
        saveRecording(app)
        app.buttons["Recordings"].tap()
        let toggle = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "recording-toggle-")).firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 10))
        let id = String(toggle.identifier.dropFirst("recording-toggle-".count))
        let row = app.cells.containing(.button, identifier: toggle.identifier).firstMatch
        toggle.tap()
        row.swipeLeft()
        let delete = app.buttons["recording-delete-\(id)"]
        XCTAssertTrue(delete.waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.exists, "Swiping must not execute deletion")
        delete.tap()
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        capture(app, "Confirm recording deletion")
        alert.buttons["Cancel"].tap()
        XCTAssertTrue(toggle.exists)
        XCTAssertEqual(toggle.label, "Pause")
        row.swipeLeft()
        delete.tap()
        alert.buttons["Delete"].tap()
        XCTAssertTrue(toggle.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["No Recordings"].exists)
        app.buttons["Done"].tap()
        app.buttons["Recordings"].tap()
        XCTAssertFalse(toggle.exists)
    }

    func testSwitchingRecordingsResetsOnlyThePreviousRow() {
        let app = launch(recordings: true)
        saveRecording(app, count: 2)
        app.buttons["Recordings"].tap()
        let toggles = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "recording-toggle-"))
        XCTAssertTrue(toggles.element(boundBy: 1).waitForExistence(timeout: 10))
        XCTAssertEqual(toggles.count, 2)
        let first = toggles.element(boundBy: 0)
        let second = toggles.element(boundBy: 1)
        let firstID = String(first.identifier.dropFirst("recording-toggle-".count))
        let secondID = String(second.identifier.dropFirst("recording-toggle-".count))
        let firstProgress = app.sliders["recording-progress-\(firstID)"]
        let secondProgress = app.sliders["recording-progress-\(secondID)"]
        first.tap()
        first.tap()
        firstProgress.adjust(toNormalizedSliderPosition: 0.5)
        XCTAssertNotEqual(app.staticTexts["recording-elapsed-\(firstID)"].label, "0:00")
        XCTAssertEqual(app.staticTexts["recording-elapsed-\(secondID)"].label, "0:00")
        XCTAssertFalse(secondProgress.isEnabled)
        second.tap()
        XCTAssertEqual(first.label, "Play")
        XCTAssertFalse(firstProgress.isEnabled)
        XCTAssertEqual(app.staticTexts["recording-elapsed-\(firstID)"].label, "0:00")
        XCTAssertEqual(second.label, "Pause")
        XCTAssertTrue(secondProgress.isEnabled)
        capture(app, "Switch recording playback")
        secondProgress.adjust(toNormalizedSliderPosition: 0.9)
        let ended = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label == %@", "Play"), object: second)
        XCTAssertEqual(XCTWaiter.wait(for: [ended], timeout: 10), .completed)
        XCTAssertFalse(secondProgress.isEnabled)
        XCTAssertEqual(app.staticTexts["recording-elapsed-\(secondID)"].label, "0:00")
    }

    func testDetailsAndGlobalCallSurviveTabNavigation() {
        let app = launch()
        capture(app, "Aligned call history")
        app.buttons["call-record-past-call"].tap()
        XCTAssertTrue(app.staticTexts["Call Route"].waitForExistence(timeout: 5))
        capture(app, "Native Calls details")
        app.buttons["call-detail-call"].tap()
        XCTAssertTrue(app.buttons["calls-minimize"].waitForExistence(timeout: 5), app.debugDescription)
        capture(app, "Connected call")
        app.buttons["calls-minimize"].tap()
        XCTAssertTrue(app.buttons["calls-minimize"].waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["calls-return"].waitForExistence(timeout: 5))
        capture(app, "Minimized call above navigation")
        let sim = app.tabBars.buttons["SIM"]
        if !sim.exists {
            let back = app.navigationBars.buttons.element(boundBy: 0)
            XCTAssertGreaterThanOrEqual(back.frame.minY, app.buttons["calls-return"].frame.maxY - 1, "The call bar must not cover navigation")
            back.tap()
        }
        XCTAssertTrue(sim.waitForExistence(timeout: 5), app.debugDescription)
        sim.tap()
        app.buttons["modemSettings"].tap()
        XCTAssertTrue(app.buttons["calls-return"].exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons["backToDevices"].tap()
        XCTAssertTrue(app.buttons["calls-return"].exists, "The call survives returning to Home")
        app.buttons["calls-return"].tap()
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 5))
        app.buttons["calls-end"].tap()
        XCTAssertFalse(app.buttons["calls-return"].waitForExistence(timeout: 1))
    }

    func testDetailsMessagePrefillsRecipientAndCancels() throws {
        let app = launch()
        app.buttons["call-record-past-call"].tap()
        let call = app.buttons["call-detail-call"]
        let message = app.buttons["call-detail-message"]
        XCTAssertTrue(message.waitForExistence(timeout: 5))
        XCTAssertEqual(call.label, "Call")
        XCTAssertEqual(message.label, "Message")
        XCTAssertTrue(call.isEnabled && message.isEnabled)
        XCTAssertEqual(call.frame.width, message.frame.width, accuracy: 1)
        XCTAssertEqual(call.frame.midY, message.frame.midY, accuracy: 1)
        XCTAssertEqual(call.frame.width, call.frame.height, accuracy: 1)
        XCTAssertEqual(message.frame.width, message.frame.height, accuracy: 1)
        XCTAssertGreaterThanOrEqual(message.frame.height, 44)
        XCTAssertGreaterThanOrEqual(call.frame.minY, app.staticTexts["call-detail-number"].frame.maxY)
        capture(app, "Liquid Glass call details")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            XCTFail("\(issue.compactDescription): \(issue.element?.debugDescription ?? issue.detailedDescription)")
            return false
        }

        message.tap()
        let recipient = app.textFields["message-recipient"]
        XCTAssertTrue(recipient.waitForExistence(timeout: 5))
        XCTAssertEqual(recipient.value as? String, "10086")
        XCTAssertFalse(app.buttons["message-send"].isEnabled)
        app.buttons["Cancel"].tap()
        XCTAssertTrue(message.waitForExistence(timeout: 5))
        XCTAssertFalse(recipient.exists)
        XCTAssertFalse(app.buttons["calls-end"].exists)
    }

    func testDetailsChineseDarkLargeTextActions() throws {
        let app = launch(chinese: true, large: true)
        app.buttons["call-record-missed-call"].tap()
        let call = app.buttons["call-detail-call"]
        let message = app.buttons["call-detail-message"]
        reveal(message, in: app)
        XCTAssertEqual(call.label, "拨号")
        XCTAssertEqual(message.label, "短信")
        XCTAssertEqual(call.frame.width, message.frame.width, accuracy: 1)
        XCTAssertEqual(call.frame.midY, message.frame.midY, accuracy: 1)
        XCTAssertEqual(call.frame.width, call.frame.height, accuracy: 1)
        XCTAssertEqual(message.frame.width, message.frame.height, accuracy: 1)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped])
        capture(app, "Liquid Glass details Chinese dark large text")
        message.tap()
        XCTAssertTrue(app.textFields["message-recipient"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["message-recipient"].value as? String, "10086")
        app.buttons["取消"].tap()
        let event = app.descendants(matching: .any)["call-detail-event"].firstMatch
        reveal(event, in: app)
        XCTAssertTrue(event.label.contains("未接来电"))
    }

    func testDetailsMessageSendsThroughSameDevice() {
        let app = launch()
        app.buttons["call-record-past-call"].tap()
        app.buttons["call-detail-message"].tap()
        let input = app.descendants(matching: .any)["message-input"].firstMatch
        XCTAssertTrue(input.waitForExistence(timeout: 5))
        input.tap()
        input.typeText("Reply from call details")
        app.buttons["message-send"].tap()
        XCTAssertTrue(app.textFields["message-recipient"].waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["call-detail-message"].exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.tabBars.buttons["Messages"].tap()
        let conversation = app.buttons["conversation-conversation-active"]
        XCTAssertTrue(conversation.waitForExistence(timeout: 5), app.debugDescription)
        conversation.tap()
        XCTAssertTrue(app.staticTexts["Reply from call details"].waitForExistence(timeout: 5), app.debugDescription)
    }

    func testDialpadDismissesBeforePresentingCall() {
        let app = launch()
        app.buttons["calls-keypad"].tap()
        XCTAssertEqual(app.buttons["call-key-0"].value as? String, "+")
        for digit in ["1", "2", "3", "4", "5"] { app.buttons["call-key-\(digit)"].tap() }
        app.buttons["calls-delete-digit"].tap()
        XCTAssertEqual(app.textFields["calls-number"].value as? String, "1234")
        capture(app, "Liquid Glass dialpad")
        reveal(app.buttons["calls-dial"], in: app)
        XCTAssertEqual(app.buttons["calls-dial"].frame.width, app.buttons["calls-dial"].frame.height, accuracy: 1)
        app.buttons["calls-dial"].tap()
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertFalse(app.buttons["calls-dial"].exists)
        let topRow = ["calls-mute", "calls-show-keypad", "calls-audio"].map { app.buttons[$0] }
        let bottomRow = ["calls-hold", "calls-end", "calls-record"].map { app.buttons[$0] }
        for row in [topRow, bottomRow] {
            for button in row {
                XCTAssertTrue(button.isHittable)
                XCTAssertEqual(button.frame.midY, row[0].frame.midY, accuracy: 1)
                XCTAssertGreaterThanOrEqual(button.frame.height, 44)
                XCTAssertEqual(button.frame.width, button.frame.height, accuracy: 1)
            }
            XCTAssertLessThan(row[0].frame.maxX, row[1].frame.minX)
            if row.count == 3 { XCTAssertLessThan(row[1].frame.maxX, row[2].frame.minX) }
            XCTAssertGreaterThanOrEqual(row[1].frame.minX - row[0].frame.maxX, 32)
            if row.count == 3 { XCTAssertGreaterThanOrEqual(row[2].frame.minX - row[1].frame.maxX, 32) }
        }
        XCTAssertLessThan(topRow[0].frame.maxY, bottomRow[0].frame.minY)
        for (top, bottom) in zip(topRow, bottomRow) {
            XCTAssertEqual(top.frame.midX, bottom.frame.midX, accuracy: 1)
        }
        app.buttons["calls-mute"].tap()
        XCTAssertTrue(app.buttons["calls-mute"].isSelected)
        XCTAssertEqual(app.buttons["calls-mute"].frame.width, app.buttons["calls-mute"].frame.height, accuracy: 1)
        capture(app, "Selected circular call control")
        app.buttons["calls-mute"].tap()
        app.buttons["calls-hold"].tap()
        XCTAssertTrue(app.buttons["Resume"].waitForExistence(timeout: 5))
        app.buttons["calls-hold"].tap()
        XCTAssertTrue(app.buttons["Hold"].waitForExistence(timeout: 5))
        capture(app, "Call from dialpad")
        app.buttons["calls-show-keypad"].tap()
        assertInCallKeypadVisible(app)
        app.buttons["call-key-1"].tap()
        XCTAssertEqual(app.staticTexts["calls-sent-digits"].value as? String, "1")
        let zero = app.buttons["call-key-0"]
        XCTAssertNotEqual(zero.value as? String, "+", "In-call DTMF cannot send a plus sign")
        zero.tap()
        XCTAssertEqual(app.staticTexts["calls-sent-digits"].value as? String, "10")
        app.buttons["calls-hide-keypad"].tap()
        XCTAssertTrue(app.buttons["calls-show-keypad"].isHittable)
        app.buttons["calls-show-keypad"].tap()
        assertInCallKeypadVisible(app)
        app.buttons["calls-end"].tap()
        XCTAssertTrue(app.buttons["call-record-past-call"].waitForExistence(timeout: 5))
    }

    func testSystemCallbackOpensHomeDialerWithNumber() {
        let app = launchCallback(scenario: "calls", mode: "home")
        let number = app.textFields["calls-number"]
        XCTAssertTrue(number.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertEqual(number.value as? String, "10086")
    }

    func testSystemCallbackOpensModemDialerWithNumber() {
        let app = launchCallback(scenario: "communication-modem", mode: "modem")
        let number = app.textFields["calls-number"]
        XCTAssertTrue(number.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertEqual(number.value as? String, "10086")
    }

    func testSystemCallbackDialsCompleteRouteDirectly() {
        let app = launchCallback(scenario: "calls", mode: "dial")
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.buttons["calls-show-keypad"].exists)
        XCTAssertTrue(app.staticTexts["10086"].firstMatch.exists)
    }

    func testAudioCallIntentCallbackDialsCompleteRouteDirectly() {
        let app = launchCallback(scenario: "calls", mode: "audio")
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.staticTexts["10086"].firstMatch.exists)
    }

    func testLegacyAudioCallIntentCallbackDialsCompleteRouteDirectly() {
        let app = launchCallback(scenario: "calls", mode: "legacy-audio")
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.staticTexts["10086"].firstMatch.exists)
    }

    func testSystemCallbackUsesLastActiveLineAcrossBackends() {
        let app = launchCallback(scenario: "calls-multiple", mode: "latest-line")
        XCTAssertTrue(app.buttons["calls-end"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Studio ·")).firstMatch.exists)
        XCTAssertFalse(app.sheets["Choose a Sigmo backend"].exists)
    }

    func testUnavailableSystemCallbackShowsAllBackendsBeforeDialing() {
        let app = launchCallback(scenario: "communication-multiple", mode: "unavailable-line")
        let chooser = app.sheets["Choose a Sigmo backend"]
        let home = chooser.buttons["Home"]
        let studio = chooser.buttons["Studio"]
        XCTAssertTrue(home.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(studio.exists)
        XCTAssertFalse(app.buttons["calls-end"].exists)
        studio.tap()
        let number = app.textFields["calls-number"]
        XCTAssertTrue(number.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(number.value as? String, "10086")
        XCTAssertFalse(app.buttons["calls-end"].exists, "Choosing a backend still requires explicit dialing")
    }

    func testInCallKeypadFitsLargeTextAndLandscapeWithoutScrolling() {
        let app = launch(chinese: true, large: true)
        app.buttons["call-record-past-call"].tap()
        reveal(app.buttons["call-detail-call"], in: app)
        app.buttons["call-detail-call"].tap()
        let keypad = app.buttons["calls-show-keypad"]
        XCTAssertTrue(keypad.waitForExistence(timeout: 5))
        reveal(keypad, in: app)
        keypad.tap()
        assertInCallKeypadVisible(app)
        capture(app, "Full screen keypad Chinese large text")
        XCUIDevice.shared.orientation = .landscapeLeft
        assertInCallKeypadVisible(app)
        capture(app, "Full screen keypad landscape")
        app.buttons["call-key-9"].tap()
        XCTAssertEqual(app.staticTexts["calls-sent-digits"].value as? String, "9")
        app.buttons["calls-hide-keypad"].tap()
        XCTAssertTrue(keypad.waitForExistence(timeout: 5))
        XCUIDevice.shared.orientation = .portrait
        reveal(keypad, in: app)
        keypad.tap()
        assertInCallKeypadVisible(app)
        XCTAssertEqual(app.staticTexts["calls-sent-digits"].value as? String, "9")
        app.buttons["calls-end"].tap()
    }

    private func assertInCallKeypadVisible(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        for id in ["1", "2", "3", "4", "5", "6", "7", "8", "9", "*", "0", "#"].map({ "call-key-" + $0 }) + ["calls-hide-keypad", "calls-end"] {
            let button = app.buttons[id]
            XCTAssertTrue(button.isHittable, id, file: file, line: line)
            XCTAssertTrue(app.frame.contains(button.frame), "Offscreen: \(id) \(button.frame)", file: file, line: line)
            XCTAssertGreaterThanOrEqual(button.frame.height, 44, id, file: file, line: line)
        }
        let duration = app.staticTexts["calls-keypad-duration"]
        let number = app.staticTexts["calls-sent-digits"]
        XCTAssertTrue(duration.exists, file: file, line: line)
        XCTAssertLessThan(duration.frame.maxY, number.frame.minY, "Duration must appear above the number",
                          file: file, line: line)
        if app.frame.width < app.frame.height {
            XCTAssertLessThan(number.frame.maxY, app.buttons["call-key-1"].frame.minY,
                              "The number must appear above the keypad", file: file, line: line)
            XCTAssertEqual(app.buttons["calls-end"].frame.midX, app.frame.midX, accuracy: 1,
                           "End Call must stay centered below the keypad", file: file, line: line)
            let keys = ["call-key-*", "call-key-0", "call-key-#"].map { app.buttons[$0] }
            let keypadBottom = keys.map(\.frame.maxY).max() ?? 0
            XCTAssertLessThanOrEqual(app.buttons["calls-end"].frame.minY - keypadBottom, 48,
                                     "Keypad actions must stay close to the keys", file: file, line: line)
        }
        XCTAssertGreaterThan(app.buttons["calls-hide-keypad"].frame.midX, app.buttons["calls-end"].frame.midX,
                             "Hide Keypad must stay to the right of End Call", file: file, line: line)
        XCTAssertFalse(app.scrollViews.firstMatch.exists, file: file, line: line)
    }

    func testKeypadUSSDDoesNotCreateCall() {
        let app = launch()
        app.buttons["calls-keypad"].tap()
        for digit in ["*", "1", "2", "3", "#"] {
            let key = app.buttons["call-key-\(digit)"]
            XCTAssertTrue(key.waitForExistence(timeout: 5)); key.tap()
        }
        app.buttons["calls-dial"].tap()
        XCTAssertTrue(app.staticTexts["1. Balance / 余额\n2. Usage / 用量"].waitForExistence(timeout: 5), app.debugDescription)
        let reply = app.textFields["ussdReply"]
        reply.tap()
        reply.typeText("1")
        app.buttons["sendUSSDReply"].tap()
        XCTAssertTrue(app.staticTexts["Balance: 100.00 / 余额 100.00"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["calls-return"].exists)
        app.buttons["Done"].tap()
        XCTAssertTrue(app.buttons["call-record-past-call"].exists)
    }

    func testChineseDarkModeAndDynamicType() throws {
        let app = launch(chinese: true, large: true)
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
        capture(app, "Calls Chinese dark accessibility size")
        app.buttons["calls-keypad"].tap()
        let contacts = app.buttons["calls-contacts"]
        reveal(contacts, in: app)
        XCTAssertEqual(contacts.label, "通讯录")
        XCTAssertTrue(contacts.isHittable)
        XCTAssertTrue(app.frame.contains(contacts.frame))
        capture(app, "Keypad Chinese large text actions")
        reveal(app.buttons["call-key-0"], in: app)
        app.buttons["call-key-0"].press(forDuration: 0.7)
        XCTAssertEqual(app.textFields["calls-number"].value as? String, "+")
        let middleKeyHeight = app.buttons["call-key-8"].frame.height
        for digit in ["7", "9"] {
            XCTAssertEqual(app.buttons["call-key-\(digit)"].frame.height, middleKeyHeight, accuracy: 1,
                "Four-letter keypad labels must not wrap or change the key height")
        }
        capture(app, "Keypad Chinese dark accessibility size")
    }
}

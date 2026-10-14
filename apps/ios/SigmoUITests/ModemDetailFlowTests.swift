import CoreGraphics
import XCTest

@MainActor
final class ModemDetailFlowTests: XCTestCase {
    private func launch(_ scenario: String = "detail", chinese: Bool = false, large: Bool = false, dark: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = scenario
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = dark ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        let modem = app.descendants(matching: .any)["modem-00000000-0000-0000-0000-000000000001-device-1"].firstMatch
        XCTAssertTrue(modem.waitForExistence(timeout: 10)); modem.tap()
        return app
    }
    private func tap(_ element: XCUIElement, in app: XCUIApplication) {
        _ = element.waitForExistence(timeout: 2)
        for _ in 0..<12 {
            if element.exists && element.isHittable {
                element.tap()
                return
            }
            app.swipeUp()
        }
        XCTFail("Cannot reach element: \(element)\n\(app.debugDescription)")
    }
    private func tapOverviewRow(_ element: XCUIElement, in app: XCUIApplication) {
        let bar = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
        guard bar.waitForExistence(timeout: 5) else {
            XCTFail("Missing eSIM network bar")
            return
        }
        for _ in 0..<12 {
            let frame = app.frame
            let top = bar.frame.maxY + 8
            let bottom = app.tabBars.firstMatch.frame.minY - 8
            let viewport = CGRect(x: frame.minX, y: top, width: frame.width, height: bottom - top)
            guard viewport.height >= 44 else {
                XCTFail("No tappable list area below the network bar")
                return
            }
            if element.exists {
                let visible = element.frame.intersection(viewport)
                if element.isHittable, !visible.isNull, visible.height >= 44 {
                    let enabled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: element)
                    XCTAssertEqual(XCTWaiter.wait(for: [enabled], timeout: 5), .completed)
                    app.coordinate(withNormalizedOffset: CGVector(
                        dx: (visible.midX - frame.minX) / frame.width,
                        dy: (visible.midY - frame.minY) / frame.height
                    )).tap()
                    return
                }
            }
            // Keep gestures inside the list; dragging on the fixed bar cannot scroll it.
            let down = element.exists && element.frame.midY < top
            let startY = top + viewport.height * (down ? 0.25 : 0.75)
            let endY = top + viewport.height * (down ? 0.75 : 0.25)
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (startY - frame.minY) / frame.height))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (endY - frame.minY) / frame.height))
            start.press(forDuration: 0.1, thenDragTo: end)
        }
        XCTFail("Cannot reach overview row: \(element)\n\(app.debugDescription)")
    }
    private func screenshot(_ app: XCUIApplication, name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name; attachment.lifetime = .keepAlways; add(attachment)
        let file = URL.temporaryDirectory.appending(path: "Detail-" + name.replacingOccurrences(of: " ", with: "-") + ".png")
        do { try app.screenshot().pngRepresentation.write(to: file); print("DETAIL_SCREENSHOT " + file.path) }
        catch { XCTFail("Save screenshot: \(error)") }
    }
    private func openActions(_ app: XCUIApplication, profile: String = "se-a:8986000000000000002") {
        for id in ["saveDetail", "saveReminder"] where app.buttons[id].exists {
            XCTAssertTrue(app.buttons[id].waitForNonExistence(timeout: 5))
        }
        if !app.buttons["modemInfo"].isHittable { app.navigationBars.buttons.element(boundBy: 0).tap() }
        tapOverviewRow(app.buttons["profileActions-" + profile], in: app)
        let opened = app.switches["profileEnabled-" + profile].waitForExistence(timeout: 5)
        if !opened { screenshot(app, name: "Profile navigation failure") }
        XCTAssertTrue(opened, app.debugDescription)
    }
    private func expectToggle(_ element: XCUIElement, value: String) {
        let changed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", value), object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [changed], timeout: 5), .completed)
    }
    private func replace(_ field: XCUIElement, value: String) {
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        let current = field.value as? String ?? ""
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: current.count) + value)
    }
    private func installForm(_ app: XCUIApplication, manual: Bool = true) {
        tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
        tap(app.alerts.buttons["skipESIMDiscoveries"].firstMatch, in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForNonExistence(timeout: 5))
        if manual {
            tap(app.buttons["scanESIM"], in: app)
            tap(app.buttons["manualESIM"], in: app)
        }
    }

    private func notificationRow(_ app: XCUIApplication, id: String = "se-a:18446744073709551615") -> XCUIElement {
        app.descendants(matching: .any)["esimNotification-" + id].firstMatch
    }

    func testContactsOnOlderBackendShowsUpgradeAndKeepsDeviceNavigation() {
        let app = launch()
        let contacts = app.tabBars.buttons["Contacts"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 5)); contacts.tap()
        XCTAssertTrue(app.staticTexts["Update this Sigmo backend to use this feature."].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["modem-contact-actions"].exists)
        app.tabBars.buttons["SIM"].tap()
        XCTAssertTrue(app.buttons["modemInfo"].waitForExistence(timeout: 5))
        app.buttons["backToDevices"].tap()
        XCTAssertTrue(app.navigationBars["Sigmo"].waitForExistence(timeout: 5))
    }

    func testProfileRowsShowNumbersAndLocalizedPlaceholder() {
        for chinese in [false, true] {
            let app = launch(chinese: chinese, large: chinese)
            let active = app.buttons["profileActions-se-a:8986000000000000001"]
            let inactive = app.buttons["profileActions-se-a:8986000000000000002"]
            XCTAssertTrue(active.waitForExistence(timeout: 5))
            XCTAssertTrue(active.label.contains("Primary"), active.label)
            XCTAssertTrue(active.label.contains("+86 138 0000 0000"), active.label)
            XCTAssertFalse(active.label.contains("8986000000000000001"), active.label)
            // Large Dynamic Type can leave the second lazy list row offscreen.
            for _ in 0..<8 where !inactive.exists {
                let top = app.descendants(matching: .any)["esimNetworkBar"].firstMatch.frame.maxY + 8
                let bottom = app.tabBars.firstMatch.frame.minY - 8
                let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (top + (bottom - top) * 0.75 - app.frame.minY) / app.frame.height))
                let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (top + (bottom - top) * 0.25 - app.frame.minY) / app.frame.height))
                start.press(forDuration: 0.1, thenDragTo: end)
            }
            XCTAssertTrue(inactive.waitForExistence(timeout: 5), "Inactive profile missing, Chinese: \(chinese)")
            XCTAssertTrue(inactive.label.contains(chinese ? "无号码" : "No Number"), inactive.label)
            XCTAssertFalse(inactive.label.contains("8986000000000000002"), inactive.label)
            app.terminate()
        }
    }

    func testOverviewInfoAndNotifications() {
        let app = launch()
        XCTAssertTrue(app.buttons["modemInfo"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["addESIM-se-a"].exists)
        for title in ["Messages", "Calls", "Contacts"] {
            app.tabBars.buttons[title].tap()
            XCTAssertFalse(app.buttons["addESIM-se-a"].exists)
        }
        app.tabBars.buttons["SIM"].tap()
        XCTAssertFalse(app.staticTexts["Quectel"].exists)
        XCTAssertFalse(app.staticTexts["Device information"].exists)
        XCTAssertFalse(app.staticTexts["device-1"].exists)
        XCTAssertFalse(app.tabBars.buttons["Settings"].exists)
        XCTAssertLessThan(app.buttons["modemSettings"].frame.midX, app.buttons["modemInfo"].frame.midX)
        app.buttons["modemSettings"].tap()
        XCTAssertTrue(app.buttons["modemDeviceSettings"].waitForExistence(timeout: 5))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        screenshot(app, name: "Detail overview")
        app.buttons["modemInfo"].tap()
        XCTAssertTrue(app.staticTexts["Quectel"].waitForExistence(timeout: 5))
        screenshot(app, name: "Module information")
        tap(app.buttons["esimNotifications"], in: app)
        let row = notificationRow(app)
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        for value in ["#18446744073709551615", "Install", "8986000000000000001", "smdp.example"] {
            XCTAssertTrue(row.label.contains(value), row.label)
        }
        let resend = app.buttons["resendNotification-se-a:18446744073709551615"]
        let delete = app.buttons["deleteNotification-se-a:18446744073709551615"]
        XCTAssertFalse(resend.exists)
        XCTAssertFalse(delete.exists)
        screenshot(app, name: "eSIM notification rows")
        row.swipeLeft()
        XCTAssertTrue(resend.waitForExistence(timeout: 5))
        XCTAssertTrue(delete.exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertEqual(resend.label, "Resend")
        XCTAssertEqual(delete.label, "Delete")
        screenshot(app, name: "eSIM notification swipe actions")
        resend.tap()
        XCTAssertTrue(row.exists)
        row.swipeLeft()
        tap(delete, in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        app.alerts.buttons["Cancel"].tap()
        XCTAssertTrue(row.exists)
        row.swipeLeft()
        tap(delete, in: app)
        tap(app.alerts.buttons["confirmNotificationDeletion"].firstMatch, in: app)
        XCTAssertTrue(row.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["No eSIM notifications"].waitForExistence(timeout: 5))
    }

    func testNotificationOperationsAndScopedSwipeDeletion() throws {
        let app = launch("detail-notifications")
        app.buttons["modemInfo"].tap()
        tap(app.buttons["esimNotifications"], in: app)
        for (id, operation) in [
            ("se-a:18446744073709551615", "Install"),
            ("se-a:2", "Enable"),
            ("se-a:3", "Disable"),
            ("se-a:4", "Delete"),
            ("se-b:18446744073709551615", "Install")
        ] {
            let row = notificationRow(app, id: id)
            XCTAssertTrue(row.waitForExistence(timeout: 5))
            XCTAssertTrue(row.label.contains(operation), row.label)
        }
        screenshot(app, name: "eSIM notification operations")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            print("NOTIFICATION_AUDIT \(issue.compactDescription): \(issue.detailedDescription)")
            print(issue.element?.debugDescription ?? "No element identified")
            return false
        }
        let row = notificationRow(app)
        row.swipeLeft()
        tap(app.buttons["deleteNotification-se-a:18446744073709551615"], in: app)
        tap(app.alerts.buttons["confirmNotificationDeletion"].firstMatch, in: app)
        XCTAssertTrue(row.waitForNonExistence(timeout: 5))
        XCTAssertTrue(notificationRow(app, id: "se-b:18446744073709551615").exists)
        XCTAssertTrue(notificationRow(app, id: "se-a:2").exists)
    }

    func testSingleHardwareUsesESIMLabels() {
        let app = launch("detail-single")
        XCTAssertTrue(app.buttons["profileActions-se-a:8986000000000000001"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["simSlotPicker"].exists)
        XCTAssertFalse(app.staticTexts["SIM 1"].exists)
        XCTAssertFalse(app.staticTexts["SE 1"].exists)
        XCTAssertTrue(app.staticTexts["eSIMs"].firstMatch.exists)
        XCTAssertEqual(app.buttons["seEID-se-a"].value as? String, "89049032000000000000000000000001")
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64KiB")
        XCTAssertFalse(app.buttons["seEID-se-b"].exists)
        screenshot(app, name: "Single SIM and eSIM overview")
        app.buttons["modemInfo"].tap()
        XCTAssertTrue(app.staticTexts["eSIM"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["SE 1"].exists)
        XCTAssertFalse(app.staticTexts["Active slot"].exists)
        tap(app.buttons["esimNotifications"], in: app)
        XCTAssertTrue(notificationRow(app).waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["eSIM"].firstMatch.exists)
        XCTAssertFalse(app.staticTexts["SE 1"].exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.navigationBars.buttons.element(boundBy: 0).tap()
        installForm(app, manual: false)
        XCTAssertTrue(app.buttons["scanESIM"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["targetEUICC-se-a"].exists)
        XCTAssertFalse(app.buttons["manualESIM"].exists)
        XCTAssertFalse(app.buttons["discoverESIM"].exists)
    }

    func testNotificationsUseChineseLargeText() throws {
        let app = launch("detail-single", chinese: true, large: true, dark: true)
        app.buttons["modemInfo"].tap()
        tap(app.buttons["esimNotifications"], in: app)
        let row = notificationRow(app)
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        XCTAssertTrue(row.label.contains("安装"), row.label)
        XCTAssertTrue(row.label.contains("#18446744073709551615"), row.label)
        XCTAssertFalse(app.staticTexts["SE 1"].exists)
        screenshot(app, name: "Chinese large text eSIM notifications")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped])
        row.swipeLeft()
        let resend = app.buttons["resendNotification-se-a:18446744073709551615"]
        let delete = app.buttons["deleteNotification-se-a:18446744073709551615"]
        XCTAssertTrue(resend.waitForExistence(timeout: 5))
        XCTAssertEqual(resend.label, "重新发送")
        XCTAssertEqual(delete.label, "删除")
        delete.tap()
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        app.alerts.buttons["取消"].tap()
        XCTAssertTrue(row.exists)
    }

    func testLoadingPlaceholdersBecomeProfiles() {
        let app = launch("detail-loading")
        let skeleton = app.descendants(matching: .any)["profileLoading"].firstMatch
        XCTAssertTrue(skeleton.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["seEID-se-a"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64KiB")
        XCTAssertTrue(skeleton.exists)
        XCTAssertFalse(app.buttons["profileActions-se-a:8986000000000000001"].exists)
        XCTAssertFalse(app.buttons["addESIM-se-a"].exists)
        XCTAssertFalse(app.buttons["addESIM-se-b"].exists)
        screenshot(app, name: "Detail loading placeholders")
        XCTAssertTrue(app.buttons["profileActions-se-a:8986000000000000001"].waitForExistence(timeout: 12))
        XCTAssertTrue(skeleton.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["addESIM-se-a"].exists)
    }

    func testProfileLoadErrorUsesDismissOnlyAlert() {
        let app = launch("detail-unavailable")
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertTrue(alert.staticTexts["The server could not complete the request."].exists)
        XCTAssertEqual(alert.buttons.count, 1)
        XCTAssertFalse(alert.buttons["Retry"].exists)
        screenshot(app, name: "Detail load error alert")
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["The server could not complete the request."].exists)
        XCTAssertFalse(app.descendants(matching: .any)["profileLoading"].firstMatch.exists)
        XCTAssertFalse(app.buttons["Retry"].exists)
        XCTAssertTrue(app.buttons["seEID-se-a"].exists)
        XCTAssertTrue(app.buttons["seEID-se-b"].exists)
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64KiB")
    }

    func testSignalBarsRemainVisibleInGlass() throws {
        let app = launch()
        let signal = app.descendants(matching: .any)["signal-device-1"].firstMatch
        XCTAssertTrue(signal.waitForExistence(timeout: 5))
        XCTAssertEqual(signal.value as? String, "78%")
        let image = try XCTUnwrap(signal.screenshot().image.cgImage)
        var pixels = [UInt8](repeating: 0, count: image.width * image.height * 4)
        let darkPixels = try pixels.withUnsafeMutableBytes { buffer in
            let context = try XCTUnwrap(CGContext(
                data: buffer.baseAddress, width: image.width, height: image.height,
                bitsPerComponent: 8, bytesPerRow: image.width * 4,
                space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGBitmapInfo.byteOrder32Big.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue
            ))
            context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
            var count = 0
            for offset in stride(from: 0, to: buffer.count, by: 4) {
                if buffer[offset] < 100 && buffer[offset + 1] < 100 && buffer[offset + 2] < 100 && buffer[offset + 3] > 200 {
                    count += 1
                }
            }
            return count
        }
        screenshot(app, name: "Signal and SE heading contrast")
        XCTAssertGreaterThan(darkPixels, image.width * image.height / 10, "78% signal must render filled bars, not an all-gray icon")
    }

    func testSecureElementMetadataAndCopy() {
        let app = launch()
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
        screenshot(app, name: "SE metadata below headings")
        for (id, eid, storage) in [
            ("se-a", "89049032000000000000000000000001", "64KiB"),
            ("se-b", "89049032000000000000000000000002", "32KiB")
        ] {
            let identifier = app.buttons["seEID-" + id]
            XCTAssertTrue(identifier.waitForExistence(timeout: 5))
            XCTAssertEqual(identifier.value as? String, eid)
            XCTAssertEqual(app.staticTexts["seStorage-" + id].value as? String, storage)
            XCTAssertLessThan(identifier.frame.maxY, app.buttons["addESIM-" + id].frame.minY)
            identifier.press(forDuration: 1)
            screenshot(app, name: "EID copy menu " + id)
            let copy = app.buttons["Copy"]
            XCTAssertTrue(copy.waitForExistence(timeout: 5), app.debugDescription)
            copy.tap()
            openActions(app)
            app.buttons["Label"].tap()
            let input = app.textFields["detailInput"]
            replace(input, value: "")
            input.press(forDuration: 1)
            let paste = app.descendants(matching: .any)["Paste"].firstMatch
            XCTAssertTrue(paste.waitForExistence(timeout: 5))
            paste.tap()
            XCTAssertTrue(input.value as? String == eid, "Pasted EID does not match \(id)")
            app.buttons["Cancel"].tap()
            XCTAssertTrue(input.waitForNonExistence(timeout: 5))
            app.navigationBars.buttons.element(boundBy: 0).tap()
        }
        screenshot(app, name: "SE metadata below headings")
    }

    func testNetworkBarStaysFixedWhileScrolling() {
        for chinese in [false, true] {
            let app = launch(chinese: chinese, large: true, dark: chinese)
            let eid = app.buttons["seEID-se-a"]
            XCTAssertTrue(eid.waitForExistence(timeout: 5))
            let bar = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
            XCTAssertTrue(bar.exists)
            let original = bar.frame
            let eidY = eid.frame.minY
            screenshot(app, name: "Large text fixed network bar \(chinese)")
            app.swipeUp()
            XCTAssertEqual(bar.frame.minY, original.minY, accuracy: 1)
            XCTAssertEqual(bar.frame.height, original.height, accuracy: 1)
            XCTAssertTrue(!eid.isHittable || eid.frame.minY < eidY)
            // The glass bar's menu wrapper can report false for isHittable; verify the actual interaction.
            let slotPicker = app.buttons["simSlotPicker"]
            slotPicker.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
            XCTAssertTrue(app.buttons["simSlot-2"].waitForExistence(timeout: 5), app.debugDescription)
            app.buttons["simSlot-2"].tap()
            app.alerts.buttons[chinese ? "取消" : "Cancel"].tap()
            screenshot(app, name: "Fixed network bar after scrolling \(chinese)")
            openActions(app, profile: "se-a:8986000000000000001")
            app.navigationBars.buttons.element(boundBy: 0).tap()
            tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
            XCTAssertTrue(app.alerts.buttons["skipESIMDiscoveries"].firstMatch.waitForExistence(timeout: 5))
            app.terminate()
        }
    }

    func testIncorrectPINAlertPreservesEditor() {
        let app = launch("detail-locked")
        let pin = app.secureTextFields["detailInput"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        pin.typeText("0000")
        tap(app.buttons["saveDetail"], in: app)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertTrue(alert.staticTexts["Incorrect PIN"].exists)
        screenshot(app, name: "PIN error alert")
        alert.buttons["OK"].tap()
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        XCTAssertTrue(pin.exists)
        XCTAssertTrue(app.buttons["saveDetail"].isEnabled)
        pin.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 4) + "1234")
        tap(app.buttons["saveDetail"], in: app)
        XCTAssertTrue(pin.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
    }

    func testAddESIMRowsAndCopy() {
        let app = launch("detail-discovery-loading")
        let install = app.buttons["addESIM-se-a"]
        XCTAssertTrue(install.waitForExistence(timeout: 5))
        screenshot(app, name: "Compact eSIM overview")
        XCTAssertEqual(install.label, "Add eSIM")
        XCTAssertGreaterThanOrEqual(install.frame.height, 44)
        XCTAssertFalse(app.buttons["installESIM"].exists)
        tapOverviewRow(app.buttons["addESIM-se-b"], in: app)
        XCTAssertTrue(app.activityIndicators.firstMatch.waitForExistence(timeout: 2))
        XCTAssertFalse(app.buttons["addESIM-se-a"].isEnabled)
        XCTAssertFalse(app.buttons["addESIM-se-b"].isEnabled)
        XCTAssertFalse(app.buttons["closeESIMFlow"].exists)
        screenshot(app, name: "Add eSIM loading on row")
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["closeESIMFlow"].exists)
        tap(app.alerts.buttons["skipESIMDiscoveries"].firstMatch, in: app)
        XCTAssertTrue(app.staticTexts["Set Up eSIM"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["targetEUICC-se-a"].exists)
        XCTAssertFalse(app.buttons["backESIMSetup"].exists)
        screenshot(app, name: "Add eSIM methods")
        app.buttons["closeESIMFlow"].tap()
        app.buttons["modemInfo"].tap()
        let identifier = app.staticTexts["device-1"].firstMatch
        XCTAssertTrue(identifier.waitForExistence(timeout: 5))
        identifier.press(forDuration: 1)
        XCTAssertTrue(app.buttons["Copy"].waitForExistence(timeout: 5))
        screenshot(app, name: "Information copy menu")
        app.buttons["Copy"].tap()
    }

    func testLeavingDetailCancelsPendingESIMSetup() {
        let app = launch("detail-discovery-loading")
        tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
        XCTAssertTrue(app.activityIndicators.firstMatch.waitForExistence(timeout: 2))
        app.tabBars.buttons["Messages"].tap()
        XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 6))
        XCTAssertFalse(app.buttons["closeESIMFlow"].exists)
        app.tabBars.buttons["SIM"].tap()
        let install = app.buttons["addESIM-se-a"]
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: install)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
        tapOverviewRow(install, in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 10))
        tap(app.alerts.buttons["skipESIMDiscoveries"].firstMatch, in: app)
        XCTAssertTrue(app.buttons["scanESIM"].waitForExistence(timeout: 5))
    }

    func testRenameReminderAndSIMApplication() {
        let app = launch()
        openActions(app)
        app.buttons["Label"].tap()
        replace(app.textFields["detailInput"], value: "Travel renamed")
        tap(app.buttons["saveDetail"], in: app)
        XCTAssertTrue(app.staticTexts["Travel renamed"].waitForExistence(timeout: 5))
        openActions(app)
        app.buttons["Reminder"].tap()
        let content = app.textFields["reminderContent"]
        XCTAssertTrue(content.waitForExistence(timeout: 5))
        content.tap(); content.typeText("Renew plan")
        tap(app.buttons["saveReminder"], in: app)
        XCTAssertTrue(app.staticTexts["Renew plan"].waitForExistence(timeout: 5))
        for _ in 0..<5 { app.swipeDown() }
        openActions(app, profile: "se-a:8986000000000000001")
        app.buttons["SIM Applications"].tap()
        tap(app.buttons["Balance"], in: app)
        XCTAssertTrue(app.staticTexts["Balance: 10.00"].waitForExistence(timeout: 5), app.debugDescription)
        screenshot(app, name: "SIM application")
        app.buttons["Confirm"].tap()
        tap(app.buttons["Done"], in: app)
    }

    func testDownloadWithPreviewAndConfirmation() {
        let app = launch()
        installForm(app)
        XCTAssertFalse(app.buttons["Refresh"].exists)
        XCTAssertTrue(app.buttons["Next"].exists)
        XCTAssertFalse(app.buttons["Other Options"].exists)
        XCTAssertTrue(app.buttons["backESIMSetup"].exists)
        XCTAssertFalse(app.buttons["closeESIMFlow"].exists)
        XCTAssertEqual(app.buttons["startESIMDownload"].label, "Next")
        let smdp = app.textFields["smdpInput"]
        XCTAssertTrue(smdp.waitForExistence(timeout: 5))
        smdp.typeText("LPA:1$smdp.example$code")
        screenshot(app, name: "Enter eSIM details")
        tap(app.buttons["startESIMDownload"], in: app)
        XCTAssertTrue(app.staticTexts["downloadProfileName"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.staticTexts["downloadProfileName"].label, "Downloaded")
        XCTAssertEqual(app.staticTexts["downloadProfileICCID"].label, "8986000000000000003")
        XCTAssertEqual(app.staticTexts["downloadProfileProvider"].label, "Example Mobile")
        XCTAssertFalse(app.staticTexts["Service provider"].exists)
        XCTAssertFalse(app.staticTexts["Profile State"].exists)
        XCTAssertEqual(app.buttons["confirmESIMPreview"].label, "Confirm Download")
        XCTAssertEqual(app.buttons["copyDownloadICCID"].label, "Copy ICCID")
        XCTAssertGreaterThanOrEqual(app.staticTexts["Ready to Add eSIM"].frame.minY, app.buttons["closeESIMFlow"].frame.maxY)
        screenshot(app, name: "Add eSIM preview")
        tap(app.buttons["confirmESIMPreview"], in: app)
        let code = app.secureTextFields["downloadConfirmation"]
        XCTAssertTrue(code.waitForExistence(timeout: 5)); code.tap(); code.typeText("1234")
        tap(app.buttons["Confirm"], in: app)
        XCTAssertTrue(app.staticTexts["eSIM Added"].waitForExistence(timeout: 10))
        XCTAssertEqual(app.staticTexts["downloadProfileName"].label, "Downloaded")
        XCTAssertEqual(app.staticTexts["downloadProfileICCID"].label, "8986000000000000003")
        XCTAssertEqual(app.staticTexts["downloadProfileProvider"].label, "Example Mobile")
        XCTAssertFalse(app.staticTexts["Service provider"].exists)
        XCTAssertFalse(app.staticTexts["Profile State"].exists)
        screenshot(app, name: "Download completed")
        app.buttons["closeCompletedESIM"].tap()
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
    }

    func testDownloadSummaryInChineseWithLargeTextAndDarkMode() {
        let app = launch(chinese: true, large: true, dark: true)
        tapOverviewRow(app.buttons["addESIM-se-b"], in: app)
        tap(app.alerts.buttons["showESIMDiscoveries"].firstMatch, in: app)
        tap(app.buttons["downloadDiscoveredESIM"], in: app)
        XCTAssertTrue(app.buttons["confirmESIMPreview"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.buttons["confirmESIMPreview"].label, "确认下载")
        XCTAssertGreaterThanOrEqual(app.staticTexts["准备添加 eSIM"].frame.minY, app.buttons["closeESIMFlow"].frame.maxY)
        tap(app.buttons["copyDownloadICCID"], in: app)
        let copyButton = app.buttons["copyDownloadICCID"]
        XCTAssertEqual(copyButton.label, "复制 ICCID")
        XCTAssertGreaterThanOrEqual(copyButton.frame.width, 44)
        XCTAssertGreaterThanOrEqual(copyButton.frame.height, 44)
        let iccid = app.staticTexts["downloadProfileICCID"]
        XCTAssertEqual(iccid.label, "8986000000000000003")
        XCTAssertGreaterThanOrEqual(iccid.frame.minX, app.frame.minX)
        XCTAssertLessThanOrEqual(iccid.frame.maxX, app.frame.maxX)
        XCTAssertFalse(app.staticTexts["配置状态"].exists)
        screenshot(app, name: "Download summary Chinese large dark")
        tap(app.buttons["downloadEIDDetails"], in: app)
        let eid = app.staticTexts.matching(NSPredicate(format: "label == %@", "89049032000000000000000000000002")).firstMatch
        // Expanded rows can start below the viewport at accessibility text sizes.
        tap(eid, in: app)
        XCTAssertTrue(eid.isHittable, app.debugDescription)
        tap(app.buttons["downloadProfileDetails"], in: app)
        for name in ["GID1", "GID2"] {
            let field = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
            tap(field, in: app)
            XCTAssertTrue(field.isHittable, app.debugDescription)
        }
        tap(app.buttons["confirmESIMPreview"], in: app)
        let code = app.secureTextFields["downloadConfirmation"]
        XCTAssertTrue(code.waitForExistence(timeout: 5))
        code.tap(); code.typeText("1234")
        tap(app.buttons["confirmESIMCode"], in: app)
        XCTAssertTrue(app.buttons["closeESIMFlow"].waitForExistence(timeout: 10))
        tap(app.buttons["copyDownloadICCID"], in: app)
        XCTAssertEqual(app.staticTexts["downloadProfileICCID"].label, "8986000000000000003")
        XCTAssertFalse(app.staticTexts["配置状态"].exists)
        screenshot(app, name: "Downloaded summary Chinese large dark")
        tap(app.buttons["closeESIMFlow"], in: app)
        XCTAssertTrue(app.buttons["closeESIMFlow"].waitForNonExistence(timeout: 5), app.debugDescription)
        // The refreshed profile list can move SE 2 below the viewport at this text size.
        XCTAssertTrue(app.buttons["modemInfo"].waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(app.buttons["modemInfo"].isHittable, app.debugDescription)
    }

    func testEveryTransferSourceRespondsAcrossItsRow() {
        let app = launch()
        installForm(app, manual: false)
        tap(app.buttons["transferESIM"], in: app)
        for (key, name) in [("modem:device-2", "Source modem"), ("ccid:reader-1", "USB reader")] {
            let source = app.buttons["transferSource-" + key]
            XCTAssertTrue(source.waitForExistence(timeout: 5))
            XCTAssertFalse(app.buttons["Refresh"].exists)
            // Tap the empty part of the row, not its text or chevron.
            source.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)).tap()
            XCTAssertTrue(app.buttons["startESIMTransfer"].waitForExistence(timeout: 3))
            XCTAssertTrue(app.staticTexts[name].exists)
            XCTAssertFalse(app.buttons["Refresh"].exists)
            tap(app.buttons["backESIMSetup"], in: app)
        }
    }

    func testClosingESIMSetupDoesNotWaitForDeviceReload() {
        let app = launch("detail-loading")
        installForm(app, manual: false)
        let close = app.buttons["closeESIMFlow"]
        XCTAssertTrue(close.waitForExistence(timeout: 5))
        close.tap()
        // The fixture takes three seconds per device read; dismissal must not wait for it.
        XCTAssertTrue(close.waitForNonExistence(timeout: 2))
        XCTAssertFalse(app.buttons["scanESIM"].exists)
    }

    func testTransferShowsOnlySupportedProfiles() {
        let app = launch("detail-transfer-mixed")
        installForm(app, manual: false)
        tap(app.buttons["transferESIM"], in: app)
        tap(app.buttons["transferSource-modem:device-2"], in: app)
        let profile = app.buttons.containing(.staticText, identifier: "Inactive line").firstMatch
        XCTAssertTrue(profile.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Unsupported line"].exists)
        XCTAssertTrue(profile.isEnabled)
        XCTAssertFalse(app.buttons["startESIMTransfer"].isEnabled)
        tap(profile, in: app)
        XCTAssertTrue(app.buttons["startESIMTransfer"].isEnabled)
        tap(app.buttons["backESIMSetup"], in: app)
        tap(app.buttons["transferSource-modem:device-2"], in: app)
        XCTAssertTrue(profile.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["startESIMTransfer"].isEnabled)
    }

    func testUnsupportedTransferProfilesShowEmptyState() {
        let app = launch("detail-transfer-unsupported")
        installForm(app, manual: false)
        tap(app.buttons["transferESIM"], in: app)
        tap(app.buttons["transferSource-modem:device-2"], in: app)
        XCTAssertTrue(app.staticTexts["No eSIMs available to transfer"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Unsupported line"].exists)
        XCTAssertFalse(app.buttons["startESIMTransfer"].isEnabled)
        tap(app.buttons["backESIMSetup"], in: app)
        XCTAssertTrue(app.buttons["transferSource-modem:device-2"].waitForExistence(timeout: 5))
    }

    func testTransferPromptsAndSourceDeletion() {
        let app = launch()
        installForm(app, manual: false)
        tap(app.buttons["transferESIM"], in: app)
        tap(app.buttons.containing(.staticText, identifier: "Source modem").firstMatch, in: app)
        let profile = app.buttons.containing(.staticText, identifier: "Source profile").firstMatch
        tap(profile, in: app)
        tap(app.buttons["startESIMTransfer"], in: app)
        XCTAssertTrue(app.staticTexts["Confirm transfer"].waitForExistence(timeout: 5))
        app.textFields["Verification code"].tap(); app.textFields["Verification code"].typeText("1234")
        tap(app.buttons["Confirm"], in: app)
        XCTAssertTrue(app.staticTexts["Delete the source profile to continue?"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.alerts.firstMatch.exists)
        app.alerts.buttons["Delete"].tap()
        XCTAssertTrue(app.staticTexts["eSIM Added"].waitForExistence(timeout: 10))
        screenshot(app, name: "Transfer completed")
    }

    func testPhysicalSIMAndPIN() {
        let physical = launch("detail-physical")
        XCTAssertTrue(physical.buttons["modemInfo"].waitForExistence(timeout: 5))
        XCTAssertTrue(physical.tabBars.buttons["SIM"].isSelected)
        XCTAssertFalse(physical.buttons["addESIM-se-a"].exists)
        XCTAssertTrue(physical.descendants(matching: .any)["esimNetworkBar"].exists)
        XCTAssertTrue(physical.buttons["simSlotPicker"].exists)
        XCTAssertFalse(physical.staticTexts["Quectel"].exists)
        XCTAssertFalse(physical.staticTexts["/dev/cdc-wdm0"].exists)
        XCTAssertTrue(physical.buttons["Phone number"].exists)
        XCTAssertFalse(physical.staticTexts["8986000000000000001"].exists)
        XCTAssertFalse(physical.buttons["Label"].exists)
        XCTAssertFalse(physical.switches["Turn On This Line"].exists)
        XCTAssertFalse(physical.buttons["profileManagementInfo"].exists)
        for entry in ["profileNetworkSelection", "profileInternet", "profileWiFiCalling", "profileVoLTE"] {
            XCTAssertTrue(physical.buttons[entry].exists)
        }
        screenshot(physical, name: "Physical SIM overview")
        physical.buttons["modemInfo"].tap()
        XCTAssertTrue(physical.staticTexts["Quectel"].waitForExistence(timeout: 5))
        XCTAssertTrue(physical.staticTexts["/dev/cdc-wdm0"].exists)
        XCTAssertFalse(physical.buttons["esimNotifications"].exists)
        screenshot(physical, name: "Physical modem information")
        tap(physical.staticTexts["8986000000000000001"], in: physical)
        physical.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(physical.buttons["modemInfo"].waitForExistence(timeout: 5))
        XCTAssertFalse(physical.staticTexts["Quectel"].exists)
        tap(physical.buttons["SIM Applications"], in: physical)
        tap(physical.buttons["Balance"], in: physical)
        XCTAssertTrue(physical.staticTexts["Balance: 10.00"].waitForExistence(timeout: 5))
        physical.buttons["Confirm"].tap()
        tap(physical.buttons["Done"], in: physical)
        tap(physical.buttons["Reminder"], in: physical)
        XCTAssertTrue(physical.buttons["saveReminder"].exists)
        physical.terminate()
        let locked = launch("detail-locked")
        let pin = locked.secureTextFields["detailInput"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5)); pin.tap(); pin.typeText("1234")
        tap(locked.buttons["saveDetail"], in: locked)
        XCTAssertTrue(locked.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
    }

    func testPhysicalSIMConnectionNavigationAndNumberEditing() {
        let app = launch("detail-physical")
        for (entry, control) in [("profileNetworkSelection", "networkAutomatic"), ("profileInternet", "internetConnected"),
                                 ("profileWiFiCalling", "wifiCallingEnabled"), ("profileVoLTE", "volteEnabled")] {
            tapOverviewRow(app.buttons[entry], in: app)
            XCTAssertTrue(app.switches[control].waitForExistence(timeout: 10))
            XCTAssertTrue(app.switches[control].isEnabled)
            app.navigationBars.buttons.element(boundBy: 0).tap()
        }
        tapOverviewRow(app.buttons["Phone number"], in: app)
        let number = app.textFields["detailInput"]
        replace(number, value: "+8613900000000")
        tap(app.buttons["saveDetail"], in: app)
        XCTAssertTrue(app.buttons["saveDetail"].waitForNonExistence(timeout: 5))
        tapOverviewRow(app.buttons["Phone number"], in: app)
        XCTAssertTrue(number.waitForExistence(timeout: 5))
        XCTAssertTrue((number.value as? String)?.contains("139") == true)
        app.buttons["Cancel"].tap()
        app.buttons["simSlotPicker"].tap()
        tap(app.buttons["simSlot-2"], in: app)
        tap(app.alerts.buttons["confirmDetailAction"].firstMatch, in: app)
        let switched = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", "SIM 2"), object: app.buttons["simSlotPicker"])
        XCTAssertEqual(XCTWaiter.wait(for: [switched], timeout: 10), .completed)
        XCTAssertTrue(app.buttons["profileInternet"].exists)
    }

    func testChineseLargeTextAndDarkInfo() throws {
        let app = launch(chinese: true, large: true, dark: true)
        XCTAssertTrue(app.buttons["modemInfo"].waitForExistence(timeout: 5))
        screenshot(app, name: "Chinese large text overview")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription])
        app.buttons["modemInfo"].tap()
        screenshot(app, name: "Chinese large text information")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped])
        app.navigationBars.buttons.element(boundBy: 0).tap()
        tapOverviewRow(app.buttons["profileActions-se-a:8986000000000000001"], in: app)
        screenshot(app, name: "Chinese large text profile actions")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped])
        app.navigationBars.buttons.element(boundBy: 0).tap()
        tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
        tap(app.alerts.buttons["skipESIMDiscoveries"].firstMatch, in: app)
        XCTAssertFalse(app.staticTexts["选择 SE"].exists)
        XCTAssertTrue(app.staticTexts["设置 eSIM"].waitForExistence(timeout: 5))
        screenshot(app, name: "Chinese large text Add eSIM")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            print("ESIM_SETUP_AUDIT \(issue.compactDescription): \(issue.detailedDescription)")
            print(issue.element?.debugDescription ?? "No element identified")
            return false
        }
        tap(app.buttons["scanESIM"], in: app)
        XCTAssertTrue(app.staticTexts["扫描二维码"].waitForExistence(timeout: 5))
        screenshot(app, name: "Chinese large text scanner")
        try app.performAccessibilityAudit(for: [.sufficientElementDescription, .textClipped]) { issue in
            print("ESIM_SCANNER_AUDIT \(issue.compactDescription): \(issue.detailedDescription)")
            print(issue.element?.debugDescription ?? "No element identified")
            return false
        }
        tap(app.buttons["manualESIM"], in: app)
        let address = app.textFields["smdpInput"]
        XCTAssertTrue(address.waitForExistence(timeout: 5))
        address.typeText("smdp.example")
        XCTAssertTrue(app.buttons["startESIMDownload"].isEnabled)
        screenshot(app, name: "Chinese large text manual entry")
        tap(app.buttons["backESIMSetup"], in: app)
        XCTAssertTrue(app.buttons["manualESIM"].waitForExistence(timeout: 5))
    }

    func testBackgroundDoesNotRestartDownload() {
        let app = launch()
        installForm(app)
        app.textFields["smdpInput"].tap(); app.textFields["smdpInput"].typeText("smdp.example")
        tap(app.buttons["startESIMDownload"], in: app)
        XCTAssertTrue(app.buttons["confirmESIMPreview"].waitForExistence(timeout: 5))
        XCUIDevice.shared.press(.home)
        app.activate()
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        alert.buttons["OK"].tap()
        XCTAssertTrue(app.staticTexts["Operation did not complete"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Refresh"].exists)
        XCTAssertFalse(app.buttons["confirmESIMPreview"].exists)
        screenshot(app, name: "Interrupted download")
        app.buttons["closeESIMFlow"].tap()
        XCTAssertTrue(app.buttons["closeESIMFlow"].waitForNonExistence(timeout: 5))
    }

    func testSlotActivationAndScopedDeletion() {
        let app = launch(dark: true)
        let picker = app.buttons["simSlotPicker"]
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        XCTAssertEqual(picker.value as? String, "SIM 1")
        picker.tap()
        XCTAssertFalse(app.buttons["simSlot-1"].isEnabled)
        tap(app.buttons["simSlot-2"], in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        app.alerts.buttons["Cancel"].tap()
        XCTAssertEqual(picker.value as? String, "SIM 1")
        picker.tap()
        tap(app.buttons["simSlot-2"], in: app)
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)
        let switched = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == 'SIM 2' AND enabled == true"), object: picker)
        XCTAssertEqual(XCTWaiter.wait(for: [switched], timeout: 5), .completed)
        screenshot(app, name: "Carrier and SIM slot menu")
        picker.tap()
        XCTAssertFalse(app.buttons["simSlot-2"].isEnabled)
        tap(app.buttons["simSlot-1"], in: app)
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)
        openActions(app)
        tap(app.switches["profileEnabled-se-a:8986000000000000002"].switches.firstMatch, in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.debugDescription.lowercased().contains("secure element"))
        XCTAssertFalse(app.alerts.firstMatch.debugDescription.contains("SE 1"))
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)
        XCTAssertTrue(app.buttons["profileActions-se-a:8986000000000000002"].waitForExistence(timeout: 2))
        openActions(app)
        XCTAssertTrue(app.buttons["profileInternet"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Delete eSIM"].exists)
        app.buttons["Reminder"].tap()
        XCTAssertTrue(app.textFields["reminderContent"].waitForExistence(timeout: 5))
        app.buttons["Cancel"].tap()
        XCTAssertTrue(app.textFields["reminderContent"].waitForNonExistence(timeout: 5))
        openActions(app, profile: "se-b:8986000000000000002")
        tap(app.buttons["Delete eSIM"], in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.alerts.staticTexts["This cannot be undone."].exists)
        screenshot(app, name: "Delete eSIM confirmation")
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)
        let deleted = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: app.buttons["profileActions-se-b:8986000000000000002"])
        XCTAssertEqual(XCTWaiter.wait(for: [deleted], timeout: 5), .completed, app.debugDescription)
        XCTAssertTrue(app.buttons["profileActions-se-a:8986000000000000002"].exists)
    }

    func testConnectionNavigationAndNumberEditing() {
        let app = launch()
        openActions(app, profile: "se-a:8986000000000000001")
        let internet = app.buttons["profileInternet"]
        XCTAssertTrue(internet.waitForExistence(timeout: 5))
        let labelHeight = app.buttons["Label"].frame.height
        XCTAssertGreaterThanOrEqual(labelHeight, 44)
        XCTAssertEqual(labelHeight, app.buttons["Phone number"].frame.height, accuracy: 1)
        XCTAssertEqual(labelHeight, internet.frame.height, accuracy: 1)
        XCTAssertEqual(labelHeight, app.buttons["Reminder"].frame.height, accuracy: 1)
        XCTAssertEqual(labelHeight, app.buttons["profileManagementInfo"].frame.height, accuracy: 1)
        for entry in ["profileNetworkSelection", "profileInternet", "profileWiFiCalling", "profileVoLTE"] {
            XCTAssertTrue(app.buttons[entry].exists)
        }
        screenshot(app, name: "Profile connection settings")
        tap(app.buttons["Phone number"], in: app)
        let number = app.textFields["detailInput"]
        XCTAssertTrue(number.waitForExistence(timeout: 5))
        screenshot(app, name: "Phone number editor")
        replace(number, value: "+8613900000000")
        tap(app.buttons["saveDetail"], in: app)
        openActions(app, profile: "se-a:8986000000000000001")
        tap(app.buttons["Phone number"], in: app)
        XCTAssertTrue(number.waitForExistence(timeout: 5))
        XCTAssertTrue((number.value as? String)?.contains("139") == true)
    }

    func testActivationReturnsToListAndDisablesProfile() {
        for chinese in [false, true] {
            let app = launch("detail-activation-loading", chinese: chinese)
            openActions(app)
            tap(app.switches["profileEnabled-se-a:8986000000000000002"].switches.firstMatch, in: app)
            tap(app.buttons["confirmDetailAction"].firstMatch, in: app)

            let row = app.buttons["profileActions-se-a:8986000000000000002"]
            XCTAssertTrue(row.waitForExistence(timeout: 2))
            XCTAssertTrue(row.label.contains(chinese ? "正在启用" : "Enabling"), row.label)
            XCTAssertFalse(row.label.contains(chinese ? "关闭" : "Off"), row.label)
            XCTAssertFalse(row.isEnabled)
            XCTAssertFalse(app.buttons["profileManagementInfo"].exists)
            let other = app.buttons["profileActions-se-b:8986000000000000002"]
            XCTAssertTrue(other.isEnabled)
            XCTAssertTrue(other.label.contains(chinese ? "关闭" : "Off"), other.label)

            let enabled = XCTNSPredicateExpectation(
                predicate: NSPredicate(format: "enabled == true AND label CONTAINS %@", chinese ? "开启" : "On"), object: row)
            XCTAssertEqual(XCTWaiter.wait(for: [enabled], timeout: 12), .completed)
            XCTAssertFalse(row.label.contains(chinese ? "正在启用" : "Enabling"), row.label)
            XCTAssertFalse(app.alerts.firstMatch.exists)
            openActions(app)
            XCTAssertTrue(app.buttons["profileInternet"].waitForExistence(timeout: 5))
            XCTAssertFalse(app.buttons[chinese ? "删除 eSIM" : "Delete eSIM"].exists)
            app.terminate()
        }
    }

    func testActivationFailureRestoresProfileActions() {
        let app = launch("detail-activation-error")
        openActions(app)
        tap(app.switches["profileEnabled-se-a:8986000000000000002"].switches.firstMatch, in: app)
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)

        let row = app.buttons["profileActions-se-a:8986000000000000002"]
        XCTAssertTrue(row.waitForExistence(timeout: 2))
        XCTAssertTrue(row.label.contains("Enabling"), row.label)
        XCTAssertFalse(row.isEnabled)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 12))
        XCTAssertTrue(alert.staticTexts["The server could not complete the request."].exists)
        alert.buttons["OK"].tap()

        XCTAssertTrue(row.isEnabled)
        XCTAssertTrue(row.label.contains("Off"), row.label)
        XCTAssertFalse(row.label.contains("Enabling"), row.label)
        openActions(app)
        XCTAssertTrue(app.switches["profileEnabled-se-a:8986000000000000002"].isEnabled)
    }

    func testDeletionReturnsToListAndDisablesProfile() {
        for chinese in [false, true] {
            let app = launch("detail-deletion-loading", chinese: chinese)
            openActions(app)
            tap(app.buttons[chinese ? "删除 eSIM" : "Delete eSIM"], in: app)
            tap(app.buttons["confirmDetailAction"].firstMatch, in: app)

            let row = app.buttons["profileActions-se-a:8986000000000000002"]
            XCTAssertTrue(row.waitForExistence(timeout: 2))
            XCTAssertTrue(row.label.contains(chinese ? "删除中" : "Deleting"), row.label)
            XCTAssertFalse(row.label.contains(chinese ? "关闭" : "Off"), row.label)
            XCTAssertFalse(row.isEnabled)
            XCTAssertFalse(app.buttons["profileManagementInfo"].exists)
            let other = app.buttons["profileActions-se-b:8986000000000000002"]
            XCTAssertTrue(other.isEnabled)
            XCTAssertTrue(other.label.contains(chinese ? "关闭" : "Off"), other.label)

            XCTAssertTrue(row.waitForNonExistence(timeout: 12))
            XCTAssertTrue(other.exists)
            XCTAssertFalse(app.alerts.firstMatch.exists)
            app.terminate()
        }
    }

    func testForegroundRefreshDisablesActionsUntilSnapshotArrives() {
        let app = launch("detail-loading")
        let row = app.buttons["profileActions-se-a:8986000000000000002"]
        XCTAssertTrue(row.waitForExistence(timeout: 12))
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: row)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)

        XCUIDevice.shared.press(.home)
        app.activate()
        let refreshing = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == false"), object: row)
        XCTAssertEqual(XCTWaiter.wait(for: [refreshing], timeout: 2), .completed)
        XCTAssertTrue(row.label.contains("Off"), row.label)

        let refreshed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: row)
        XCTAssertEqual(XCTWaiter.wait(for: [refreshed], timeout: 12), .completed)
        openActions(app)
    }

    func testDeletionFailureRestoresProfileActions() {
        let app = launch("detail-deletion-error")
        openActions(app)
        tap(app.buttons["Delete eSIM"], in: app)
        tap(app.buttons["confirmDetailAction"].firstMatch, in: app)

        let row = app.buttons["profileActions-se-a:8986000000000000002"]
        XCTAssertTrue(row.waitForExistence(timeout: 2))
        XCTAssertTrue(row.label.contains("Deleting"), row.label)
        XCTAssertFalse(row.isEnabled)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 12))
        XCTAssertTrue(alert.staticTexts["The server could not complete the request."].exists)
        alert.buttons["OK"].tap()

        XCTAssertTrue(row.isEnabled)
        XCTAssertTrue(row.label.contains("Off"), row.label)
        XCTAssertFalse(row.label.contains("Deleting"), row.label)
        openActions(app)
        XCTAssertTrue(app.buttons["Delete eSIM"].isEnabled)
    }

    func testScannerFallbackAndManualBackNavigation() {
        let app = launch(dark: true)
        installForm(app, manual: false)
        screenshot(app, name: "Dark Add eSIM methods")
        tap(app.buttons["scanESIM"], in: app)
        XCTAssertTrue(app.staticTexts["Scan QR Code"].waitForExistence(timeout: 5))
        screenshot(app, name: "Scan QR Code and manual fallback")
        tap(app.buttons["manualESIM"], in: app)
        let address = app.textFields["smdpInput"]
        XCTAssertTrue(address.waitForExistence(timeout: 5))
        address.typeText("smdp.example")
        screenshot(app, name: "Dark manual eSIM entry")
        tap(app.buttons["backESIMSetup"], in: app)
        XCTAssertTrue(app.staticTexts["Scan QR Code"].waitForExistence(timeout: 5))
        tap(app.buttons["manualESIM"], in: app)
        XCTAssertEqual(address.value as? String, "smdp.example")
        tap(app.buttons["backESIMSetup"], in: app)
        tap(app.buttons["backESIMSetup"], in: app)
        XCTAssertTrue(app.buttons["scanESIM"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 1))
    }

    func testEmptyAndFailedDiscoveryKeepSetupAvailable() {
        for (scenario, failed) in [
            ("detail-no-discoveries", false),
            ("detail-discovery-error", true)
        ] {
            let app = launch(scenario)
            tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
            let alert = app.alerts.firstMatch
            if failed {
                XCTAssertTrue(alert.waitForExistence(timeout: 5))
                XCTAssertTrue(alert.staticTexts["Operation did not complete"].exists)
                alert.buttons["OK"].tap()
            }
            XCTAssertTrue(app.buttons["scanESIM"].waitForExistence(timeout: 5))
            XCTAssertFalse(alert.exists)
            XCTAssertTrue(app.staticTexts["Set Up eSIM"].exists)
            screenshot(app, name: scenario)
            XCTAssertFalse(app.buttons["downloadDiscoveredESIM"].exists)
            tap(app.buttons["scanESIM"], in: app)
            tap(app.buttons["manualESIM"], in: app)
            let address = app.textFields["smdpInput"]
            address.tap(); address.typeText("smdp.example")
            XCTAssertTrue(app.buttons["startESIMDownload"].isEnabled)
            tap(app.buttons["backESIMSetup"], in: app)
            tap(app.buttons["backESIMSetup"], in: app)
            XCTAssertFalse(app.alerts.firstMatch.waitForExistence(timeout: 1))
            app.terminate()
        }
    }

    func testEmptyProfileListsRetainAddESIM() {
        let app = launch("detail-empty-profiles")
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
        tapOverviewRow(app.buttons["addESIM-se-b"], in: app)
        tap(app.alerts.buttons["skipESIMDiscoveries"].firstMatch, in: app)
        XCTAssertTrue(app.buttons["scanESIM"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["targetEUICC-se-b"].exists)
    }

    func testProfileStateUsesOnAndOff() {
        let app = launch()
        let active = app.buttons["profileActions-se-a:8986000000000000001"]
        XCTAssertTrue(active.waitForExistence(timeout: 5))
        XCTAssertTrue(active.label.contains("On"), active.label)
        let inactive = app.buttons["profileActions-se-a:8986000000000000002"]
        XCTAssertTrue(inactive.label.contains("Off"), inactive.label)
        openActions(app, profile: "se-a:8986000000000000001")
        tap(app.buttons["profileManagementInfo"], in: app)
        XCTAssertTrue(app.staticTexts["Profile State"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["On"].exists)
        XCTAssertFalse(app.staticTexts["Device state"].exists)
        screenshot(app, name: "Profile information state")
    }

    func testDiscoveryDownloadUsesTheOwningSecureElement() {
        let app = launch()
        tapOverviewRow(app.buttons["addESIM-se-b"], in: app)
        tap(app.alerts.buttons["showESIMDiscoveries"].firstMatch, in: app)
        tap(app.buttons["downloadDiscoveredESIM"], in: app)
        tap(app.buttons["confirmESIMPreview"], in: app)
        let code = app.secureTextFields["downloadConfirmation"]
        XCTAssertTrue(code.waitForExistence(timeout: 5))
        code.tap(); code.typeText("1234")
        tap(app.buttons["confirmESIMCode"], in: app)
        XCTAssertTrue(app.staticTexts["eSIM Added"].waitForExistence(timeout: 10))
        app.buttons["closeESIMFlow"].tap()
        let downloaded = app.buttons["profileActions-se-b:8986000000000000003"]
        tapOverviewRow(downloaded, in: app)
        XCTAssertTrue(app.buttons["profileManagementInfo"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["profileActions-se-a:8986000000000000003"].exists)
    }

    func testDiscoveredProfilePreviewCancellation() {
        let app = launch("detail-loading", dark: true)
        tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
        tap(app.alerts.buttons["showESIMDiscoveries"].firstMatch, in: app)
        let download = app.buttons["downloadDiscoveredESIM"]
        XCTAssertTrue(download.waitForExistence(timeout: 5))
        XCTAssertGreaterThanOrEqual(download.frame.height, 44)
        XCTAssertLessThanOrEqual(download.frame.height, 56)
        XCTAssertFalse(app.textFields["smdpInput"].exists)
        screenshot(app, name: "Choose available eSIM")
        tap(download, in: app)
        XCTAssertTrue(app.buttons["confirmESIMPreview"].waitForExistence(timeout: 5))
        XCTAssertLessThanOrEqual(app.buttons["confirmESIMPreview"].frame.height, 56)
        screenshot(app, name: "Discovered profile preview")
        app.buttons["closeESIMFlow"].tap()
        // Delayed reconciliation keeps the sheet visible long enough to catch a transient alert.
        let unexpectedAlert = expectation(for: NSPredicate(format: "exists == true"), evaluatedWith: app.alerts.firstMatch)
        unexpectedAlert.isInverted = true
        wait(for: [unexpectedAlert], timeout: 3)
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.staticTexts["Downloaded"].exists)
    }
}

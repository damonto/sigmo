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
        if scenario.hasPrefix("detail-reminder") { return app }
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
        overviewRowCoordinate(element, in: app)?.tap()
    }
    private func overviewRowCoordinate(_ element: XCUIElement, in app: XCUIApplication) -> XCUICoordinate? {
        let minimumVisibleHeight: CGFloat = 44
        let compactBar = app.descendants(matching: .any)["esimCompactNetworkBar"].firstMatch
        for _ in 0..<12 {
            let frame = app.frame
            let top = (compactBar.exists ? compactBar.frame.maxY : app.navigationBars.firstMatch.frame.maxY) + 8
            let bottom = app.tabBars.firstMatch.frame.minY - 8
            let viewport = CGRect(x: frame.minX, y: top, width: frame.width, height: bottom - top)
            guard viewport.height >= minimumVisibleHeight else {
                XCTFail("No tappable list area below the network bar")
                return nil
            }
            let rowFrame = element.waitForExistence(timeout: 1) ? element.frame : .null
            if !rowFrame.isEmpty {
                let visible = rowFrame.intersection(viewport)
                // Compare whole points to account for fractional accessibility frames.
                if !visible.isNull, visible.height.rounded() >= minimumVisibleHeight {
                    let enabled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: element)
                    XCTAssertEqual(XCTWaiter.wait(for: [enabled], timeout: 5), .completed)
                    return app.coordinate(withNormalizedOffset: CGVector(
                        dx: (visible.midX - frame.minX) / frame.width,
                        dy: (visible.midY - frame.minY) / frame.height
                    ))
                }
            }
            // Keep gestures below the compact bar when it overlays the list.
            // Brake before release so large rows don't fling past the available viewport.
            let down = !rowFrame.isEmpty && rowFrame.midY < viewport.midY
            let distance = viewport.height * 0.25
            let startY = viewport.midY + (down ? -distance : distance)
            let endY = viewport.midY + (down ? distance : -distance)
            // Drag through the gutter so the EID's context menu cannot intercept the gesture.
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: (startY - frame.minY) / frame.height))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: (endY - frame.minY) / frame.height))
            start.press(forDuration: 0.1, thenDragTo: end, withVelocity: .slow, thenHoldForDuration: 0.15)
        }
        XCTFail("Cannot reach overview row: \(element)\n\(app.debugDescription)")
        return nil
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
        XCTAssertTrue(app.buttons["closePeerTransferRequest"].waitForNonExistence(timeout: 5))
        if manual {
            tap(app.buttons["scanESIM"], in: app)
            tap(app.buttons["manualESIM"], in: app)
        }
    }

    private func notificationRow(_ app: XCUIApplication, id: String = "se-a:18446744073709551615") -> XCUIElement {
        app.descendants(matching: .any)["esimNotification-" + id].firstMatch
    }

    func testDeletingReminderProfileKeepsHistoryAssignmentOpen() {
        let app = launch("detail-reminder")
        XCTAssertTrue(app.switches["profileEnabled-se-a:8986000000000000002"].waitForExistence(timeout: 10))
        tap(app.buttons["Delete eSIM"], in: app)
        tap(app.alerts.buttons["confirmDetailAction"].firstMatch, in: app)
        let assignment = app.descendants(matching: .any)["history-assignment-8986000000000000002"].firstMatch
        XCTAssertTrue(assignment.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        app.buttons["historyAssignmentLater"].tap()
        XCTAssertTrue(assignment.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["manageBackends"].waitForExistence(timeout: 5))
    }

    func testDeletingReminderProfileWithoutHistoryReturnsWithoutAssignment() {
        let app = launch("detail-reminder-empty-history")
        XCTAssertTrue(app.switches["profileEnabled-se-a:8986000000000000002"].waitForExistence(timeout: 10))
        tap(app.buttons["Delete eSIM"], in: app)
        tap(app.alerts.buttons["confirmDetailAction"].firstMatch, in: app)

        XCTAssertTrue(app.buttons["manageBackends"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertFalse(app.buttons["historyAssignmentLater"].exists)
    }

    func testContactsKeepsDeviceNavigation() {
        let app = launch()
        let contacts = app.tabBars.buttons["Contacts"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 5)); contacts.tap()
        XCTAssertTrue(app.staticTexts["No contacts yet. Add your first contact."].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["modem-contact-actions"].isEnabled)
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
            // Large Dynamic Type can leave both profiles offscreen.
            _ = overviewRowCoordinate(active, in: app)
            XCTAssertGreaterThanOrEqual(active.frame.height, 68)
            XCTAssertTrue(app.staticTexts[chinese ? "2 个配置" : "2 profiles"].exists)
            XCTAssertTrue(active.label.contains("Primary"), active.label)
            XCTAssertTrue(active.label.contains("+86 138 0000 0000"), active.label)
            XCTAssertFalse(active.label.contains("8986000000000000001"), active.label)
            let list = app.collectionViews.firstMatch
            for _ in 0..<8 where !inactive.exists {
                XCTAssertTrue(list.exists, app.debugDescription)
                let start = list.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.8))
                let end = list.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.55))
                start.press(forDuration: 0.1, thenDragTo: end)
            }
            XCTAssertTrue(inactive.waitForExistence(timeout: 5), "Inactive profile missing, Chinese: \(chinese)\n\(app.debugDescription)")
            XCTAssertTrue(inactive.label.contains(chinese ? "无号码" : "No Number"), inactive.label)
            XCTAssertFalse(inactive.label.contains("8986000000000000002"), inactive.label)
            app.terminate()
        }
    }

    func testProfileNumberCopiesFromOverview() {
        let app = launch()
        let active = "se-a:8986000000000000001"
        let copy = app.buttons["Copy"]

        overviewRowCoordinate(app.buttons["profileActions-" + active], in: app)?.press(forDuration: 1)
        XCTAssertTrue(copy.waitForExistence(timeout: 5), app.debugDescription)
        copy.tap()
        XCTAssertTrue(app.buttons["modemInfo"].isHittable)

        openActions(app, profile: active)
        app.buttons["Label"].tap()
        let input = app.textFields["detailInput"]
        replace(input, value: "")
        input.press(forDuration: 1)
        let paste = app.descendants(matching: .any)["Paste"].firstMatch
        XCTAssertTrue(paste.waitForExistence(timeout: 5))
        paste.tap()
        XCTAssertEqual(input.value as? String, "+8613800000000")
        app.buttons["Cancel"].tap()
        XCTAssertTrue(input.waitForNonExistence(timeout: 5))
        app.navigationBars.buttons.element(boundBy: 0).tap()

        let inactive = app.buttons["profileActions-se-a:8986000000000000002"]
        let inactiveCoordinate = overviewRowCoordinate(inactive, in: app)
        XCTAssertTrue(inactive.label.contains("No Number"), inactive.debugDescription)
        inactiveCoordinate?.press(forDuration: 1)
        XCTAssertFalse(copy.exists, app.debugDescription)
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
        XCTAssertEqual(app.buttons["seEID-se-a"].value as? String, "0001")
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64 KiB")
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
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64 KiB")
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
        XCTAssertEqual(app.staticTexts["seStorage-se-a"].value as? String, "64 KiB")
    }

    func testOverviewGlassDoesNotShadeTheCardGutter() throws {
        let app = launch("detail-traffic")
        let network = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
        let traffic = app.descendants(matching: .any)["internetTrafficCard"].firstMatch
        XCTAssertTrue(traffic.waitForExistence(timeout: 5))
        XCTAssertTrue(network.exists)
        let image = try XCTUnwrap(app.screenshot().image.cgImage)
        let frame = app.frame
        let scale = CGFloat(image.width) / frame.width

        func red(at point: CGPoint) throws -> Int {
            let x = ((point.x - frame.minX) * scale).rounded(.down)
            let y = ((point.y - frame.minY) * scale).rounded(.down)
            let sample = try XCTUnwrap(image.cropping(to: CGRect(x: x, y: y, width: 1, height: 1)))
            var pixel = [UInt8](repeating: 0, count: 4)
            try pixel.withUnsafeMutableBytes { buffer in
                let context = try XCTUnwrap(CGContext(
                    data: buffer.baseAddress, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4,
                    space: CGColorSpaceCreateDeviceRGB(),
                    bitmapInfo: CGBitmapInfo.byteOrder32Big.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue
                ))
                context.draw(sample, in: CGRect(x: 0, y: 0, width: 1, height: 1))
            }
            return Int(pixel[0])
        }

        for card in [network, traffic] {
            let bounds = card.frame
            // Sample either side of the list row's outer edge, outside the glass itself.
            let nearEdge = try red(at: CGPoint(x: bounds.minX - 3, y: bounds.midY))
            let gutter = try red(at: CGPoint(x: bounds.minX - 11, y: bounds.midY))
            XCTAssertEqual(nearEdge, gutter, accuracy: 4, card.identifier)
        }
    }

    func testInternetTrafficCardShowsLiveRatesAndUsesSystemIconTint() throws {
        let app = launch("detail-traffic")
        let card = app.descendants(matching: .any)["internetTrafficCard"].firstMatch
        let networkCard = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
        let profilesCard = app.descendants(matching: .any)["esimProfilesCard-se-a"].firstMatch
        let download = app.descendants(matching: .any)["internetTrafficDownload"].firstMatch
        let upload = app.descendants(matching: .any)["internetTrafficUpload"].firstMatch
        let usage = app.descendants(matching: .any)["internetTrafficUsage"].firstMatch
        let heading = card.descendants(matching: .any)["internetTrafficHeading"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 5))
        let egress = card.descendants(matching: .any)["internetEgressIP"].firstMatch
        XCTAssertTrue(egress.waitForExistence(timeout: 5))
        XCTAssertEqual(egress.value as? String, "203.0.113.1")
        XCTAssertTrue(card.staticTexts["Fixture ISP"].exists)
        let sampled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == '200 kb/s'"), object: upload)
        XCTAssertEqual(XCTWaiter.wait(for: [sampled], timeout: 5), .completed)
        XCTAssertGreaterThanOrEqual(card.frame.minY, networkCard.frame.maxY)
        XCTAssertTrue(profilesCard.waitForExistence(timeout: 5))
        XCTAssertEqual(profilesCard.frame.minX, card.frame.minX, accuracy: 1)
        XCTAssertEqual(profilesCard.frame.maxX, card.frame.maxX, accuracy: 1)
        XCTAssertEqual(profilesCard.frame.minY - card.frame.maxY, card.frame.minY - networkCard.frame.maxY, accuracy: 1)
        XCTAssertEqual(heading.label, "Internet")
        XCTAssertTrue(["1 Mb/s", "2 Mb/s"].contains(download.value as? String ?? ""))
        XCTAssertNotEqual(usage.value as? String, "Unknown")
        XCTAssertTrue(app.staticTexts["Last 30 seconds"].exists)
        try assertSystemTint(heading, minimumCoverage: 0.01)
        screenshot(app, name: "Live Internet traffic")
    }

    func testInternetEgressIPv6FitsWithChineseAndAccessibilityText() {
        let app = launch("detail-traffic-ipv6", chinese: true, large: true)
        let card = app.descendants(matching: .any)["internetTrafficCard"].firstMatch
        let egress = card.descendants(matching: .any)["internetEgressIP"].firstMatch
        for _ in 0..<6 {
            if egress.waitForExistence(timeout: 2), egress.isHittable { break }
            app.swipeUp()
        }
        XCTAssertTrue(egress.exists, app.debugDescription)
        XCTAssertEqual(egress.label, "出口 IP")
        XCTAssertEqual(egress.value as? String, "2001:0db8:1234:5678:90ab:cdef:1234:5678")
        XCTAssertGreaterThanOrEqual(egress.frame.minX, card.frame.minX)
        XCTAssertLessThanOrEqual(egress.frame.maxX, card.frame.maxX)
    }

    func testNetworkCardTitlesRegisteredOperatorAndKeepsSystemIconTintAcrossSIMSlots() throws {
        let app = launch("detail-roaming")
        let card = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
        let registration = app.descendants(matching: .any)["modemNetworkRegistration"].firstMatch
        let title = app.staticTexts["modemNetworkTitle"]
        XCTAssertTrue(registration.waitForExistence(timeout: 5))
        XCTAssertEqual(registration.label, "Roaming")
        XCTAssertEqual(title.label, "Travel Mobile")
        XCTAssertFalse(card.descendants(matching: .any)["modemNetworkDetails"].firstMatch.exists)
        let signal = app.descendants(matching: .any)["signal-device-1"].firstMatch
        XCTAssertTrue(signal.waitForExistence(timeout: 5))
        XCTAssertEqual(signal.value as? String, "78%")
        let technology = app.descendants(matching: .any)["modemNetworkTechnology"].firstMatch
        XCTAssertEqual(technology.value as? String, "LTE")
        XCTAssertEqual(signal.frame.midY, technology.frame.midY, accuracy: 2)
        XCTAssertGreaterThan(technology.frame.minX, signal.frame.maxX)
        XCTAssertLessThanOrEqual(technology.frame.height, title.frame.height)
        for (id, value) in [("Data", "Connected"), ("VoLTE", "Connected"), ("WiFiCalling", "Disabled")] {
            let status = app.descendants(matching: .any)["modemNetwork" + id].firstMatch
            XCTAssertEqual(status.value as? String, value, id)
        }
        let services = ["Data", "VoLTE", "WiFiCalling"].map {
            app.descendants(matching: .any)["modemNetwork" + $0].firstMatch.frame
        }
        XCTAssertLessThanOrEqual(services[0].height, 20, "Service status should fit on one line")
        for (left, right) in zip(services, services.dropFirst()) {
            XCTAssertEqual(left.minY, right.minY, accuracy: 1, "Service statuses should share a row at the default text size")
            XCTAssertEqual(left.height, right.height, accuracy: 1, "Service labels should fit on one line")
            XCTAssertGreaterThanOrEqual(right.minX, left.maxX)
        }
        XCTAssertLessThan(services[1].width, services[2].width, "VoLTE should leave room for the longer Wi-Fi Calling label")
        XCTAssertEqual(services[0].minX, card.frame.minX + 16, accuracy: 2)
        XCTAssertEqual(services[2].maxX, card.frame.maxX - 16, accuracy: 2)
        XCTAssertEqual(services[1].minX - services[0].maxX, services[2].minX - services[1].maxX, accuracy: 2)
        try assertSystemTint(signal)
        for id in ["modemNetworkData", "modemNetworkVoLTE"] {
            try assertSystemTint(app.descendants(matching: .any)[id].firstMatch, minimumCoverage: 0.008)
        }
        let wifiCalling = app.descendants(matching: .any)["modemNetworkWiFiCalling"].firstMatch
        XCTAssertLessThan(try systemTintCoverage(wifiCalling), 0.008, "Disabled Wi-Fi Calling should use a muted icon")
        screenshot(app, name: "Roaming network card with system tint")

        let picker = app.buttons["simSlotPicker"]
        picker.tap()
        tap(app.buttons["simSlot-2"], in: app)
        tap(app.alerts.buttons["confirmDetailAction"].firstMatch, in: app)
        let switched = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == 'SIM 2' AND enabled == true"), object: picker)
        XCTAssertEqual(XCTWaiter.wait(for: [switched], timeout: 10), .completed)
        try assertSystemTint(signal)
    }

    private func assertSystemTint(_ element: XCUIElement, minimumCoverage: Double = 0.1) throws {
        XCTAssertGreaterThan(try systemTintCoverage(element), minimumCoverage,
                             "\(element.label) should use the system tint")
    }

    private func systemTintCoverage(_ element: XCUIElement) throws -> Double {
        let image = try XCTUnwrap(element.screenshot().image.cgImage)
        var pixels = [UInt8](repeating: 0, count: image.width * image.height * 4)
        let tintedPixels = try pixels.withUnsafeMutableBytes { buffer in
            let context = try XCTUnwrap(CGContext(
                data: buffer.baseAddress, width: image.width, height: image.height,
                bitsPerComponent: 8, bytesPerRow: image.width * 4,
                space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGBitmapInfo.byteOrder32Big.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue
            ))
            context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
            var count = 0
            for offset in stride(from: 0, to: buffer.count, by: 4) {
                guard buffer[offset + 3] > 200 else { continue }
                let red = Int(buffer[offset])
                let green = Int(buffer[offset + 1])
                let blue = Int(buffer[offset + 2])
                if blue > 150, blue >= green + 40, green >= red + 40 { count += 1 }
            }
            return count
        }
        return Double(tintedPixels) / Double(image.width * image.height)
    }

    func testSecureElementMetadataAndCopy() {
        let app = launch()
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
        screenshot(app, name: "eSIM cards and metadata")
        for (id, eid, storage, countLabel) in [
            ("se-a", "89049032000000000000000000000001", "64 KiB", "2 profiles"),
            ("se-b", "89049032000000000000000000000002", "32 KiB", "1 profile")
        ] {
            let identifier = app.buttons["seEID-" + id]
            for _ in 0..<4 where !identifier.isHittable { app.swipeUp() }
            XCTAssertTrue(identifier.isHittable, "Could not reach EID for \(id)")
            XCTAssertEqual(identifier.value as? String, String(eid.suffix(4)))
            XCTAssertEqual(app.staticTexts["seStorage-" + id].value as? String, storage)
            XCTAssertTrue(app.staticTexts[countLabel].exists)
            XCTAssertLessThan(app.buttons["addESIM-" + id].frame.midY, identifier.frame.midY)
            identifier.press(forDuration: 1)
            screenshot(app, name: "EID copy menu " + id)
            let copy = app.buttons["Copy"]
            XCTAssertTrue(copy.waitForExistence(timeout: 5), app.debugDescription)
            copy.tap()
            openActions(app, profile: id + ":8986000000000000002")
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
        screenshot(app, name: "eSIM cards after copying EID")
    }

    func testNetworkCardCollapsesWhileScrollingAndKeepsSlotPicker() {
        for chinese in [false, true] {
            let app = launch("detail-roaming", chinese: chinese, large: true, dark: chinese)
            let compactBar = app.descendants(matching: .any)["esimCompactNetworkBar"].firstMatch
            let registration = app.descendants(matching: .any)["modemNetworkRegistration"].firstMatch
            XCTAssertTrue(registration.waitForExistence(timeout: 5))
            XCTAssertFalse(compactBar.exists)
            screenshot(app, name: "Large text network card \(chinese)")
            for _ in 0..<4 where !compactBar.exists { app.swipeUp() }
            XCTAssertTrue(compactBar.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(compactBar.staticTexts["Travel Mobile"].exists)
            XCTAssertLessThan(compactBar.frame.height, app.frame.height / 2)
            // The glass bar's menu wrapper can report false for isHittable; verify the actual interaction.
            let slotPicker = compactBar.buttons["simSlotPicker"].firstMatch
            XCTAssertTrue(slotPicker.exists, app.debugDescription)
            slotPicker.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
            XCTAssertTrue(app.buttons["simSlot-2"].waitForExistence(timeout: 5), app.debugDescription)
            app.buttons["simSlot-2"].tap()
            app.alerts.buttons[chinese ? "取消" : "Cancel"].tap()
            screenshot(app, name: "Compact network bar after scrolling \(chinese)")
            for _ in 0..<8 where compactBar.exists {
                let top = compactBar.frame.maxY
                let bottom = app.tabBars.firstMatch.frame.minY
                let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: (top + 12) / app.frame.height))
                let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: (bottom - 12) / app.frame.height))
                start.press(forDuration: 0.1, thenDragTo: end)
            }
            XCTAssertTrue(compactBar.waitForNonExistence(timeout: 5))
            XCTAssertTrue(registration.waitForExistence(timeout: 5))
            openActions(app, profile: "se-a:8986000000000000001")
            app.navigationBars.buttons.element(boundBy: 0).tap()
            tapOverviewRow(app.buttons["addESIM-se-a"], in: app)
            XCTAssertTrue(app.alerts.buttons["skipESIMDiscoveries"].firstMatch.waitForExistence(timeout: 5))
            app.terminate()
        }
    }

    func testIncorrectPINClearsCredentialsAndRefreshesAttempts() {
        let app = launch("detail-locked")
        let pin = app.secureTextFields["simUnlockPIN"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        XCTAssertEqual(app.secureTextFields.count, 1)
        XCTAssertFalse(app.switches["simPINProtection"].exists)
        pin.tap(); pin.typeText("0000")
        tap(app.buttons["simUnlockSubmit"], in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.alerts.staticTexts["Incorrect PIN. Check the remaining attempts before trying again."].exists)
        app.alerts.buttons["OK"].tap()
        XCTAssertTrue(app.staticTexts["PIN attempts remaining: 2"].exists)
        XCTAssertFalse(app.buttons["simUnlockSubmit"].isEnabled)
        screenshot(app, name: "PIN error and remaining attempts")
        pin.tap(); pin.typeText("1234")
        tap(app.buttons["simUnlockSubmit"], in: app)
        XCTAssertTrue(pin.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
    }

    func testSIMPINManagementUsesNativeFailureAlert() {
        let app = launch("detail-physical")
        tapOverviewRow(app.buttons["physicalSIMPIN"], in: app)
        let protection = app.switches["simPINProtection"]
        XCTAssertTrue(protection.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Refresh status"].exists)
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: protection)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
        protection.switches.firstMatch.tap()
        let pin = app.secureTextFields["simPINCurrent"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5), app.debugDescription)
        pin.tap(); pin.typeText("0000")
        tap(app.buttons["simPINSubmit"], in: app)
        XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
        app.alerts.buttons["OK"].tap()
        XCTAssertTrue(pin.exists)
        XCTAssertFalse(app.buttons["simPINSubmit"].isEnabled)
        XCTAssertTrue(app.staticTexts["PIN attempts remaining: 2"].exists)
        pin.tap(); pin.typeText("1234")
        tap(app.buttons["simPINSubmit"], in: app)
        expectToggle(protection, value: "0")
        XCTAssertFalse(app.staticTexts["The operation succeeded. Refresh status before another change."].exists)
        protection.switches.firstMatch.tap()
        pin.tap(); pin.typeText("1234")
        tap(app.buttons["simPINSubmit"], in: app)
        expectToggle(protection, value: "1")
        tap(app.buttons["Change PIN"], in: app)
        pin.tap(); pin.typeText("1234")
        let replacement = app.secureTextFields["simPINNew"]
        replacement.tap(); replacement.typeText("0001")
        app.secureTextFields["simPINConfirmation"].tap()
        app.secureTextFields["simPINConfirmation"].typeText("0001")
        tap(app.buttons["simPINSubmit"], in: app)
        XCTAssertTrue(pin.waitForNonExistence(timeout: 5))
        screenshot(app, name: "SIM PIN settings")
    }

    func testSIMPINRefreshKeepsProgressInSubmitButton() {
        let app = launch("detail-physical-pin-loading")
        tapOverviewRow(app.buttons["physicalSIMPIN"], in: app)
        let protection = app.switches["simPINProtection"]
        expectToggle(protection, value: "1")
        protection.switches.firstMatch.tap()
        let pin = app.secureTextFields["simPINCurrent"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        pin.tap(); pin.typeText("1234")
        let submit = app.buttons["simPINSubmit"]
        let size = submit.frame.size
        tap(submit, in: app)
        let working = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", "Working…"), object: submit)
        XCTAssertEqual(XCTWaiter.wait(for: [working], timeout: 5), .completed, submit.debugDescription)
        XCTAssertEqual(submit.label, "Disable SIM PIN")
        XCTAssertFalse(submit.isEnabled)
        XCTAssertEqual(submit.frame.width, size.width, accuracy: 1)
        XCTAssertEqual(submit.frame.height, size.height, accuracy: 1)
        XCTAssertFalse(app.staticTexts["Reading SIM PIN status…"].exists)
        let completed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", "0"), object: protection)
        XCTAssertEqual(XCTWaiter.wait(for: [completed], timeout: 10), .completed, app.debugDescription)
        XCTAssertTrue(submit.waitForNonExistence(timeout: 5))
    }

    func testPINTransitionsToPUKInUnlockSheet() {
        let app = launch("detail-locked")
        let current = app.secureTextFields["simUnlockPIN"]
        for remaining in [2, 1, 0] {
            XCTAssertTrue(current.waitForExistence(timeout: 5))
            current.tap(); current.typeText("0000")
            tap(app.buttons["simUnlockSubmit"], in: app)
            XCTAssertTrue(app.alerts.firstMatch.waitForExistence(timeout: 5))
            app.alerts.buttons["OK"].tap()
            let attempts = remaining == 0 ? "PUK attempts remaining: 10" : "PIN attempts remaining: \(remaining)"
            XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", attempts)).firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        }
        let replacement = app.secureTextFields["simUnlockNewPIN"]
        XCTAssertTrue(replacement.waitForExistence(timeout: 5))
        current.tap(); current.typeText("12345678")
        replacement.tap(); replacement.typeText("0001")
        app.secureTextFields["simUnlockConfirmation"].tap()
        app.secureTextFields["simUnlockConfirmation"].typeText("0001")
        tap(app.buttons["simUnlockSubmit"], in: app)
        XCTAssertTrue(current.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
    }

    func testAddESIMRowsAndCopy() {
        let app = launch("detail-discovery-loading")
        let install = app.buttons["addESIM-se-a"]
        XCTAssertTrue(install.waitForExistence(timeout: 5))
        screenshot(app, name: "Compact eSIM overview")
        XCTAssertEqual(install.label, "Add eSIM")
        XCTAssertGreaterThanOrEqual(install.frame.height, 44)
        let idleWidth = install.frame.width
        XCTAssertFalse(app.buttons["installESIM"].exists)
        tapOverviewRow(install, in: app)
        XCTAssertTrue(app.activityIndicators.firstMatch.waitForExistence(timeout: 2))
        XCTAssertFalse(install.isEnabled)
        XCTAssertFalse(app.buttons["addESIM-se-b"].isEnabled)
        XCTAssertEqual(install.label, "Add eSIM")
        XCTAssertGreaterThan(install.frame.width, idleWidth)
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

    func testReminderOpensAtFullHeightForPhysicalSIMAndESIM() {
        for physicalSIM in [true, false] {
            let app = launch(physicalSIM ? "detail-physical" : "detail")
            let reminder = app.buttons["Reminder"]
            if physicalSIM {
                tapOverviewRow(reminder, in: app)
            } else {
                openActions(app)
                tap(reminder, in: app)
            }
            let save = app.buttons["saveReminder"]
            XCTAssertTrue(save.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertLessThan(save.frame.midY, app.frame.minY + app.frame.height / 3)
            app.terminate()
        }
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

    func testPhysicalAutomationShowsActionAndNextRun() {
        let app = launch("detail-physical-automation")
        let row = app.buttons["physicalSIMAutomations"]
        let scheduled = XCTNSPredicateExpectation(
            predicate: NSPredicate(format: "value BEGINSWITH %@ AND value CONTAINS %@", "Send SMS, ", "2099"),
            object: row
        )
        XCTAssertEqual(XCTWaiter.wait(for: [scheduled], timeout: 5), .completed)
        tapOverviewRow(row, in: app)
        let recipient = app.textFields["Phone number"]
        XCTAssertTrue(recipient.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(recipient.value as? String, "+12025550123")
        let wifiOnly = app.switches["automationWiFiCallingOnly"]
        XCTAssertTrue(wifiOnly.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertEqual(wifiOnly.value as? String, "1")
        let wifiOnlyHint = app.staticTexts["automationWiFiCallingOnlyHint"]
        XCTAssertTrue(wifiOnlyHint.waitForExistence(timeout: 5), app.debugDescription)
    }

    func testESIMAutomationDistinguishesCompletedAndUnsetLines() {
        let app = launch("detail-esim-automation")
        for profile in ["se-a:8986000000000000001", "se-a:8986000000000000002"] {
            let row = app.buttons["profileActions-" + profile]
            let bell = XCTNSPredicateExpectation(
                predicate: NSPredicate(format: "label CONTAINS %@", "Reminder or automation set"),
                object: row
            )
            XCTAssertEqual(XCTWaiter.wait(for: [bell], timeout: 5), .completed, row.label)
            XCTAssertFalse(row.label.contains("Use mobile data"), row.label)
        }
        openActions(app)
        let row = app.buttons["esimAutomations"]
        expectToggle(row, value: "Use mobile data, No upcoming run")
        tap(row, in: app)
        let amount = app.textFields["Amount"]
        XCTAssertTrue(amount.waitForExistence(timeout: 5))
        XCTAssertEqual(amount.value as? String, "1")
        XCTAssertFalse(app.switches["automationWiFiCallingOnly"].exists)
        app.navigationBars.buttons["Cancel"].tap()
        XCTAssertTrue(amount.waitForNonExistence(timeout: 5))
        openActions(app, profile: "se-b:8986000000000000002")
        expectToggle(row, value: "Add automation")
        tap(row, in: app)
        XCTAssertTrue(amount.waitForExistence(timeout: 5))
        XCTAssertEqual(amount.value as? String, "Amount")
        let unit = app.buttons["automationDataUnit"]
        XCTAssertTrue(unit.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(unit.isHittable)
        unit.tap()
        XCTAssertTrue(app.buttons["KiB"].waitForExistence(timeout: 5))
    }

    func testESIMSetupSheetUsesSystemIconTint() throws {
        let app = launch("detail-roaming")
        installForm(app, manual: false)
        let scan = app.buttons["scanESIM"]
        XCTAssertTrue(scan.waitForExistence(timeout: 5))
        try assertSystemTint(scan, minimumCoverage: 0.005)
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

    func testTransferFromAnotherSigmoStartsInsideDownload() {
        let app = launch()
        installForm(app, manual: false)
        tap(app.buttons["transferFromSigmo"], in: app)
        let address = app.textFields["peerAddress"]
        XCTAssertTrue(address.waitForExistence(timeout: 5))
        address.tap(); address.typeText("https://source.example")
        tap(app.buttons["requestPeerTransfer"], in: app)
        XCTAssertFalse(app.buttons["transferSource-modem:device-2"].exists)
        XCTAssertFalse(app.buttons["startESIMTransfer"].exists)
        XCTAssertTrue(app.buttons["closeCompletedESIM"].waitForExistence(timeout: 5))
    }

    private func launchPeerApprovals(chinese: Bool = false, large: Bool = false, dark: Bool = false) -> XCUIApplication {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment["SIGMO_UI_SCENARIO"] = "detail-peer-approval"
        app.launchEnvironment["SIGMO_UI_RESET"] = "1"
        app.launchEnvironment["SIGMO_UI_APPEARANCE"] = dark ? "dark" : "light"
        app.launchArguments = ["-AppleLanguages", chinese ? "(zh-Hans)" : "(en)", "-AppleLocale", chinese ? "zh_CN" : "en_US", "-UIPreferredContentSizeCategoryName", large ? "UICTContentSizeCategoryAccessibilityXXXL" : "UICTContentSizeCategoryL"]
        app.launch()
        XCTAssertTrue(app.staticTexts["peerTransferHeading"].waitForExistence(timeout: 10))
        return app
    }

    private func revealPeerTransferElement(_ element: XCUIElement, in app: XCUIApplication) {
        let scroll = app.scrollViews["peerTransferRequest"]
        for _ in 0..<12 {
            let viewport = CGRect(x: scroll.frame.minX, y: scroll.frame.minY + 8,
                                  width: scroll.frame.width,
                                  height: app.buttons["allowPeerTransfer"].frame.minY - 24 - scroll.frame.minY)
            if element.exists && element.isHittable && viewport.contains(element.frame) { return }
            // Keep gestures above the fixed action footer at accessibility text sizes.
            let start = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(
                dx: scroll.frame.midX,
                dy: app.buttons["allowPeerTransfer"].frame.minY - 32
            ))
            let end = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(
                dx: scroll.frame.midX,
                dy: scroll.frame.minY + 48
            ))
            start.press(forDuration: 0.1, thenDragTo: end)
        }
        XCTFail("Cannot reveal transfer details: \(element)\n\(app.debugDescription)")
    }

    private func choosePeerTransferProfile(in app: XCUIApplication) {
        let source = app.buttons["peerTransferSource"]
        revealPeerTransferElement(source, in: app)
        source.coordinate(withNormalizedOffset: CGVector(dx: 0.94, dy: 0.5)).tap()
        tap(app.buttons["Source modem"], in: app)
        let profile = app.buttons["peerTransferProfilePicker"]
        revealPeerTransferElement(profile, in: app)
        profile.coordinate(withNormalizedOffset: CGVector(dx: 0.94, dy: 0.5)).tap()
        tap(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Shared line")).firstMatch, in: app)
    }

    func testSourceApprovalsAppearOnTheHomePage() {
        let app = launchPeerApprovals()
        XCTAssertEqual(app.staticTexts["peerTransferHeading"].label, "Choose an eSIM to transfer")
        XCTAssertFalse(app.buttons["allowPeerTransfer"].isEnabled)
        XCTAssertFalse(app.staticTexts["peerTransferProfile"].exists)
        let contentWidth = min(app.scrollViews["peerTransferRequest"].frame.width, 560) - 48
        for identifier in ["peerTransferSource", "peerTransferProfilePicker"] {
            let row = app.buttons[identifier]
            XCTAssertEqual(row.frame.width, contentWidth, accuracy: 2)
            XCTAssertGreaterThanOrEqual(row.frame.height, 44)
        }
        choosePeerTransferProfile(in: app)
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "Shared line")).firstMatch.exists)
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "deletion of the source profile")).firstMatch.exists)
        let allow = app.buttons["allowPeerTransfer"]
        XCTAssertTrue(allow.isEnabled)
        XCTAssertGreaterThanOrEqual(allow.frame.height, 44)
        screenshot(app, name: "Sigmo source selection English")
        allow.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5)).tap()
        XCTAssertTrue(app.buttons["closePeerTransferRequest"].waitForExistence(timeout: 5))
        app.buttons["closePeerTransferRequest"].tap()
        XCTAssertTrue(app.buttons["closePeerTransferRequest"].waitForNonExistence(timeout: 5))
    }

    func testSourceApprovalDetailsInChineseLargeDark() {
        let app = launchPeerApprovals(chinese: true, large: true, dark: true)
        XCTAssertEqual(app.staticTexts["peerTransferHeading"].label, "选择要转出的 eSIM")
        choosePeerTransferProfile(in: app)
        let eid = app.staticTexts.matching(NSPredicate(format: "label == %@", String(repeating: "1", count: 32))).firstMatch
        XCTAssertFalse(eid.exists)
        let details = app.buttons["peerTransferDetails"]
        revealPeerTransferElement(details, in: app)
        details.tap()
        revealPeerTransferElement(eid, in: app)
        XCTAssertTrue(eid.isHittable, app.debugDescription)
        XCTAssertTrue(app.buttons["rejectPeerTransfer"].isHittable)
        screenshot(app, name: "Sigmo source selection Chinese large dark")
        tap(app.buttons["rejectPeerTransfer"], in: app)
        XCTAssertTrue(app.buttons["closePeerTransferRequest"].waitForExistence(timeout: 5))
        tap(app.buttons["closePeerTransferRequest"], in: app)
        XCTAssertTrue(app.buttons["closePeerTransferRequest"].waitForNonExistence(timeout: 5))
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
        XCTAssertEqual(close.label, "Cancel")
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
        let pin = locked.secureTextFields["simUnlockPIN"]
        XCTAssertTrue(pin.waitForExistence(timeout: 5)); pin.tap(); pin.typeText("1234")
        tap(locked.buttons["simUnlockSubmit"], in: locked)
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
        XCTAssertTrue(app.buttons["confirmESIMPreview"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["startESIMDownload"].exists)
        screenshot(app, name: "Download preview after returning to foreground")
        tap(app.buttons["confirmESIMPreview"], in: app)
        let code = app.secureTextFields["downloadConfirmation"]
        XCTAssertTrue(code.waitForExistence(timeout: 5))
        code.tap()
        code.typeText("1234")
        tap(app.buttons["Confirm"], in: app)
        XCTAssertTrue(app.staticTexts["eSIM Added"].waitForExistence(timeout: 10))
        app.buttons["closeCompletedESIM"].tap()
        XCTAssertTrue(app.buttons["addESIM-se-a"].waitForExistence(timeout: 5))
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
        XCTAssertFalse(app.buttons["historyAssignmentLater"].exists)
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
            XCTAssertFalse(app.buttons["historyAssignmentLater"].exists)
            XCTAssertTrue(other.exists)
            XCTAssertFalse(app.alerts.firstMatch.exists)
            app.terminate()
        }
    }

    func testDeletionWithHistoryPresentsAssignment() {
        let app = launch("detail-deletion-history")
        openActions(app)
        tap(app.buttons["Delete eSIM"], in: app)
        tap(app.alerts.buttons["confirmDetailAction"].firstMatch, in: app)

        let later = app.buttons["historyAssignmentLater"]
        XCTAssertTrue(later.waitForExistence(timeout: 5), app.debugDescription)
        later.tap()
        XCTAssertTrue(later.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.buttons["profileActions-se-a:8986000000000000002"].waitForNonExistence(timeout: 5))
    }

    func testExplicitRefreshReloadsProfileSnapshot() {
        let app = launch("detail-refresh")
        let row = app.buttons["profileActions-se-a:8986000000000000002"]
        XCTAssertTrue(row.waitForExistence(timeout: 12))
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: row)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 5), .completed)
        XCTAssertFalse(row.label.contains("Travel Refreshed"))

        let bar = app.descendants(matching: .any)["esimNetworkBar"].firstMatch
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        let frame = app.frame
        let top = bar.frame.maxY + 20
        let bottom = app.tabBars.firstMatch.frame.minY - 20
        XCTAssertGreaterThan(bottom - top, 100)
        // Start the refresh gesture below the expanded network card.
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (top - frame.minY) / frame.height))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: (bottom - frame.minY) / frame.height))
        start.press(forDuration: 0.1, thenDragTo: end)
        let refreshed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label CONTAINS 'Travel Refreshed'"), object: row)
        XCTAssertEqual(XCTWaiter.wait(for: [refreshed], timeout: 10), .completed, app.debugDescription)
        XCTAssertTrue(row.label.contains("Off"), row.label)
        XCTAssertTrue(row.isEnabled)
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

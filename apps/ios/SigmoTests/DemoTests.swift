import XCTest
import WebKit
@testable import Sigmo

@MainActor
final class DemoTests: XCTestCase {
    private func makeDemo() async throws -> (DemoSession, BackendSession) {
        let demo = try DemoSession()
        addTeardownBlock { await demo.stop() }
        await demo.store.refreshAll()
        XCTAssertEqual(demo.store.onlineSessions.count, 2)
        let session = try XCTUnwrap(demo.store.orderedSessions().first)
        XCTAssertTrue(session.isDemo)
        XCTAssertTrue(session.canAccessDevices)
        return (demo, session)
    }

    func testMissingDemoTransportsNeverUseLiveDefaults() async throws {
        let configuration = BackendConfiguration(name: "Demo", address: URL(string: "https://demo.invalid/api/v1")!)
        let api = BackendAPI(configuration: configuration, isDemo: true)
        do { _ = try await api.modems(token: nil); XCTFail("Missing HTTP transport succeeded") }
        catch let error as BackendFailure { XCTAssertEqual(error.code, "demo.unsupportedRequest") }
        do { _ = try await api.socket(["modems", "device-1", "calls", "events"], token: nil); XCTFail("Missing socket transport succeeded") }
        catch let error as BackendFailure { XCTAssertEqual(error.code, "demo.unsupportedRequest") }
    }

    func testMessagesShareDeviceAndUnifiedHistory() async throws {
        let (demo, session) = try await makeDemo()
        let modem = try XCTUnwrap(session.modems.first)
        let sent = try await session.protectedRequest { api, token in
            try await api.sendMessage(modemID: modem.id, submission: SMSSubmission(to: "+12025550101", text: "Demo write", expectedProfileId: modem.sim.identifier), token: token)
        }
        let conversation = try XCTUnwrap(sent.conversationId)
        let deviceMessages = try await session.protectedRequest { api, token in try await api.conversationMessages(conversation, token: token) }
        XCTAssertEqual(deviceMessages.last?.text, "Demo write")
        let threads = try await session.protectedRequest { api, token in try await api.messageThreads(search: "", cursor: "", token: token) }
        let thread = try XCTUnwrap(threads.items.first { $0.participant == "+12025550101" })
        XCTAssertEqual(thread.latestMessage.text, "Demo write")
        let timeline = try await session.protectedRequest { api, token in try await api.threadMessages(thread.id, cursor: "", token: token) }
        _ = try await session.protectedRequest { api, token in try await api.readThread(thread.id, messageIDs: timeline.items.map(\.id), token: token) }
        let snapshot = try await session.protectedRequest { api, token in try await api.conversations(modemID: nil, token: token) }
        XCTAssertEqual(snapshot.items.first { $0.id == conversation }?.unreadCount, 0)
        try await demo.receiveMessage()
        let updated = try await session.protectedRequest { api, token in try await api.messageThreads(search: "", cursor: "", token: token) }
        XCTAssertEqual(updated.items.first { $0.id == thread.id }?.unreadCount, 1)
        try await session.protectedRequest { api, token in try await api.deleteThread(thread.id, token: token) }
        let deleted = try await session.protectedRequest { api, token in try await api.conversations(modemID: nil, token: token) }
        XCTAssertFalse(deleted.items.contains { $0.participant == thread.participant })
    }

    func testContactsAndSettingsStayWithinTheirDemoConnection() async throws {
        let (demo, first) = try await makeDemo()
        let second = try XCTUnwrap(demo.store.orderedSessions().last)
        let saved = try await first.protectedRequest { api, token in
            try await api.saveContact(SigmoContact(name: "Demo only", numbers: [ContactPhone(number: "+12025550103")]), token: token)
        }
        let other = try await second.protectedRequest { api, token in try await api.contacts(token: token) }
        XCTAssertFalse(other.contains { $0.id == saved.id })
        try await first.protectedRequest { api, token in
            try await api.detailMutation("device-1", path: ["settings"], method: "PUT", value: ModemDeviceSettings(alias: "My Demo Modem", mss: 240), token: token)
        }
        await first.refresh(force: true)
        await second.refresh(force: true)
        XCTAssertEqual(first.modems.first?.name, "My Demo Modem")
        XCTAssertNotEqual(second.modems.first?.name, "My Demo Modem")
        let archive = try await first.protectedRequest { api, token in
            try await api.request(["settings", "mcp", "skills", "sigmo-control"], method: "GET", token: token).data
        }
        XCTAssertEqual(Array(archive.prefix(4)), [0x50, 0x4b, 0x03, 0x04])
        XCTAssertNotNil(archive.range(of: Data("sigmo-control/SKILL.md".utf8)))
    }

    func testESIMActivationUpdatesTheAvailableCommunicationLine() async throws {
        let (_, session) = try await makeDemo()
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        let travel = try XCTUnwrap(detail.groups.flatMap(\.profiles).first { !$0.enabled })
        let enabled = await detail.enable(travel)
        XCTAssertTrue(enabled)
        let lines = try await session.protectedRequest { api, token in try await api.communicationLines(token: token) }
        XCTAssertEqual(lines.filter(\.active).map(\.profileId), [travel.iccid])
        XCTAssertEqual(session.modems.first?.sim.identifier, travel.iccid)
    }

    func testDownloadSocketMutatesProfilesAndStopsOnExit() async throws {
        let (demo, session) = try await makeDemo()
        let socket = try await session.protectedRequest { api, token in
            try await api.socket(["modems", "device-1", "esim-downloads", "sessions"], token: token)
        }
        try await socket.send(JSONEncoder().encode(ESIMClientMessage(type: "start", seId: "se-a", smdp: "demo.invalid", activationCode: "DEMO")))
        _ = try await socket.receive()
        let preview = try JSONDecoder().decode(ESIMServerMessage.self, from: await socket.receive())
        XCTAssertEqual(preview.type, "preview")
        try await socket.send(JSONEncoder().encode(ESIMClientMessage(type: "confirm", accept: true)))
        let confirmation = try JSONDecoder().decode(ESIMServerMessage.self, from: await socket.receive())
        XCTAssertEqual(confirmation.type, "confirmation_code_required")
        try await socket.send(JSONEncoder().encode(ESIMClientMessage(type: "confirmation_code", confirmationCode: "123456")))
        _ = try await socket.receive()
        let completed = try JSONDecoder().decode(ESIMServerMessage.self, from: await socket.receive())
        XCTAssertEqual(completed.type, "completed")
        let profiles = try await session.protectedRequest { api, token in try await api.profiles("device-1", token: token) }
        XCTAssertEqual(profiles.ses.flatMap(\.profiles).count, 3)
        await demo.stop()
        do { _ = try await socket.receive(); XCTFail("Closed demo socket returned data") }
        catch is CancellationError {} catch { XCTFail("Unexpected error: \(error)") }
    }

    func testLocalReaderAndCarrierPageNeedNoNetworkOrHardware() async throws {
        let (_, session) = try await makeDemo()
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        let flow = ESIMFlowState(detail: detail, seID: "se-a")
        await flow.loadSources()
        let source = try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" })
        flow.selectSource(source)
        await flow.loadProfiles()
        XCTAssertNil(flow.failure)
        XCTAssertFalse(flow.profiles.isEmpty)
        flow.interrupt()
        XCTAssertTrue(DemoResources.carrierHTML.contains("Content-Security-Policy"))
    }

    func testCarrierSetupCompletesThroughTheLocalWebPage() async throws {
        let (_, session) = try await makeDemo()
        struct Enable: Encodable, Sendable { let enabled = true; let underlay = WiFiUnderlay(mode: "modem", modemId: "device-1") }
        try await session.protectedRequest { api, token in
            try await api.detailMutation("device-1", path: ["wifi-calling", "settings"], method: "PUT", value: Enable(), token: token)
        }
        let wifi = try await session.protectedRequest { api, token in try await api.detailWiFi("device-1", token: token) }
        let sheet = try XCTUnwrap(wifi.websheet)
        let url = try await session.protectedRequest { api, _ in try CarrierWebsheetAddress.url(sheet, api: api) }
        let received = expectation(description: "Demo carrier callback")
        var callback: CarrierCallback?
        let page = CarrierWebsheetView(url: url, callback: { callback = $0; received.fulfill() }, failure: { XCTFail($0.message) }, session: session)
        let coordinator = page.makeCoordinator()
        let webView = page.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        let click = "window.addEventListener('load', () => document.querySelector('button').click());"
        webView.configuration.userContentController.addUserScript(WKUserScript(source: click, injectionTime: .atDocumentEnd, forMainFrameOnly: true))
        webView.load(URLRequest(url: CarrierResourceLoader.localURL(url)))
        await fulfillment(of: [received], timeout: 10)
        let result = try XCTUnwrap(callback)
        XCTAssertEqual(result.event, "entitlementChanged")
        try await session.protectedRequest { api, token in try await api.carrierCallback(sheet.id, callback: result, token: token) }
        let connected = try await session.protectedRequest { api, token in try await api.detailWiFi("device-1", token: token) }
        XCTAssertTrue(connected.connected)
        XCTAssertNil(connected.websheet)
    }

    func testCallsRecordAndAppearInBothHistories() async throws {
        let (demo, session) = try await makeDemo()
        let line = try XCTUnwrap(demo.calls.availableLines.first { $0.backendID == session.id })
        await demo.calls.dial(number: "+12025550101", line: line)
        for _ in 0..<100 where !demo.calls.audio.ready { try await Task.sleep(for: .milliseconds(20)) }
        XCTAssertTrue(demo.calls.audio.ready)
        XCTAssertNil(demo.calls.failure)
        await demo.calls.toggleRecording()
        await demo.calls.toggleRecording()
        XCTAssertEqual(demo.calls.recordings.items.count, 1)
        XCTAssertTrue(demo.calls.recordings.items.first?.complete == true)
        let call = try XCTUnwrap(demo.calls.primary)
        await demo.calls.act(.end(call.id))
        let deviceHistory = try await session.protectedRequest { api, token in try await api.calls(modemID: "device-1", token: token) }
        let history = try await session.protectedRequest { api, token in try await api.callHistory(search: "", cursor: "", token: token) }
        XCTAssertTrue(history.items.contains { $0.id == call.record?.id && $0.terminal })
        XCTAssertEqual(Set(history.items.map(\.id)), Set(deviceHistory.filter(\.terminal).map(\.id)))
    }

    func testExitRemovesOnlyDemoStorageAndNextSessionResetsData() async throws {
        let (demo, session) = try await makeDemo()
        let realConfiguration = try? Data(contentsOf: ConfigurationFile.application.url)
        let realSelection = UserDefaults.standard.string(forKey: "communication.backend")
        demo.defaults.set("demo-only", forKey: "communication.backend")
        let recordingDirectory = demo.calls.recordings.directory
        demo.store.deleteBackend(session.id)
        await demo.stop()
        XCTAssertFalse(FileManager.default.fileExists(atPath: recordingDirectory.path))
        XCTAssertEqual(try? Data(contentsOf: ConfigurationFile.application.url), realConfiguration)
        XCTAssertEqual(UserDefaults.standard.string(forKey: "communication.backend"), realSelection)
        let (next, _) = try await makeDemo()
        XCTAssertEqual(next.store.settings.backends.count, 2)
        XCTAssertNil(next.defaults.string(forKey: "communication.backend"))
    }

    func testEditedAddressPreservesStateAndLocalSockets() async throws {
        let (demo, original) = try await makeDemo()
        let contact = try await original.protectedRequest { api, token in
            try await api.saveContact(SigmoContact(name: "Preserved", numbers: [ContactPhone(number: "+12025550103")]), token: token)
        }
        XCTAssertTrue(demo.store.saveBackend(id: original.id, name: "Renamed Demo", address: "https://renamed.demo.invalid/proxy/sigmo"))
        await demo.store.refreshAll()
        let session = try XCTUnwrap(demo.store.sessions[original.id])
        XCTAssertTrue(session.isOnline)
        let contacts = try await session.protectedRequest { api, token in try await api.contacts(token: token) }
        XCTAssertTrue(contacts.contains { $0.id == contact.id })
        let threads = try await session.protectedRequest { api, token in try await api.messageThreads(search: "+12025550101", cursor: "", token: token) }
        let thread = try XCTUnwrap(threads.items.first)
        let messages = try await session.protectedRequest { api, token in try await api.threadMessages(thread.id, cursor: "", token: token) }
        XCTAssertFalse(messages.items.isEmpty)
        let socket = try await session.protectedRequest { api, token in
            try await api.socket(["modems", "device-1", "calls", "events"], token: token)
        }
        defer { socket.close() }
        try await demo.receiveCall()
        let event = try JSONDecoder().decode(CallEvent.self, from: await socket.receive())
        XCTAssertEqual(event.call?.state, "ringing")
    }

    func testNotificationsAndSampleEventsFollowRemainingConnections() async throws {
        let (demo, first) = try await makeDemo()
        let second = try XCTUnwrap(demo.store.orderedSessions().last)
        demo.notifications(for: first.id).sms = false
        XCTAssertFalse(demo.notifications(for: first.id).sms)
        XCTAssertTrue(demo.notifications(for: second.id).sms)
        demo.store.deleteBackend(first.id)
        let before = second.unreadCounts["device-1", default: 0]
        try await demo.receiveMessage()
        XCTAssertEqual(second.unreadCounts["device-1"], before + 1)
        demo.store.deleteBackend(second.id)
        do { try await demo.receiveMessage(); XCTFail("Generated a message for a deleted connection") }
        catch let failure as BackendFailure { XCTAssertEqual(failure.code, "demo.noConnection") }
    }

    func testResetAndConcurrentExitDisposeEverySession() async throws {
        let presentation = try DemoPresentation()
        addTeardownBlock { await presentation.close() }
        let original = presentation.session
        let originalDirectory = original.calls.recordings.directory.deletingLastPathComponent()
        original.notifications(for: original.store.settings.backends[0].id).sms = false
        await presentation.perform(.reset)
        XCTAssertFalse(presentation.session === original)
        XCTAssertFalse(FileManager.default.fileExists(atPath: originalDirectory.path))
        let replacement = presentation.session
        XCTAssertTrue(replacement.notifications(for: replacement.store.settings.backends[0].id).sms)

        async let reset: Void = presentation.perform(.reset)
        async let close: Void = presentation.close()
        await close
        // close() must wait for the in-flight reset and for storage cleanup.
        let last = presentation.session
        XCTAssertFalse(FileManager.default.fileExists(atPath: replacement.calls.recordings.directory.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: last.calls.recordings.directory.path))
        await reset
        await presentation.perform(.reset)
        XCTAssertTrue(presentation.session === last)
        XCTAssertNil(presentation.failure)
    }

    func testConcurrentStopWaitsForCleanupAndRejectsLateCalls() async throws {
        let (demo, session) = try await makeDemo()
        let line = try XCTUnwrap(demo.calls.availableLines.first { $0.backendID == session.id })
        await demo.calls.dial(number: "+12025550101", line: line)
        let record = try XCTUnwrap(demo.calls.primary?.record)
        async let first: Void = demo.stop()
        await demo.stop()
        XCTAssertFalse(FileManager.default.fileExists(atPath: demo.calls.recordings.directory.path))
        demo.calls.ingest(record, line: line)
        XCTAssertFalse(demo.calls.hasCall)
        await first
    }
}

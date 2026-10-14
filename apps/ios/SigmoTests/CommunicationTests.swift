import XCTest
@testable import Sigmo

@MainActor
final class CommunicationTests: XCTestCase {
    private func make(_ server: CommunicationFixture = CommunicationFixture(), name: String = "Backend", clock: BackendClock = BackendClock()) async throws -> BackendSession {
        let config = BackendConfiguration(name: name, address: try BackendAddress.normalize("https://\(name.lowercased()).example"))
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store, api: BackendAPI(configuration: config, transport: { try await server.respond($0) }), clock: clock)
        await session.refresh(force: true)
        return session
    }
    private func modem(_ id: String, profile: String = "active") throws -> Modem {
        let json = PreviewBackendServer.modemJSON(id: id).replacingOccurrences(of: "8986000000000000000", with: profile)
        return try JSONDecoder().decode(Modem.self, from: Data(json.utf8))
    }
    func testRepliesChooseOriginalOrSoleModem() throws {
        let a = try modem("a"), b = try modem("b")
        let cases: [([Modem], String, String)] = [([a], "", "a"), ([a, b], "", ""), ([a, b], "b", "b"), ([a, b], "removed", ""), ([a], "removed", "a")]
        for (devices, original, expected) in cases {
            XCTAssertEqual(CommunicationComposerState.choose(modems: devices, originalID: original), expected)
        }
    }
    func testBackendContactsAreIsolatedEvenWithTheSameIDs() async throws {
        let firstServer = CommunicationFixture(contactName: "First only")
        let first = try await make(firstServer, name: "First"), second = try await make(name: "Second")
        try await first.refreshContacts(); try await second.refreshContacts()
        XCTAssertEqual(second.contact(for: "+14155552671")?.name, "Alice")
        XCTAssertEqual(first.contact(for: "+14155552671")?.name, "First only")
        await firstServer.setContacts(first.savedContacts + [SigmoContact(id: "duplicate", name: "Someone else", numbers: [ContactPhone(number: "+14155552671", key: "+14155552671")])])
        try await first.refreshContacts(force: true)
        XCTAssertNil(first.contact(for: "+14155552671"))
        XCTAssertNil(second.contact(for: "5552671"))
        await first.logout()
        XCTAssertTrue(first.savedContacts.isEmpty)
        XCTAssertEqual(second.savedContacts.count, 1)
    }
    func testManualModemSurvivesRecipientAndSIMChanges() async throws {
        let server = CommunicationFixture()
        await server.setModems([try modem("a"), try modem("b")])
        let session = try await make(server)
        let state = CommunicationComposerState(session: session, action: CommunicationAction(number: "10086", preferredModemID: "removed"))
        await state.load()
        XCTAssertTrue(state.selectedID.isEmpty)
        state.selectModem("b")
        state.number = "10010"
        await state.load()
        XCTAssertEqual(state.selectedID, "b")
        await server.setModems([try modem("a"), try modem("b", profile: "replacement")])
        state.text = "Hello"
        let sent = await state.send()
        XCTAssertTrue(sent)
        let requests = await server.allRequests()
        let request = try XCTUnwrap(requests.last { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/modems/b/messages") == true })
        let body = try JSONSerialization.jsonObject(with: XCTUnwrap(request.httpBody)) as? [String: String]
        XCTAssertEqual(body?["expectedProfileId"], "replacement")
        XCTAssertFalse(requests.contains { $0.url?.path.contains("/esims") == true || $0.url?.path.hasSuffix("/sim-profiles") == true })
    }
    func testLateContactReadCannotRestoreLoggedOutData() async throws {
        let server = CommunicationFixture(), session = try await make()
        // Use a dedicated session with an independently held response.
        let config = session.configuration
        let heldSession = BackendSession(configuration: config, credentials: MemoryCredentials().store, api: BackendAPI(configuration: config, transport: { try await server.respond($0) }))
        await heldSession.refresh(force: true)
        await server.holdNextRead()
        let task = Task { try? await heldSession.refreshContacts() }
        await server.waitForRead()
        await heldSession.logout(); await server.releaseRead(); await task.value
        XCTAssertTrue(heldSession.savedContacts.isEmpty)
    }

    func testDeviceActionsStayOnCurrentModemAfterSwap() async throws {
        let server = CommunicationFixture()
        await server.setModems([try modem("a"), try modem("b")])
        let session = try await make(server)
        for calling in [false, true] {
            let state = CommunicationComposerState(session: session, action: CommunicationAction(number: "10086", calling: calling, modemID: "a"))
            await state.load()
            XCTAssertEqual(state.availableModems.map(\.id), ["a"])
            XCTAssertEqual(state.selectedID, "a")
            await server.setModems([try modem("a", profile: "replacement"), try modem("b")])
            let current = await state.currentModem()
            XCTAssertEqual(current?.sim.identifier, "replacement")
        }
    }

    func testMissingDeviceDoesNotFallBackToAnotherLine() async throws {
        let server = CommunicationFixture(), session = try await make(server)
        for calling in [false, true] {
            let state = CommunicationComposerState(session: session, action: CommunicationAction(number: "+14155552671", calling: calling, modemID: "removed"))
            await state.load()
            XCTAssertNil(state.selected)
            XCTAssertTrue(state.unavailable)
            XCTAssertFalse(state.canSubmit)
        }
    }
    func testConcurrentReadersShareContactAndDirectoryRequests() async throws {
        let server = CommunicationFixture(), session = try await make(server)
        await server.holdNextRead()
        let first = Task { try await session.refreshContacts() }
        await server.waitForRead()
        let second = Task { try await session.refreshContacts() }
        await server.releaseRead()
        try await first.value
        try await second.value
        try await session.refreshContacts()
        async let firstLines = session.communicationLines()
        async let secondLines = session.communicationLines()
        _ = try await (firstLines, secondLines)
        var requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/contacts") == true }.count, 1)
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/sim-profiles") == true }.count, 1)
        XCTAssertTrue(requests.filter { $0.url?.path.hasSuffix("/sim-profiles") == true }.allSatisfy { $0.url?.query == "cached=true" })
        session.invalidateCommunicationLines()
        _ = try await session.communicationLines()
        try await session.refreshContacts(force: true)
        requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/contacts") == true }.count, 2)
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/sim-profiles") == true }.count, 2)
    }

    func testContactCRUDAndNameOnlyEntries() async throws {
        let server = CommunicationFixture(), session = try await make(server)
        let nameOnly = SigmoContact(name: "Name only", notes: "Note")
        let saved = try await session.protectedRequest { api, token in try await api.saveContact(nameOnly, token: token) }
        XCTAssertFalse(saved.id.isEmpty); XCTAssertTrue(saved.numbers.isEmpty)
        try await session.refreshContacts(force: true); XCTAssertEqual(session.savedContacts.count, 2)
        try await session.protectedRequest { api, token in try await api.deleteContact(saved.id, token: token) }
        try await session.refreshContacts(force: true); XCTAssertEqual(session.savedContacts.count, 1)
        let requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/contacts") == true }.count, 1)
    }

    func testCommunicationCachesExpireUsingSessionClock() async throws {
        let server = CommunicationFixture(), time = TestTime()
        let session = try await make(server, clock: time.clock)
        try await session.refreshContacts()
        _ = try await session.communicationLines()
        time.advance(14)
        try await session.refreshContacts()
        _ = try await session.communicationLines()
        var requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/contacts") == true }.count, 1)
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/sim-profiles") == true }.count, 1)
        time.advance(2)
        try await session.refreshContacts()
        _ = try await session.communicationLines()
        requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/contacts") == true }.count, 2)
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/sim-profiles") == true }.count, 2)
    }

    func testCapabilityRemovalInvalidatesPendingContacts() async throws {
        let server = CommunicationFixture(), session = try await make(server)
        try await session.refreshContacts()
        await server.holdNextRead()
        let oldRead = Task { try? await session.refreshContacts(force: true) }
        await server.waitForRead()
        await server.setCapabilities([])
        await session.refresh(force: true)
        XCTAssertTrue(session.savedContacts.isEmpty)
        await server.releaseRead()
        await oldRead.value
        XCTAssertTrue(session.savedContacts.isEmpty)
        await server.setCapabilities(["contacts"])
        await session.refresh(force: true)
        try await session.refreshContacts()
        XCTAssertEqual(session.savedContacts.count, 1)
    }

    func testInvalidatedDirectoryRejectsLateResponse() async throws {
        let server = CommunicationFixture(), session = try await make(server)
        await server.holdNextRead("/sim-profiles")
        let oldRead = Task { try? await session.communicationLines() }
        await server.waitForRead()
        session.invalidateCommunicationLines()
        await server.setLines([CommunicationFixture.line("replacement")])
        let fresh = try await session.communicationLines()
        XCTAssertEqual(fresh.map(\.profileId), ["replacement"])
        await server.releaseRead()
        let stale = await oldRead.value
        XCTAssertNil(stale)
        let cached = try await session.communicationLines()
        XCTAssertEqual(cached.map(\.profileId), ["replacement"])
    }

    func testHistoryRemainsVisibleWhenDirectoryIsUnavailable() async throws {
        let server = CommunicationFixture(includesHistory: true), session = try await make(server)
        await server.setDirectoryUnavailable(true)
        let messages = CommunicationState(session: session, section: .messages)
        await messages.load()
        XCTAssertEqual(messages.threads.count, 1)
        XCTAssertNil(messages.failure)
        let calls = CommunicationState(session: session, section: .calls)
        await calls.load()
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertNil(calls.failure)
    }
}

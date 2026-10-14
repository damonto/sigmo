import XCTest
@testable import Sigmo

@MainActor
final class CommunicationOverviewTests: XCTestCase {
    private func session(_ name: String, server: OverviewHistoryFixture) async throws -> BackendSession {
        let configuration = BackendConfiguration(name: name, address: try BackendAddress.normalize("https://\(name.lowercased()).example"))
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        return session
    }

    func testContactsWithIdenticalIDsRemainSeparateAndDeleteOnlyTheirSource() async throws {
        let firstServer = OverviewHistoryFixture(name: "First"), secondServer = OverviewHistoryFixture(name: "Second")
        let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
        let state = CommunicationOverviewState(sessions: [second, first], section: .contacts)
        await state.load()
        XCTAssertEqual(state.contacts.map(\.value.name), ["First", "Second"])
        XCTAssertEqual(state.contacts.map(\.value.id), ["alice", "alice"])
        XCTAssertEqual(Set(state.contacts.map(\.id)).count, 2)
        XCTAssertEqual(state.creationSessions.map(\.id), [second.id, first.id])

        let target = try XCTUnwrap(state.contacts.first)
        await state.delete(target.id)
        XCTAssertEqual(state.contacts.map(\.session.id), [second.id])
        let secondRequests = await secondServer.allRequests()
        XCTAssertFalse(secondRequests.contains { $0.httpMethod == "DELETE" })
        state.search = "missing"
        await state.load()
        XCTAssertTrue(state.contacts.isEmpty)
        state.search = "Second"
        await state.load()
        XCTAssertEqual(state.contacts.map(\.session.id), [second.id])
    }

    func testHistoryMergesChronologicallyWithIndependentPaginationAndFilters() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let firstServer = OverviewHistoryFixture(name: "First", days: [3, 1])
            let secondServer = OverviewHistoryFixture(name: "Second", days: [4])
            let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
            let state = CommunicationOverviewState(sessions: [first, second], section: section)
            await state.load()
            if section == .messages {
                XCTAssertEqual(state.threads.map(\.session.id), [second.id, first.id])
                XCTAssertEqual(Set(state.threads.map(\.id)).count, 2)
            } else {
                XCTAssertEqual(state.calls.map(\.session.id), [second.id, first.id])
                XCTAssertEqual(Set(state.calls.map(\.id)).count, 2)
            }
            XCTAssertTrue(state.hasMore)
            await state.load(more: true)
            XCTAssertFalse(state.hasMore)
            let requests = await secondServer.allRequests().filter { $0.url?.path.hasSuffix(section == .messages ? "/message-threads" : "/calls") == true }
            XCTAssertEqual(requests.count, 1, "An exhausted backend must not reload its first page.")
            await state.load(automatic: true)
            let expandedRequests = await firstServer.historyRequests()
            XCTAssertEqual(expandedRequests.count, 2, "Automatic refresh must preserve appended history pages.")
            if section == .messages {
                XCTAssertEqual(state.threads.map(\.session.id), [second.id, first.id, first.id])
                state.unreadOnly = true
                XCTAssertEqual(state.threads.map(\.value.latestMessage.timestamp), ["2026-10-03T00:00:00Z", "2026-10-01T00:00:00Z"])
            } else {
                XCTAssertEqual(state.calls.map(\.session.id), [second.id, first.id, first.id])
                state.missedOnly = true
                await state.load()
                XCTAssertEqual(state.calls.map(\.session.id), [first.id])
                XCTAssertTrue(state.calls.allSatisfy { $0.value.missed })
            }
        }
    }

    func testUnavailableBackendDoesNotHideOtherHistoryAndDeletionStaysScoped() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let firstServer = OverviewHistoryFixture(name: "First", days: [3])
            let secondServer = OverviewHistoryFixture(name: "Second", days: [4])
            let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
            await secondServer.setUnavailable(true)
            let state = CommunicationOverviewState(sessions: [first, second], section: section)
            await state.load()
            XCTAssertNotNil(state.source(for: second.id)?.failure)
            XCTAssertNil(state.source(for: first.id)?.failure)
            XCTAssertEqual(section == .messages ? state.threads.count : state.calls.count, 1)
            await secondServer.setUnavailable(false)
            await state.load()
            XCTAssertEqual(section == .messages ? state.threads.count : state.calls.count, 2)
            await state.delete(CommunicationRecordID(backendID: first.id, recordID: "record-0"))
            XCTAssertEqual(section == .messages ? state.threads.map(\.session.id) : state.calls.map(\.session.id), [second.id])
            let secondRequests = await secondServer.allRequests()
            XCTAssertFalse(secondRequests.contains { $0.httpMethod == "DELETE" })
        }
    }

    func testSlowBackendDoesNotDelayContactsFromAnotherBackend() async throws {
        let firstServer = OverviewHistoryFixture(name: "First"), secondServer = OverviewHistoryFixture(name: "Second")
        let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
        let state = CommunicationOverviewState(sessions: [first, second], section: .contacts)
        await firstServer.holdContacts()
        let pending = Task { await state.load() }
        await firstServer.waitForContactRead()
        try await second.refreshContacts()
        XCTAssertEqual(state.contacts.map(\.session.id), [second.id])
        XCTAssertTrue(state.loading)
        await firstServer.releaseContacts()
        await pending.value
        XCTAssertEqual(state.contacts.count, 2)
    }

    func testRevokedBackendCannotRestoreItsLateContactsInAll() async throws {
        let firstServer = OverviewHistoryFixture(name: "First"), secondServer = OverviewHistoryFixture(name: "Second")
        let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
        let state = CommunicationOverviewState(sessions: [first, second], section: .contacts)
        await state.load()
        await firstServer.holdContacts()
        let pending = Task { await state.load(force: true) }
        await firstServer.waitForContactRead()
        await first.logout()
        XCTAssertEqual(state.contacts.map(\.session.id), [second.id])
        XCTAssertEqual(state.creationSessions.map(\.id), [second.id])
        await firstServer.releaseContacts()
        await pending.value
        XCTAssertEqual(state.contacts.map(\.session.id), [second.id])
    }

    func testSearchChangeDiscardsCursorsBeforeTheNextLoad() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = OverviewHistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationOverviewState(sessions: [session], section: section)
            await state.load()
            XCTAssertTrue(state.hasMore)
            XCTAssertFalse(state.isEmpty)

            state.search = "no-match"
            XCTAssertTrue(state.isEmpty)
            XCTAssertFalse(state.hasMore)
            await state.load(more: true)
            let beforeReload = await server.historyRequests()
            XCTAssertEqual(beforeReload.count, 1, "Load more cannot use a previous search's cursor.")

            await state.load()
            XCTAssertTrue(state.isEmpty)
            XCTAssertFalse(state.hasMore)
            let requests = await server.historyRequests()
            let last = try XCTUnwrap(requests.last?.url)
            let query = URLComponents(url: last, resolvingAgainstBaseURL: false)?.queryItems ?? []
            XCTAssertEqual(query.first { $0.name == "q" }?.value, "no-match")
            XCTAssertFalse(query.contains { $0.name == "cursor" && $0.value?.isEmpty == false })
        }
    }

    func testLateHistoryPageCannotRestorePreviousSearch() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = OverviewHistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationOverviewState(sessions: [session], section: section)
            await state.load()
            await server.holdNextHistoryRead()
            let pending = Task { await state.load(more: true) }
            await server.waitForHistoryRead()

            state.search = "no-match"
            await server.releaseHistoryRead()
            await pending.value
            XCTAssertTrue(state.isEmpty, "The old page must be rejected even before the new search starts.")
            XCTAssertFalse(state.hasMore)
            await state.load()
            XCTAssertTrue(state.isEmpty)
            XCTAssertNil(state.sources.first?.failure)
        }
    }

    func testConcurrentLoadMoreDoesNotAppendTheSamePageTwice() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = OverviewHistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationOverviewState(sessions: [session], section: section)
            await state.load()
            await server.holdNextHistoryRead()
            let pending = Task { await state.load(more: true) }
            await server.waitForHistoryRead()
            await state.load(more: true)
            await server.releaseHistoryRead()
            await pending.value

            let requests = await server.historyRequests()
            XCTAssertEqual(requests.count, 2)
            let ids = section == .messages ? state.threads.map(\.id) : state.calls.map(\.id)
            XCTAssertEqual(ids.count, 2)
            XCTAssertEqual(Set(ids).count, 2)
            XCTAssertFalse(state.hasMore)
        }
    }

    func testFailedLoadMoreDoesNotDisableAutomaticRefresh() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = OverviewHistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationOverviewState(sessions: [session], section: section)
            await state.load()
            await server.setUnavailable(true)
            await state.load(more: true)
            XCTAssertNotNil(state.sources.first?.failure)
            XCTAssertFalse(state.isEmpty)

            await server.setUnavailable(false)
            await state.load(automatic: true)
            XCTAssertNil(state.sources.first?.failure)
            let requests = await server.historyRequests()
            XCTAssertEqual(requests.count, 3)
            XCTAssertTrue(state.hasMore)
        }
    }
}

private actor OverviewHistoryFixture {
    private let contacts: CommunicationFixture
    private let name: String
    private let days: [Int]
    private var removed: Set<String> = []
    private var unavailable = false
    private var requests: [URLRequest] = []
    private var holdsHistory = false
    private var heldHistory: CheckedContinuation<Void, Never>?
    private var historyArrival: CheckedContinuation<Void, Never>?

    init(name: String, days: [Int] = []) {
        self.name = name
        self.days = days
        contacts = CommunicationFixture(contactName: name)
    }
    func allRequests() -> [URLRequest] { requests }
    func historyRequests() -> [URLRequest] {
        requests.filter { $0.url?.path.hasSuffix("/message-threads") == true || $0.url?.path.hasSuffix("/calls") == true }
    }
    func holdNextHistoryRead() { holdsHistory = true }
    func waitForHistoryRead() async {
        if heldHistory == nil { await withCheckedContinuation { historyArrival = $0 } }
    }
    func releaseHistoryRead() { heldHistory?.resume(); heldHistory = nil }
    func setUnavailable(_ value: Bool) { unavailable = value }
    func holdContacts() async { await contacts.holdNextRead() }
    func waitForContactRead() async { await contacts.waitForRead() }
    func releaseContacts() async { await contacts.releaseRead() }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        guard let url = request.url else { throw URLError(.badURL) }
        let path = url.path
        if path.contains("/message-threads/") || path.contains("/calls/"), request.httpMethod == "DELETE" {
            removed.insert(url.lastPathComponent)
            return BackendHTTPResponse(data: Data(), status: 204)
        }
        guard path.hasSuffix("/message-threads") || path.hasSuffix("/calls") else {
            return try await contacts.respond(request)
        }
        if unavailable { throw URLError(.cannotConnectToHost) }
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let cursor = query.first { $0.name == "cursor" }?.value ?? ""
        let missed = query.first { $0.name == "missed" }?.value == "true"
        let search = query.first { $0.name == "q" }?.value ?? ""
        var offset = 0
        if !cursor.isEmpty {
            guard cursor.hasPrefix("\(name)-"), let parsed = Int(cursor.dropFirst(name.count + 1)) else { throw URLError(.badServerResponse) }
            offset = parsed
        }
        let items = days.enumerated().filter {
            !removed.contains("record-\($0.offset)") && (!missed || $0.element % 2 == 1)
                && (search.isEmpty || "+14155552671".contains(search))
        }
        let page = items.dropFirst(offset).prefix(1).map { index, day in
            let date = String(format: "2026-10-%02dT00:00:00Z", day)
            if path.hasSuffix("/calls") {
                return #"{"callID":"record-\#(index)","profileId":"a","historyProfileId":"a","modemId":"device-1","number":"+14155552671","route":"wifi_calling","direction":"incoming","state":"ended","reason":"normal","startedAt":"\#(date)","answeredAt":"\#(day % 2 == 1 ? "" : date)","endedAt":"\#(date)","updatedAt":"\#(date)"}"#
            }
            return #"{"id":"record-\#(index)","participant":"+14155552671","unreadCount":\#(day % 2),"markedUnread":false,"latestMessage":{"id":\#(index),"conversationId":"record-\#(index)","profileId":"a","historyProfileId":"a","modemId":"device-1","sender":"+14155552671","recipient":"+14155550000","text":"Fixture","timestamp":"\#(date)","incoming":true,"read":false,"status":"received","lineLabel":"Work","lineNumber":"+14155550000"}}"#
        }
        let next = offset + 1 < items.count ? "\(name)-\(offset + 1)" : ""
        let body = #"{"items":[\#(page.joined(separator: ","))],"nextCursor":"\#(next)"}"#
        if holdsHistory {
            holdsHistory = false
            await withCheckedContinuation {
                heldHistory = $0
                historyArrival?.resume()
                historyArrival = nil
            }
        }
        return BackendHTTPResponse(data: Data(body.utf8), status: 200)
    }
}

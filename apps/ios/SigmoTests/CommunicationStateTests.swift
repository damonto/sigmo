import XCTest
@testable import Sigmo

@MainActor
final class CommunicationStateTests: XCTestCase {
    private func session(_ name: String, server: HistoryFixture) async throws -> BackendSession {
        let configuration = BackendConfiguration(name: name, address: try BackendAddress.normalize("https://\(name.lowercased()).example"))
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        return session
    }

    func testContactsWithIdenticalIDsStayOnTheirBackend() async throws {
        let firstServer = HistoryFixture(name: "First"), secondServer = HistoryFixture(name: "Second")
        let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
        let state = CommunicationState(session: first, section: .contacts)
        await state.load()
        XCTAssertEqual(state.contacts.map(\.name), ["First"])
        state.search = "Second"
        XCTAssertTrue(state.contacts.isEmpty)
        state.search = "First"
        XCTAssertEqual(state.contacts.map(\.id), ["alice"])
        await state.delete("alice")
        XCTAssertTrue(state.contacts.isEmpty)
        let secondRequests = await secondServer.allRequests()
        XCTAssertFalse(secondRequests.contains { $0.url?.path.contains("/contacts") == true })

        let other = CommunicationState(session: second, section: .contacts)
        await other.load()
        XCTAssertEqual(other.contacts.map(\.name), ["Second"])
        XCTAssertEqual(other.contacts.map(\.id), ["alice"])
    }

    func testCallsAreAvailableWithoutLicensedFeatures() async throws {
        let server = HistoryFixture(name: "Backend", days: [3])
        let session = try await session("Backend", server: server)
        let state = CommunicationState(session: session, section: .calls)
        XCTAssertEqual(session.capabilities, [])
        await state.load()
        XCTAssertEqual(state.calls.count, 1)
    }

    func testHistoryPaginationAndFiltersUseTheSelectedBackend() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let firstServer = HistoryFixture(name: "First", days: [4, 3])
            let secondServer = HistoryFixture(name: "Second", days: [2])
            let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
            let state = CommunicationState(session: first, section: section)
            await state.load()
            XCTAssertEqual(state.cursor, "First-1")
            await state.load(more: true)
            XCTAssertTrue(state.cursor.isEmpty)
            await state.load(automatic: true)
            let expandedRequests = await firstServer.historyRequests()
            XCTAssertEqual(expandedRequests.count, 2, "Automatic refresh must preserve appended history pages.")
            let otherRequests = await secondServer.historyRequests()
            XCTAssertTrue(otherRequests.isEmpty)
            if section == .messages {
                XCTAssertEqual(state.threads.map(\.id), ["record-0", "record-1"])
                state.unreadOnly = true
                XCTAssertEqual(state.filteredThreads.map(\.id), ["record-1"])
            } else {
                XCTAssertEqual(state.calls.map(\.id), ["record-0", "record-1"])
                state.missedOnly = true
                await state.load()
                XCTAssertEqual(state.filteredCalls.map(\.id), ["record-1"])
            }

            let other = CommunicationState(session: second, section: section)
            await other.load()
            XCTAssertEqual(section == .messages ? other.threads.map(\.id) : other.calls.map(\.id), ["record-0"])
            XCTAssertTrue(other.cursor.isEmpty)
        }
    }

    func testDeletingHistoryWithIdenticalIDsOnlyChangesItsBackend() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let firstServer = HistoryFixture(name: "First", days: [3])
            let secondServer = HistoryFixture(name: "Second", days: [4])
            let first = try await session("First", server: firstServer), second = try await session("Second", server: secondServer)
            let state = CommunicationState(session: first, section: section)
            await state.load()
            XCTAssertFalse(state.isEmpty)
            let deletionFailure = await state.delete("record-0")
            XCTAssertTrue(state.isEmpty)
            XCTAssertNil(deletionFailure)
            let other = CommunicationState(session: second, section: section)
            await other.load()
            XCTAssertFalse(other.isEmpty)
            let secondRequests = await secondServer.allRequests()
            XCTAssertFalse(secondRequests.contains { $0.httpMethod == "DELETE" })
        }
    }

    func testSuccessfulDeletionRemainsVisibleWhenHistoryRefreshFails() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await state.load()
            XCTAssertFalse(state.isEmpty)

            await server.setUnavailable(true)
            let deletionFailure = await state.delete("record-0")
            XCTAssertTrue(state.isEmpty)
            XCTAssertNil(deletionFailure)
            XCTAssertNotNil(state.failure)
        }
    }

    func testLateHistoryFromPreviousBackendDoesNotAffectCurrentState() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let firstServer = HistoryFixture(name: "First", days: [3, 2])
            let first = try await session("First", server: firstServer)
            let previous = CommunicationState(session: first, section: section)
            await firstServer.holdNextHistoryRead()
            let pending = Task { await previous.load() }
            await firstServer.waitForHistoryRead()
            pending.cancel()

            let secondServer = HistoryFixture(name: "Second", days: [4])
            let second = try await session("Second", server: secondServer)
            let current = CommunicationState(session: second, section: section)
            await current.load()
            await firstServer.releaseHistoryRead()
            await pending.value
            XCTAssertTrue(previous.isEmpty)
            XCTAssertTrue(current.cursor.isEmpty)
            let date = section == .messages ? current.threads.first?.latestMessage.timestamp : current.calls.first?.startedAt
            XCTAssertEqual(date, "2026-10-04T00:00:00Z")
        }
    }

    func testRevokedAccessCannotRestoreLateContacts() async throws {
        let server = HistoryFixture(name: "First")
        let session = try await session("First", server: server)
        let state = CommunicationState(session: session, section: .contacts)
        await state.load()
        await server.holdContacts()
        let pending = Task { await state.load(force: true) }
        await server.waitForContactRead()
        await session.logout()
        XCTAssertTrue(state.contacts.isEmpty)
        await server.releaseContacts()
        await pending.value
        XCTAssertTrue(state.contacts.isEmpty)
    }

    func testSearchChangeDiscardsCursorsBeforeTheNextLoad() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await state.load()
            XCTAssertFalse(state.cursor.isEmpty)
            XCTAssertFalse(state.isEmpty)

            state.search = "no-match"
            XCTAssertTrue(state.isEmpty)
            XCTAssertTrue(state.cursor.isEmpty)
            await state.load(more: true)
            let beforeReload = await server.historyRequests()
            XCTAssertEqual(beforeReload.count, 1, "Load more cannot use a previous search's cursor.")

            await state.load()
            XCTAssertTrue(state.isEmpty)
            XCTAssertTrue(state.cursor.isEmpty)
            let requests = await server.historyRequests()
            let last = try XCTUnwrap(requests.last?.url)
            let query = URLComponents(url: last, resolvingAgainstBaseURL: false)?.queryItems ?? []
            XCTAssertEqual(query.first { $0.name == "q" }?.value, "no-match")
            XCTAssertFalse(query.contains { $0.name == "cursor" && $0.value?.isEmpty == false })
        }
    }

    func testLateHistoryPageCannotRestorePreviousSearch() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await state.load()
            await server.holdNextHistoryRead()
            let pending = Task { await state.load(more: true) }
            await server.waitForHistoryRead()

            state.search = "no-match"
            await server.releaseHistoryRead()
            await pending.value
            XCTAssertTrue(state.isEmpty, "The old page must be rejected even before the new search starts.")
            XCTAssertTrue(state.cursor.isEmpty)
            await state.load()
            XCTAssertTrue(state.isEmpty)
            XCTAssertNil(state.failure)
        }
    }

    func testPendingPageIgnoresDuplicateLoadsAndAutomaticRefresh() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await state.load()
            await server.holdNextHistoryRead()
            let pending = Task { await state.load(more: true) }
            await server.waitForHistoryRead()
            await state.load(more: true)
            await state.load(automatic: true)
            await server.releaseHistoryRead()
            await pending.value

            let requests = await server.historyRequests()
            XCTAssertEqual(requests.count, 2)
            let ids = section == .messages ? state.threads.map(\.id) : state.calls.map(\.id)
            XCTAssertEqual(ids.count, 2)
            XCTAssertEqual(Set(ids).count, 2)
            XCTAssertTrue(state.cursor.isEmpty)
        }
    }

    func testFailedLoadMoreDoesNotDisableAutomaticRefresh() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4, 3])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await state.load()
            await server.setUnavailable(true)
            await state.load(more: true)
            XCTAssertNotNil(state.failure)
            XCTAssertFalse(state.isEmpty)

            await server.setUnavailable(false)
            await state.load(automatic: true)
            XCTAssertNil(state.failure)
            let requests = await server.historyRequests()
            XCTAssertEqual(requests.count, 3)
            XCTAssertFalse(state.cursor.isEmpty)
        }
    }

    func testSupersededRefreshDoesNotReturnNewerLoadFailure() async throws {
        let server = HistoryFixture(name: "First", days: [4])
        let session = try await session("First", server: server)
        let state = CommunicationState(session: session, section: .messages)
        await server.holdNextHistoryRead()
        let refresh = Task { await state.load(force: true) }
        await server.waitForHistoryRead()

        await server.setUnavailable(true)
        let failure = await state.load()
        await server.releaseHistoryRead()
        let refreshFailure = await refresh.value

        XCTAssertNotNil(failure)
        XCTAssertNil(refreshFailure)
        XCTAssertEqual(state.failure, failure)
    }

    func testBackgroundPausesAutomaticRefreshWithoutDiscardingPendingRead() async throws {
        for section in [CommunicationSection.messages, .calls] {
            let server = HistoryFixture(name: "First", days: [4])
            let session = try await session("First", server: server)
            let state = CommunicationState(session: session, section: section)
            await server.holdNextHistoryRead()
            let pending = Task { await state.load(automatic: true) }
            await server.waitForHistoryRead()
            session.foreground = false
            await server.releaseHistoryRead()
            await pending.value
            XCTAssertFalse(state.isEmpty)
            await state.load(automatic: true)
            let backgroundRequests = await server.historyRequests()
            XCTAssertEqual(backgroundRequests.count, 1)

            session.foreground = true
            await state.load(automatic: true)
            let foregroundRequests = await server.historyRequests()
            XCTAssertEqual(foregroundRequests.count, 2)
        }
    }
}

private actor HistoryFixture {
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

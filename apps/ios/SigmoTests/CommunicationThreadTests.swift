import XCTest
@testable import Sigmo

@MainActor
final class CommunicationThreadTests: XCTestCase {
    private func make(_ server: ThreadServer) async throws -> CommunicationThreadState {
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        return CommunicationThreadState(session: session, threadID: "thread")
    }

    func testPollAfterInitialFailureEstablishesPaginationAndClearsError() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setResponse(HTTPProblem.response(status: 503, code: "communication_failed", detail: "Offline"))
        await state.load()
        XCTAssertFalse(state.loaded)
        XCTAssertNotNil(state.failure)

        await server.setPage([message(2)], cursor: "older")
        await state.load(preserve: true)
        XCTAssertTrue(state.loaded)
        XCTAssertEqual(state.cursor, "older")
        XCTAssertNil(state.failure)

        await server.setPage([message(1, timestamp: "2026-10-13T07:59:59Z")])
        await state.load(more: true)
        XCTAssertEqual(state.messages.map(\.id), [1, 2])
        XCTAssertTrue(state.cursor.isEmpty)
        let requests = await server.messageRequests()
        XCTAssertEqual(URLComponents(url: try XCTUnwrap(requests.last?.url), resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "cursor" }?.value, "older")
    }

    func testPollingMergesChronologicallyAndRebasesChangedSnapshots() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage([message(2)], cursor: "older-snapshot")
        await state.load()
        await server.setPage([message(2), message(3, timestamp: "2026-10-13T08:00:00.100Z"),
                              message(4, timestamp: "2026-10-13T09:00:00+01:00")], cursor: "new-snapshot")
        await state.load(preserve: true)
        XCTAssertEqual(state.messages.map(\.id), [2, 4, 3])
        XCTAssertEqual(state.cursor, "new-snapshot")

        await server.setPage([message(1, timestamp: "2026-10-13T07:59:59Z"), message(2)])
        await state.load(more: true)
        XCTAssertEqual(state.messages.map(\.id), [1, 2, 4, 3])
        XCTAssertTrue(state.cursor.isEmpty)

        await server.setPage([message(3, timestamp: "2026-10-13T08:00:00.100Z")], cursor: "refreshed")
        await state.load()
        XCTAssertEqual(state.messages.map(\.id), [3])
        XCTAssertEqual(state.cursor, "refreshed")
    }

    func testLateReadSnapshotCannotUndoAcknowledgmentOrRepeatIt() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage([message(1)])
        await state.load()
        let unread = try XCTUnwrap(state.messages.first)
        await server.holdNextRead()
        let poll = Task { await state.load(preserve: true) }
        await server.waitForRead()
        let badge = await state.acknowledge(unread)
        XCTAssertEqual(badge?.revision, 2)
        await server.releaseRead()
        await poll.value
        XCTAssertEqual(state.messages.first?.read, true)
        let duplicate = await state.acknowledge(unread)
        XCTAssertNil(duplicate)
        let writes = await server.acknowledgments()
        XCTAssertEqual(writes.count, 1)
        let body = try JSONDecoder().decode([String: [Int64]].self, from: XCTUnwrap(writes.first?.httpBody))
        XCTAssertEqual(body["messageIds"], [1])
    }

    func testPollBridgesMoreThanOnePageOfNewMessages() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage((51...100).map { message(Int64($0)) }, cursor: "old-50")
        await state.load()
        await server.setPage((111...160).map { message(Int64($0)) }, cursor: "new-110")
        await server.setPage((61...110).map { message(Int64($0)) }, cursor: "new-60", forCursor: "new-110")
        await server.setPage((11...60).map { message(Int64($0)) }, cursor: "new-10", forCursor: "new-60")
        await server.setPage((1...10).map { message(Int64($0)) }, forCursor: "new-10")
        await state.poll()
        XCTAssertEqual(state.messages.map(\.id), (51...160).map(Int64.init))
        XCTAssertEqual(state.cursor, "new-60")
        await state.load(more: true)
        await state.load(more: true)
        XCTAssertEqual(state.messages.map(\.id), (1...160).map(Int64.init))
        XCTAssertTrue(state.cursor.isEmpty)
    }

    func testPollKeepsBackdatedImportsReachableAndSkipsRetainedPages() async throws {
        let server = ThreadServer(), state = try await make(server)
        func item(_ id: Int64) -> String { message(id, timestamp: "2026-10-13T08:00:0\(id)Z") }
        await server.setPage([item(5), item(6)], cursor: "old-a")
        await server.setPage([item(3), item(4)], cursor: "old-b", forCursor: "old-a")
        await state.load()
        await state.load(more: true)
        await state.poll()
        XCTAssertEqual(state.cursor, "old-b")

        await server.setPage([item(5), item(6)], cursor: "new-a")
        await server.setPage([item(3), item(4)], cursor: "new-b", forCursor: "new-a")
        await server.setPage([item(1), item(2), message(7, timestamp: "2026-10-13T08:00:02.500Z")], forCursor: "new-b")
        await state.poll()
        XCTAssertEqual(state.cursor, "new-a")
        let before = await server.messageRequests().count
        await state.load(more: true)
        XCTAssertNil(state.failure)
        XCTAssertEqual(state.messages.map(\.id), [1, 2, 7, 3, 4, 5, 6])
        XCTAssertTrue(state.cursor.isEmpty)
        let after = await server.messageRequests().count
        XCTAssertEqual(after - before, 2)
        await state.poll()
        XCTAssertTrue(state.cursor.isEmpty)
    }

    func testMoreRejectsPagesThatChangeCursorWithoutAdvancingMessages() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage([message(1)], cursor: "old")
        await state.load()
        await server.setPage([message(1)], cursor: "new")
        await server.setPage([message(1)], cursor: "repeat", forCursor: "new")
        await server.setPage([message(1)], cursor: "again", forCursor: "repeat")
        await state.poll()
        await state.load(more: true)
        XCTAssertEqual(state.failure?.code, "api.invalidResponse")
        XCTAssertEqual(state.messages.map(\.id), [1])
    }

    func testVisibleMessagesAreAcknowledgedInOneBatch() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage((1...30).map { message(Int64($0)) })
        await state.load()
        let badge = await state.acknowledge(Set((1...30).map(Int64.init)))
        XCTAssertEqual(badge?.revision, 2)
        XCTAssertTrue(state.messages.allSatisfy(\.read))
        let duplicate = await state.acknowledge(Set((1...30).map(Int64.init)))
        XCTAssertNil(duplicate)
        let requests = await server.acknowledgments()
        XCTAssertEqual(requests.count, 1)
        let body = try JSONDecoder().decode([String: [Int64]].self, from: XCTUnwrap(requests.first?.httpBody))
        XCTAssertEqual(body["messageIds"], (1...30).map(Int64.init))
    }

    func testScrollingDoesNotCancelAnAcceptedReadBatchOrSkipOverlappingIDs() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage((1...3).map { message(Int64($0)) })
        await state.load()
        await server.holdNextAcknowledgment()
        let first = Task { await state.acknowledge(Set([1, 2])) }
        await server.waitForRead()
        first.cancel()
        let second = Task { await state.acknowledge(Set([2, 3])) }
        await Task.yield()
        await server.releaseRead()
        _ = await first.value
        _ = await second.value
        XCTAssertTrue(state.messages.allSatisfy(\.read))
        let writes = await server.acknowledgments()
        let batches = try writes.map { try JSONDecoder().decode([String: [Int64]].self, from: XCTUnwrap($0.httpBody))["messageIds"] ?? [] }
        XCTAssertEqual(batches, [[1, 2], [3]])
    }

    func testCancellationAndLogoutDiscardLatePages() async throws {
        for logout in [false, true] {
            let server = ThreadServer(), state = try await make(server)
            await server.setPage([message(1)], cursor: "older")
            await server.holdNextRead()
            let load = Task { await state.load() }
            await server.waitForRead()
            if logout { await state.session.logout() } else { load.cancel() }
            await server.releaseRead()
            await load.value
            XCTAssertTrue(state.messages.isEmpty)
            XCTAssertTrue(state.cursor.isEmpty)
            XCTAssertFalse(state.loaded)
            XCTAssertFalse(state.loading)
        }
    }

    func testBackgroundPausesPollingWithoutDiscardingPendingRead() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage([message(1)], cursor: "older")
        await server.holdNextRead()
        let pending = Task { await state.poll() }
        await server.waitForRead()
        state.session.foreground = false
        await server.releaseRead()
        await pending.value
        XCTAssertEqual(state.messages.map(\.id), [1])
        await state.poll()
        let backgroundRequests = await server.messageRequests()
        XCTAssertEqual(backgroundRequests.count, 1)

        state.session.foreground = true
        await server.setPage([message(1), message(2)])
        await state.poll()
        XCTAssertEqual(state.messages.map(\.id), [1, 2])
        XCTAssertTrue(state.cursor.isEmpty)
        let foregroundRequests = await server.messageRequests()
        XCTAssertEqual(foregroundRequests.count, 2)
    }

    private func message(_ id: Int64, timestamp: String = "2026-10-13T08:00:00Z") -> String {
        #"{"id":\#(id),"conversationId":"conversation","profileId":"sim","historyProfileId":"sim","modemId":"device-1","sender":"10086","recipient":"10010","text":"Message","timestamp":"\#(timestamp)","status":"received","lineLabel":"Home","lineNumber":"10010","incoming":true,"read":false}"#
    }
}

private actor ThreadServer {
    private let base = CommunicationFixture()
    private var response = BackendHTTPResponse(data: Data(#"{"items":[],"nextCursor":""}"#.utf8), status: 200)
    private var pages: [String: BackendHTTPResponse] = [:]
    private var requests: [URLRequest] = []
    private var hold = false
    private var holdAcknowledgment = false
    private var held: CheckedContinuation<Void, Never>?
    private var arrived: CheckedContinuation<Void, Never>?

    func setResponse(_ value: BackendHTTPResponse) { response = value }
    func setPage(_ messages: [String], cursor: String = "", forCursor: String = "") {
        let page = BackendHTTPResponse(data: Data(#"{"items":[\#(messages.joined(separator: ","))],"nextCursor":"\#(cursor)"}"#.utf8), status: 200)
        if forCursor.isEmpty { response = page } else { pages[forCursor] = page }
    }
    func holdNextRead() { hold = true }
    func holdNextAcknowledgment() { holdAcknowledgment = true }
    func waitForRead() async { if held == nil { await withCheckedContinuation { arrived = $0 } } }
    func releaseRead() { held?.resume(); held = nil }
    func messageRequests() -> [URLRequest] { requests.filter { $0.url?.path.hasSuffix("/thread/messages") == true } }
    func acknowledgments() -> [URLRequest] { requests.filter { $0.httpMethod == "PATCH" } }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if request.url?.path.hasSuffix("/message-threads/thread/messages") == true {
            let cursor = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "cursor" }?.value ?? ""
            let snapshot = pages[cursor] ?? response
            if hold {
                hold = false
                await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
            }
            return snapshot
        }
        if request.httpMethod == "PATCH", request.url?.path.hasSuffix("/message-threads/thread") == true {
            if holdAcknowledgment {
                holdAcknowledgment = false
                await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
            }
            return BackendHTTPResponse(data: Data(#"{"backendId":"backend","revision":2,"unreadCount":0}"#.utf8), status: 200)
        }
        return try await base.respond(request)
    }
}

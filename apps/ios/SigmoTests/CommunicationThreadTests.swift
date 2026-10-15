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

    func testPollingMergesChronologicallyWithoutDuplicatesOrLosingOlderCursor() async throws {
        let server = ThreadServer(), state = try await make(server)
        await server.setPage([message(2)], cursor: "older-snapshot")
        await state.load()
        await server.setPage([message(2), message(3, timestamp: "2026-10-13T08:00:00.100Z"),
                              message(4, timestamp: "2026-10-13T09:00:00+01:00")], cursor: "new-snapshot")
        await state.load(preserve: true)
        XCTAssertEqual(state.messages.map(\.id), [2, 4, 3])
        XCTAssertEqual(state.cursor, "older-snapshot")

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
        await server.setPage([message(2)])
        await state.poll()
        XCTAssertEqual(state.messages.map(\.id), [1, 2])
        XCTAssertEqual(state.cursor, "older")
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
    private var requests: [URLRequest] = []
    private var hold = false
    private var held: CheckedContinuation<Void, Never>?
    private var arrived: CheckedContinuation<Void, Never>?

    func setResponse(_ value: BackendHTTPResponse) { response = value }
    func setPage(_ messages: [String], cursor: String = "") {
        response = BackendHTTPResponse(data: Data(#"{"items":[\#(messages.joined(separator: ","))],"nextCursor":"\#(cursor)"}"#.utf8), status: 200)
    }
    func holdNextRead() { hold = true }
    func waitForRead() async { if held == nil { await withCheckedContinuation { arrived = $0 } } }
    func releaseRead() { held?.resume(); held = nil }
    func messageRequests() -> [URLRequest] { requests.filter { $0.url?.path.hasSuffix("/thread/messages") == true } }
    func acknowledgments() -> [URLRequest] { requests.filter { $0.httpMethod == "PATCH" } }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if request.url?.path.hasSuffix("/message-threads/thread/messages") == true {
            let snapshot = response
            if hold {
                hold = false
                await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
            }
            return snapshot
        }
        if request.httpMethod == "PATCH", request.url?.path.hasSuffix("/message-threads/thread") == true {
            return BackendHTTPResponse(data: Data(#"{"backendId":"backend","revision":2,"unreadCount":0}"#.utf8), status: 200)
        }
        return try await base.respond(request)
    }
}

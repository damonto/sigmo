import XCTest
@testable import Sigmo

@MainActor
final class CommunicationComposerTests: XCTestCase {
    private func make(_ server: ComposerServer) async throws -> CommunicationComposerState {
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        let state = CommunicationComposerState(session: session, action: CommunicationAction(number: "10086", modemID: "device-1"))
        await state.load()
        state.text = "Original draft"
        return state
    }

    func testUncertainSendsPreserveDraftAndNeverReplay() async throws {
        let cases: [Result<BackendHTTPResponse, URLError>] = [
            .failure(URLError(.timedOut)),
            .failure(URLError(.networkConnectionLost)),
            .success(HTTPProblem.response(status: 408, code: "message_send_unconfirmed", detail: "Timed out")),
            .success(HTTPProblem.response(status: 504, code: "message_send_unconfirmed", detail: "Unknown")),
            .success(BackendHTTPResponse(data: Data("invalid JSON".utf8), status: 200)),
        ]
        for result in cases {
            let server = ComposerServer(result: result), state = try await make(server)
            let sent = await state.send()
            XCTAssertFalse(sent)
            XCTAssertEqual(state.failure?.code, "messages.resultUnknown")
            XCTAssertEqual(state.text, "Original draft")
            XCTAssertFalse(state.busy)
            await state.load()
            let writes = await server.submissions()
            XCTAssertEqual(writes.count, 1)
        }
    }

    func testRejectedSendPreservesBackendError() async throws {
        let server = ComposerServer(result: .success(HTTPProblem.response(status: 409, code: "message_profile_changed", detail: "SIM changed")))
        let state = try await make(server)
        let sent = await state.send()
        XCTAssertFalse(sent)
        XCTAssertEqual(state.failure?.code, "message_profile_changed")
        XCTAssertEqual(state.text, "Original draft")
    }

    func testInvalidRecipientDoesNotSubmitOrDiscardDraft() async throws {
        let server = ComposerServer(), state = try await make(server)
        state.number = "+"
        let sent = await state.send()
        XCTAssertFalse(sent)
        XCTAssertEqual(state.failure?.code, "apiError.invalid_recipient")
        XCTAssertEqual(state.text, "Original draft")
        let writes = await server.submissions()
        XCTAssertTrue(writes.isEmpty)
    }

    func testSendPinsInputAndKeepsEditsMadeDuringModemRefresh() async throws {
        let server = ComposerServer(), state = try await make(server)
        await server.holdNext("/modems/device-1")
        let send = Task { await state.send() }
        await server.waitForRequest()
        state.number = "10010"
        state.text = "Next draft"
        await server.release()
        let sent = await send.value
        XCTAssertTrue(sent)
        XCTAssertEqual(state.text, "Next draft")
        let writes = await server.submissions()
        let body = try JSONDecoder().decode([String: String].self, from: XCTUnwrap(writes.first?.httpBody))
        XCTAssertEqual(body["to"], "10086")
        XCTAssertEqual(body["text"], "Original draft")
    }

    func testCancelledSubmissionReportsUnknownWithoutClearingDraft() async throws {
        let server = ComposerServer(), state = try await make(server)
        await server.holdNext("/messages")
        let send = Task { await state.send() }
        await server.waitForRequest()
        send.cancel()
        await server.release()
        let sent = await send.value
        XCTAssertFalse(sent)
        XCTAssertEqual(state.failure?.code, "messages.resultUnknown")
        XCTAssertEqual(state.text, "Original draft")
        XCTAssertFalse(state.busy)
    }
}

private actor ComposerServer {
    private let base = CommunicationFixture()
    private let result: Result<BackendHTTPResponse, URLError>
    private var requests: [URLRequest] = []
    private var heldPath: String?
    private var held: CheckedContinuation<Void, Never>?
    private var arrived: CheckedContinuation<Void, Never>?

    init(result: Result<BackendHTTPResponse, URLError> = .success(BackendHTTPResponse(data: Data(#"{"to":"10086","conversationId":"sent"}"#.utf8), status: 200))) {
        self.result = result
    }
    func holdNext(_ path: String) { heldPath = path }
    func waitForRequest() async { if held == nil { await withCheckedContinuation { arrived = $0 } } }
    func release() { held?.resume(); held = nil }
    func submissions() -> [URLRequest] { requests.filter { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/messages") == true } }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if let path = heldPath, request.url?.path.hasSuffix(path) == true {
            heldPath = nil
            await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
        }
        if request.httpMethod == "POST", request.url?.path.hasSuffix("/messages") == true { return try result.get() }
        return try await base.respond(request)
    }
}

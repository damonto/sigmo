import XCTest
@testable import Sigmo

@MainActor
final class SigmoTransferTests: XCTestCase {
    func testSourceChoosesTheProfileAndRequesterTransfersOnTheOriginalConnection() async throws {
        let peer = SigmoTransferPeer()
        let flow = try await makeFlow(peer)
        flow.showRemoteAddress()
        XCTAssertEqual(flow.phase, .remoteAddress)
        flow.remoteAddress = "https://source.example/prefix"
        flow.connectRemote()
        let runID = flow.runID
        let run = Task { await flow.run() }
        try await wait { flow.stage == "waiting_authorization" }
        XCTAssertTrue(flow.sources.isEmpty)
        await peer.authorize()
        try await wait { flow.phase == .progress && flow.selectedProfile?.id == "se:8901" }
        XCTAssertTrue(flow.sources.isEmpty)
        XCTAssertEqual(flow.runID, runID)
        flow.startTransfer()
        await peer.complete()
        await run.value
        XCTAssertEqual(flow.phase, .completed)
        let count = await peer.socketCount
        let messages = await peer.messages
        XCTAssertEqual(count, 1)
        XCTAssertEqual(messages.count, 1)
        let command = try JSONDecoder().decode(ESIMClientMessageForTest.self, from: messages[0])
        XCTAssertEqual(command.sourceType, "sigmo")
        XCTAssertNil(command.sourceId)
        XCTAssertNil(command.profileId)
        XCTAssertEqual(command.version, 1)
        flow.interrupt()
    }

    func testLeavingRemoteRequestClosesTheSession() async throws {
        let peer = SigmoTransferPeer()
        let flow = try await makeFlow(peer)
        flow.showRemoteAddress(); flow.remoteAddress = "http://192.168.1.2"; flow.connectRemote()
        let run = Task { await flow.run() }
        try await wait { flow.stage == "waiting_authorization" }
        flow.interrupt()
        await run.value
        XCTAssertNil(flow.runID)
        let count = await peer.messages.count
        XCTAssertEqual(count, 1)
    }

    func testTransferResultUncertaintyUsesLocalizedFailure() async throws {
        let flow = try await makeFlow(SigmoTransferPeer())
        flow.showRemoteAddress(); flow.remoteAddress = "https://source.example"; flow.connectRemote()
        try flow.consume(JSONDecoder().decode(ESIMServerMessage.self, from: Data(#"{"type":"error","code":"transfer_result_uncertain"}"#.utf8)))
        XCTAssertEqual(flow.phase, .failed)
        XCTAssertEqual(flow.failure?.message, detailText("peer.uncertain"))
        flow.interrupt()
    }

    func testRepeatedApprovalCannotReplaceTheSelectedProfile() async throws {
        let flow = try await makeFlow(SigmoTransferPeer())
        flow.showRemoteAddress(); flow.remoteAddress = "https://source.example"; flow.connectRemote()
        defer { flow.interrupt() }
        let approval = try JSONDecoder().decode(ESIMServerMessage.self, from: Data(#"{"type":"source_selected","version":1,"sourceProfile":{"id":"se:8901","type":"esim","name":"Line","iccid":"8901","enabled":false,"supported":true}}"#.utf8))
        try flow.consume(approval)
        XCTAssertThrowsError(try flow.consume(approval))
        XCTAssertEqual(flow.selectedProfile?.id, "se:8901")
    }

    func testUnsupportedApprovalVersionsDoNotSelectAProfile() async throws {
        for version in [0, 2] {
            let flow = try await makeFlow(SigmoTransferPeer())
            flow.showRemoteAddress(); flow.remoteAddress = "https://source.example"; flow.connectRemote()
            defer { flow.interrupt() }
            let approval = try JSONDecoder().decode(ESIMServerMessage.self, from: Data(#"{"type":"source_selected","version":\#(version),"sourceProfile":{"id":"se:8901","type":"esim","name":"Line","iccid":"8901","enabled":false,"supported":true}}"#.utf8))
            XCTAssertThrowsError(try flow.consume(approval))
            XCTAssertNil(flow.selectedProfile)
        }
    }

    func testDisconnectAfterApprovalHasAnUncertainResult() async throws {
        let peer = SigmoTransferPeer()
        let flow = try await makeFlow(peer)
        flow.showRemoteAddress(); flow.remoteAddress = "https://source.example"; flow.connectRemote()
        defer { flow.interrupt() }
        let run = Task { await flow.run() }
        try await wait { flow.stage == "waiting_authorization" }
        await peer.authorize()
        try await wait { flow.selectedProfile != nil }
        await peer.disconnect()
        await run.value
        XCTAssertEqual(flow.phase, .failed)
        XCTAssertEqual(flow.failure?.message, detailText("peer.uncertain"))
        let count = await peer.socketCount
        XCTAssertEqual(count, 1)
    }

    func testDeclinedApprovalFixtureDoesNotInventASelectedProfile() async throws {
        let server = DetailFixtureServer(scenario: .peerApprovals)
        let config = BackendConfiguration(name: "Source", address: try BackendAddress.normalize("https://source.example"))
        let api = BackendAPI(configuration: config, transport: { try await server.respond($0) }, socketFactory: { try await server.socket($0) })
        let session = BackendSession(configuration: config, credentials: BackendCredentials(read: { _ in nil }, write: { _, _ in }), api: api)
        await session.refresh(force: true)
        let state = session.transferRequests
        let observer = Task { await state.run(session: session) }
        try await wait { state.current?.needsDecision == true }
        let request = try XCTUnwrap(state.current)
        await state.decide(session: session, request: request, accept: false)
        try await wait { state.current?.phase == "ended" }
        XCTAssertNil(state.current?.profile)
        XCTAssertNil(state.current?.sourceName)
        observer.cancel()
        await observer.value
    }

    func testApprovalSubscriptionSubmitsSourceSelectionAndCleansUp() async throws {
        let server = DetailFixtureServer(scenario: .peerApprovals)
        let config = BackendConfiguration(name: "Source", address: try BackendAddress.normalize("https://source.example"))
        let api = BackendAPI(configuration: config, transport: { try await server.respond($0) }, socketFactory: { try await server.socket($0) })
        let session = BackendSession(configuration: config, credentials: BackendCredentials(read: { _ in nil }, write: { _, _ in }), api: api)
        await session.refresh(force: true)
        let state = session.transferRequests
        let observer = Task { await state.run(session: session) }
        try await wait { state.current?.phase == "authorization" }
        let request = try XCTUnwrap(state.current)
        XCTAssertNil(request.profile)
        let source = try XCTUnwrap(request.sources?.first)
        let profile = try XCTUnwrap(source.profiles.first)
        let selection = PeerTransferSelection(sourceType: source.source.type, sourceId: source.source.id, profileId: profile.id, sourceImei: nil)
        await state.decide(session: session, request: request, accept: true, selection: selection)
        try await wait { state.current?.phase == "ended" }
        XCTAssertEqual(state.current?.outcome, "completed")
        await state.decide(session: session, request: request, accept: true, selection: selection)
        let decisions = await server.allRequests().filter { $0.url?.path.contains("/decisions") == true }
        XCTAssertEqual(decisions.count, 1)
        observer.cancel()
        await observer.value
        XCTAssertNil(state.current)
    }

    private func makeFlow(_ peer: SigmoTransferPeer) async throws -> ESIMFlowState {
        let server = DetailFixtureServer()
        let config = BackendConfiguration(name: "Target", address: try BackendAddress.normalize("https://target.example"))
        let api = BackendAPI(configuration: config,
            transport: { try await server.respond($0) }, socketFactory: { try await peer.socket($0) })
        let session = BackendSession(configuration: config, credentials: BackendCredentials(read: { _ in nil }, write: { _, _ in }), api: api)
        await session.refresh(force: true)
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        return ESIMFlowState(detail: detail, seID: "se-a")
    }
    private func wait(_ predicate: @MainActor () -> Bool) async throws {
        for _ in 0..<300 { if predicate() { return }; try await Task.sleep(for: .milliseconds(10)) }
        XCTFail("Flow did not reach the expected phase")
    }
}

private struct ESIMClientMessageForTest: Decodable {
    let sourceType: String?
    let sourceId: String?
    let profileId: String?
    let version: Int?
}

private actor SigmoTransferPeer {
    let buffer = DetailFixtureBuffer()
    private(set) var socketCount = 0
    private(set) var messages: [Data] = []
    func socket(_ request: URLRequest) throws -> BackendSocket {
        socketCount += 1
        return BackendSocket(send: { try await self.send($0) }, receive: { try await self.buffer.receive() }, close: { self.buffer.close() })
    }
    func send(_ data: Data) throws {
        messages.append(data)
        let message = try JSONDecoder().decode(ESIMCommand.self, from: data)
        switch message.type {
        case "source": emit(#"{"type":"progress","stage":"waiting_authorization"}"#)
        default: break
        }
    }
    func authorize() {
        emit(#"{"type":"source_selected","version":1,"sourceName":"Source","sourceProfile":{"id":"se:8901","seId":"se","type":"esim","name":"Line","iccid":"8901","enabled":false,"supported":true}}"#)
    }
    func complete() { emit(#"{"type":"completed"}"#) }
    func disconnect() { buffer.close() }
    private func emit(_ text: String) { buffer.push(Data(text.utf8)) }
    private struct ESIMCommand: Decodable { let type: String }
}

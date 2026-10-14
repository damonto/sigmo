import XCTest
@testable import Sigmo

@MainActor
final class RemoteAPDUTests: XCTestCase {
    func testLocalTransferProfilesIncludeOnlySupportedLines() async throws {
        let inactive = DetailFixtureServer.transferProfile("Inactive line", enabled: false)
        let active = DetailFixtureServer.transferProfile("Active line")
        let unsupported = DetailFixtureServer.transferProfile("Unsupported line", supported: false)
        let cases: [(name: String, profiles: [TransferProfile], ids: [String])] = [
            ("mixed", [unsupported, inactive, active], [inactive.id, active.id]),
            ("all supported", [active, inactive], [active.id, inactive.id]),
            ("all unsupported", [unsupported], []),
            ("empty", [], [])
        ]
        for item in cases {
            let peer = RemoteCardPeer()
            try await peer.setProfiles(item.profiles)
            let flow = try await makeFlow(peer: peer, card: CardFixture())
            await flow.loadSources()
            flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" }))
            await flow.loadProfiles()

            XCTAssertEqual(flow.profiles.map(\.id), item.ids, item.name)
            XCTAssertEqual(flow.phase, .profiles)
            XCTAssertNil(flow.failure)
            XCTAssertNil(flow.selectedProfile)
            XCTAssertFalse(flow.canTransfer)
            flow.selectProfile(unsupported)
            XCTAssertNil(flow.selectedProfile)
            if let profile = flow.profiles.first {
                flow.selectProfile(profile)
                XCTAssertTrue(flow.canTransfer)
                await flow.loadSources()
                flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "modem" }))
                XCTAssertTrue(flow.profiles.isEmpty)
                XCTAssertNil(flow.selectedProfile)
                XCTAssertFalse(flow.canTransfer)
            } else {
                flow.startTransfer()
                XCTAssertEqual(flow.phase, .profiles)
                XCTAssertNil(flow.runID)
            }
            flow.interrupt()
        }
    }

    func testProfileBrowsingAndTransferReuseConnection() async throws {
        let peer = RemoteCardPeer()
        let card = CardFixture()
        let flow = try await makeFlow(peer: peer, card: card)
        await flow.loadSources()
        let source = try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" })
        flow.selectSource(source)
        await flow.loadProfiles()
        XCTAssertNil(flow.failure)
        XCTAssertEqual(flow.profiles.count, 1)
        flow.selectProfile(try XCTUnwrap(flow.profiles.first))
        XCTAssertTrue(flow.canTransfer)
        flow.startTransfer()
        let run = Task { await flow.run() }
        try await wait { flow.phase == .sourceDeletion }
        await flow.send(.init(type: "source_deletion", accept: true))
        await run.value
        XCTAssertEqual(flow.phase, .completed)
        XCTAssertEqual(card.commands, [Data([0x00, 0xA4, 0x00, 0x00]), Data([0x80, 0xE2, 0x91, 0x00])])
        XCTAssertEqual(card.opens, 2)
        XCTAssertEqual(card.closes, 2)
        XCTAssertEqual(card.invalidations, 1)
        let sockets = await peer.socketCount
        let responses = await peer.responses
        XCTAssertEqual(sockets, 1)
        XCTAssertEqual(responses.compactMap(\.data), [card.response, card.response])
        flow.interrupt()
    }

    func testReaderAndSIMHotPlugUpdateSources() async throws {
        let card = CardFixture()
        card.readers = []
        let flow = try await makeFlow(peer: RemoteCardPeer(), card: card)
        await flow.loadSources()
        XCTAssertFalse(flow.sources.contains { $0.type == "client_ccid" })
        card.readers = [.init(name: "USB reader", cardPresent: false)]
        card.changed?()
        let empty = try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" })
        XCTAssertEqual(empty.cardPresent, false)
        flow.selectSource(empty)
        XCTAssertEqual(flow.phase, .sources)
        card.readers = [.init(name: "USB reader", cardPresent: true)]
        card.changed?()
        XCTAssertEqual(flow.sources.first { $0.type == "client_ccid" }?.cardPresent, true)
        card.readers = []
        card.changed?()
        XCTAssertFalse(flow.sources.contains { $0.type == "client_ccid" })
        flow.interrupt()
        XCTAssertNil(card.changed)
    }

    func testRemovingSelectedCardInvalidatesProfilesAndDoesNotReconnect() async throws {
        let card = CardFixture()
        let peer = RemoteCardPeer()
        let flow = try await makeFlow(peer: peer, card: card)
        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" }))
        await flow.loadProfiles()
        flow.selectProfile(try XCTUnwrap(flow.profiles.first))
        XCTAssertTrue(flow.canTransfer)
        card.removed?()
        XCTAssertFalse(flow.canTransfer)
        XCTAssertTrue(flow.profiles.isEmpty)
        XCTAssertNotNil(flow.failure)
        card.changed?() // Reinsert is discoverable, but grants no new session.
        XCTAssertFalse(flow.canTransfer)
        let sockets = await peer.socketCount
        XCTAssertEqual(sockets, 1)
        flow.interrupt()
    }

    func testGoingBackReleasesSelectedReader() async throws {
        let card = CardFixture()
        let flow = try await makeFlow(peer: RemoteCardPeer(), card: card)
        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" }))
        await flow.loadProfiles()
        await flow.loadSources()
        XCTAssertEqual(flow.phase, .sources)
        XCTAssertEqual(card.invalidations, 1)
        flow.interrupt()
    }

    func testRemovalDuringAPDUStopsBrowsingAndTransfer() async throws {
        for duringTransfer in [false, true] {
            let card = CardFixture()
            let peer = RemoteCardPeer()
            let flow = try await makeFlow(peer: peer, card: card)
            await flow.loadSources()
            flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" }))
            if duringTransfer {
                await flow.loadProfiles()
                flow.selectProfile(try XCTUnwrap(flow.profiles.first))
                flow.startTransfer()
            }
            card.suspendTransmits = true
            let task = Task {
                if duringTransfer { await flow.run() }
                else { await flow.loadProfiles() }
            }
            if duringTransfer {
                try await wait { flow.phase == .sourceDeletion }
                await flow.send(.init(type: "source_deletion", accept: true))
            }
            try await wait { card.pending != nil }
            card.readers = []
            card.changed?()
            card.removed?()
            // A quick reinsert cannot revive the in-flight operation.
            card.readers = [.init(name: "USB reader", cardPresent: true)]
            card.changed?()
            await task.value
            XCTAssertFalse(flow.busy)
            XCTAssertFalse(flow.canTransfer)
            XCTAssertTrue(flow.profiles.isEmpty)
            XCTAssertNotNil(flow.failure)
            if duringTransfer { XCTAssertEqual(flow.phase, .failed) }
            XCTAssertNil(card.pending)
            XCTAssertEqual(card.commands.count, duringTransfer ? 2 : 1)
            XCTAssertEqual(card.invalidations, 1)
            let sockets = await peer.socketCount
            XCTAssertEqual(sockets, 1)
            flow.interrupt()
        }
    }

    func testInvalidCommandsCannotExecuteOnAnotherCardConnection() async throws {
        for kind in ["duplicate", "stale connection", "unknown operation"] {
            let peer = RemoteCardPeer()
            let card = CardFixture()
            let remote = try await connected(peer: peer, card: card)
            await peer.scriptInvalid(kind)
            do {
                _ = try await remote.receive()
                XCTFail("Invalid command succeeded: \(kind)")
            } catch {}
            XCTAssertTrue(remote.closed)
            XCTAssertEqual(card.commands.count, 1, kind)
            XCTAssertEqual(card.invalidations, 1, kind)
        }
    }

    func testLostResponseDoesNotReplayCommandOrSendSecondReply() async throws {
        let peer = RemoteCardPeer()
        let card = CardFixture()
        let remote = try await connected(peer: peer, card: card)
        await peer.scriptLostResponse()
        do {
            _ = try await remote.receive()
            XCTFail("Lost response succeeded")
        } catch {}
        XCTAssertEqual(card.commands.count, 1)
        let attempts = await peer.failedReplyAttempts
        XCTAssertEqual(attempts, 1)
        XCTAssertTrue(remote.closed)
    }

    func testMalformedMessagesReleaseOpenCard() async throws {
        for message in ["not JSON", #"{"type":"card_request"}"#, #"{"type":"card_request","card":{"id":"2","connectionId":"1","operation":"transmit","command":"!"}}"#] {
            let peer = RemoteCardPeer()
            let card = CardFixture()
            let remote = try await connected(peer: peer, card: card)
            await peer.scriptMalformed(message)
            do {
                _ = try await remote.receive()
                XCTFail("Malformed message succeeded")
            } catch {}
            XCTAssertTrue(remote.closed)
            XCTAssertEqual(card.opens, 1)
            XCTAssertEqual(card.invalidations, 1)
            XCTAssertTrue(card.commands.isEmpty)
        }
    }

    func testHandshakeFailureReleasesReaderBeforeAnyAPDU() async throws {
        for reply in ["send failure", #"{"type":"card_request","card":{"id":"1","connectionId":"1","operation":"open"}}"#] {
            let peer = RemoteCardPeer()
            await peer.setHandshakeReply(reply)
            let card = CardFixture()
            do {
                _ = try await connected(peer: peer, card: card)
                XCTFail("Invalid handshake succeeded")
            } catch {}
            XCTAssertEqual(card.opens, 0)
            XCTAssertEqual(card.invalidations, 1)
        }
    }

    func testCancellingPendingAPDUReleasesReader() async throws {
        let peer = RemoteCardPeer()
        let card = CardFixture()
        let remote = try await connected(peer: peer, card: card)
        defer { remote.close() }
        card.suspendTransmits = true
        await peer.scriptLostResponse()
        let task = Task { try await remote.receive() }
        try await wait { card.pending != nil }
        task.cancel()
        try await wait { remote.closed }
        do {
            _ = try await task.value
            XCTFail("Cancelled operation succeeded")
        } catch {}
        XCTAssertNil(card.pending)
        XCTAssertEqual(card.commands.count, 1)
        XCTAssertEqual(card.invalidations, 1)
        let replies = await peer.responses
        XCTAssertEqual(replies.count, 1) // Only the completed open was acknowledged.
    }

    func testConcurrentReceiveInvalidatesSession() async throws {
        let peer = RemoteCardPeer()
        let card = CardFixture()
        let remote = try await connected(peer: peer, card: card)
        defer { remote.close() }
        card.suspendTransmits = true
        await peer.scriptLostResponse()
        let task = Task { try await remote.receive() }
        try await wait { card.pending != nil }
        do {
            _ = try await remote.receive()
            XCTFail("Concurrent receiver succeeded")
        } catch {}
        do {
            _ = try await task.value
            XCTFail("Interrupted operation succeeded")
        } catch {}
        XCTAssertTrue(remote.closed)
        XCTAssertEqual(card.commands.count, 1)
        XCTAssertEqual(card.invalidations, 1)
    }

    func testLocalReaderRequiresHTTPSBeforeOpeningCard() async throws {
        let peer = RemoteCardPeer()
        let card = CardFixture()
        let flow = try await makeFlow(peer: peer, card: card, scheme: "http")
        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first { $0.type == "client_ccid" }))
        await flow.loadProfiles()
        XCTAssertNotNil(flow.failure)
        XCTAssertEqual(card.opens, 0)
        let sockets = await peer.socketCount
        XCTAssertEqual(sockets, 0)
        flow.interrupt()
    }

    private func connected(peer: RemoteCardPeer, card: CardFixture) async throws -> RemoteAPDUSession {
        let source = TransferSource(type: "client_ccid", id: "USB reader", name: "USB reader", detail: nil, requiresSourceImei: true)
        let remote = RemoteAPDUSession(socket: await peer.socket(), source: source)
        try await remote.connect(access: card.access, invalidated: {})
        return remote
    }

    private func makeFlow(peer: RemoteCardPeer, card: CardFixture, scheme: String = "https") async throws -> ESIMFlowState {
        let server = DetailFixtureServer()
        let config = BackendConfiguration(name: "Reader", address: try BackendAddress.normalize("\(scheme)://reader.local"))
        let api = BackendAPI(configuration: config, transport: { try await server.respond($0) }, socketFactory: { _ in await peer.socket() })
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store, api: api)
        await session.refresh(force: true)
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        return ESIMFlowState(detail: detail, seID: "se-a", smartCards: card.access)
    }

    private func wait(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        guard condition() else {
            XCTFail("Timed out waiting for test condition")
            throw CancellationError()
        }
    }
}

@MainActor
private final class CardFixture {
    var readers = [LocalCardReader(name: "USB reader", cardPresent: true)]
    var changed: (@MainActor () -> Void)?
    var removed: (@MainActor () -> Void)?
    var opens = 0
    var closes = 0
    var invalidations = 0
    var commands: [Data] = []
    var suspendTransmits = false
    var pending: CheckedContinuation<Data, Error>?
    let response = Data([0x01, 0x6A, 0x82])

    var access: SmartCardAccess {
        SmartCardAccess(readers: { self.readers }, bind: { name, invalidated in
            XCTAssertEqual(name, "USB reader")
            self.removed = invalidated
            return SmartCardConnection(open: { self.opens += 1 }, transmit: {
                self.commands.append($0)
                if self.suspendTransmits {
                    return try await withCheckedThrowingContinuation { self.pending = $0 }
                }
                return self.response
            }, close: { self.closes += 1 }, invalidate: {
                self.invalidations += 1
                self.pending?.resume(throwing: BackendFailure.local("detail.remoteCardInterrupted"))
                self.pending = nil
            })
        }, observe: { changed in
            self.changed = changed
            return { self.changed = nil }
        })
    }
}

private actor RemoteCardPeer {
    private let buffer = DetailFixtureBuffer()
    private var scripted = false
    private var loseResponse = false
    private var handshakeReply = #"{"type":"source_ready","version":1}"#
    private var profilesReply = #"{"type":"profiles","profiles":[{"id":"8901","type":"esim","name":"Source","iccid":"8901","enabled":true,"supported":true}]}"#
    private(set) var socketCount = 0
    private(set) var responses: [RemoteCardReply] = []
    private(set) var failedReplyAttempts = 0

    func socket() -> BackendSocket {
        socketCount += 1
        return BackendSocket(send: { try await self.send($0) }, receive: { try await self.buffer.receive() }, close: { self.buffer.close() })
    }

    private func send(_ data: Data) throws {
        struct Message: Decodable { let type: String; var card: RemoteCardReply?; var accept: Bool? }
        let message = try JSONDecoder().decode(Message.self, from: data)
        switch message.type {
        case "source":
            if handshakeReply == "send failure" { throw URLError(.networkConnectionLost) }
            emit(handshakeReply)
        case "profiles": emitCard(id: "1", connection: "1", operation: "open")
        case "start": emit(#"{"type":"source_deletion","iccid":"8901"}"#)
        case "source_deletion":
            guard message.accept == true else { throw CancellationError() }
            emitCard(id: "4", connection: "4", operation: "open")
        case "card_response":
            let reply = try XCTUnwrap(message.card)
            responses.append(reply)
            if loseResponse, reply.id == "2" {
                failedReplyAttempts += 1
                throw URLError(.networkConnectionLost)
            }
            guard !scripted else { return }
            switch reply.id {
            case "1": emitCard(id: "2", connection: "1", operation: "transmit", command: "AKQAAA==")
            case "2": emitCard(id: "3", connection: "1", operation: "close")
            case "3": emit(profilesReply)
            case "4": emitCard(id: "5", connection: "4", operation: "transmit", command: "gOKRAA==")
            case "5": emitCard(id: "6", connection: "4", operation: "close")
            case "6": emit(#"{"type":"completed"}"#)
            default: throw CancellationError()
            }
        default: throw CancellationError()
        }
    }

    func scriptInvalid(_ kind: String) {
        scripted = true
        emitCard(id: "1", connection: "1", operation: "open")
        emitCard(id: "2", connection: "1", operation: "transmit", command: "AKQAAA==")
        switch kind {
        case "duplicate": emitCard(id: "2", connection: "1", operation: "transmit", command: "AKQAAA==")
        case "stale connection": emitCard(id: "3", connection: "old", operation: "transmit", command: "AKQAAA==")
        default: emitCard(id: "3", connection: "1", operation: "reset")
        }
    }

    func scriptLostResponse() {
        scripted = true
        loseResponse = true
        emitCard(id: "1", connection: "1", operation: "open")
        emitCard(id: "2", connection: "1", operation: "transmit", command: "AKQAAA==")
    }

    func scriptMalformed(_ message: String) {
        scripted = true
        emitCard(id: "1", connection: "1", operation: "open")
        emit(message)
    }

    func setHandshakeReply(_ reply: String) { handshakeReply = reply }

    func setProfiles(_ profiles: [TransferProfile]) throws {
        struct Reply: Encodable { let type = "profiles"; let profiles: [TransferProfile] }
        profilesReply = String(decoding: try JSONEncoder().encode(Reply(profiles: profiles)), as: UTF8.self)
    }

    private func emit(_ json: String) { buffer.push(Data(json.utf8)) }
    private func emitCard(id: String, connection: String, operation: String, command: String? = nil) {
        let payload = command.map { ",\"command\":\"\($0)\"" } ?? ""
        emit("{\"type\":\"card_request\",\"card\":{\"id\":\"\(id)\",\"connectionId\":\"\(connection)\",\"operation\":\"\(operation)\"\(payload)}}")
    }
}

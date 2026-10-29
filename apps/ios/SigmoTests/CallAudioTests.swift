import XCTest
@preconcurrency import LiveKitWebRTC
@testable import Sigmo

@MainActor
final class CallAudioTests: XCTestCase {
    func testCancelledStartPreservesTheCurrentSession() async throws {
        let audio = CallAudio()
        audio.startLocal()
        defer { audio.stop() }
        let gate = SettingsRequestGate()
        let closed = expectation(description: "cancelled startup releases the new socket")
        let socket = BackendSocket(send: { _ in XCTFail("A cancelled startup must not send signaling") },
                                   receive: { XCTFail("A cancelled startup must not receive signaling"); throw CancellationError() },
                                   close: { closed.fulfill() })
        let starting = Task {
            await gate.wait()
            try await audio.start(socket: socket, ice: [], clientID: "test")
        }
        await gate.waitForCaller()
        starting.cancel()
        await gate.release()
        do { try await starting.value; XCTFail("Expected cancellation before replacing the current session") }
        catch { XCTAssertTrue(error is CancellationError) }
        await fulfillment(of: [closed], timeout: 1)
        XCTAssertTrue(audio.ready)
        XCTAssertFalse(audio.connecting)
    }

    func testFailedStartupClosesTheSocketAndCancelsItsTimeout() async throws {
        let incoming = DetailFixtureBuffer()
        let closed = expectation(description: "failed startup releases signaling")
        let timedOut = expectation(description: "failed startup must not also time out")
        timedOut.isInverted = true
        let audio = CallAudio()
        try audio.prepare()
        defer { audio.stop() }
        audio.onFailure = { _ in timedOut.fulfill() }
        let socket = BackendSocket(send: { _ in throw URLError(.networkConnectionLost) }, receive: { try await incoming.receive() }, close: {
            incoming.close()
            closed.fulfill()
        })
        do {
            try await audio.start(socket: socket, ice: [], clientID: "test", timeout: .milliseconds(100))
            XCTFail("Expected the signaling error")
        } catch {
            XCTAssertEqual((error as? URLError)?.code, .networkConnectionLost)
        }
        await fulfillment(of: [closed], timeout: 1)
        await fulfillment(of: [timedOut], timeout: 0.2)
        XCTAssertFalse(audio.ready)
        XCTAssertFalse(audio.connecting)
    }

    func testConnectedPeerOutlivesTheInitialConnectionTimeout() async throws {
        let incoming = DetailFixtureBuffer()
        let audio = CallAudio()
        try audio.prepare()
        let factory = CallWebRTC.factory(capture: CallPCMTap(), render: CallPCMTap())
        let configuration = LKRTCConfiguration()
        configuration.sdpSemantics = .unifiedPlan
        configuration.iceServers = []
        let constraints = LKRTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        let peer = try XCTUnwrap(factory.peerConnection(with: configuration, constraints: constraints, delegate: nil))
        peer.add(factory.audioTrack(withTrackId: "test-downlink"), streamIds: ["test"])
        defer { audio.stop(); peer.close() }
        let send: @MainActor @Sendable (Data) async throws -> Void = { data in
            let message = try JSONDecoder().decode(CallSignal.self, from: data)
            switch message.type {
            case "offer":
                let offer = try XCTUnwrap(message.offer)
                try await peer.setRemoteDescription(LKRTCSessionDescription(type: .offer, sdp: offer.sdp))
                try await peer.setLocalDescription(peer.answer(for: constraints))
                for _ in 0..<100 {
                    if peer.iceGatheringState == .complete { break }
                    try await Task.sleep(for: .milliseconds(10))
                }
                let answer = try XCTUnwrap(peer.localDescription)
                incoming.push(try JSONEncoder().encode(CallSignal(type: "answer", answer: CallSDP(type: "answer", sdp: answer.sdp))))
            case "candidate":
                let candidate = try XCTUnwrap(message.candidate)
                try await peer.add(LKRTCIceCandidate(sdp: candidate.candidate, sdpMLineIndex: candidate.sdpMLineIndex ?? 0,
                                                   sdpMid: candidate.sdpMid))
            default: XCTFail("Unexpected signaling message")
            }
        }
        let ready = expectation(description: "local WebRTC peer connected")
        let failed = expectation(description: "connected peer must not time out")
        failed.isInverted = true
        var connected = false
        audio.onReady = { if !connected { connected = true; ready.fulfill() } }
        audio.onFailure = { _ in failed.fulfill() }
        let socket = BackendSocket(send: { data in try await send(data) }, receive: { try await incoming.receive() }, close: { incoming.close() })
        try await audio.start(socket: socket, ice: [], clientID: "test", timeout: .seconds(2))
        await fulfillment(of: [ready], timeout: 2)
        await fulfillment(of: [failed], timeout: 2.1)
        XCTAssertTrue(audio.ready)
        XCTAssertFalse(audio.connecting)
        XCTAssertNil(audio.failure)
    }

    func testSilentSignalingTimesOutAndClosesTheSocket() async throws {
        let buffer = DetailFixtureBuffer()
        let closed = expectation(description: "signaling closed after timeout")
        let failed = expectation(description: "initial audio connection timed out")
        let audio = CallAudio()
        try audio.prepare()
        defer { audio.stop() }
        audio.onFailure = { failure in
            XCTAssertEqual(failure.code, "calls.audioFailed")
            failed.fulfill()
        }
        let socket = BackendSocket(send: { _ in }, receive: { try await buffer.receive() }, close: {
            buffer.close()
            closed.fulfill()
        })
        try await audio.start(socket: socket, ice: [], clientID: "test", timeout: .milliseconds(200))
        XCTAssertTrue(audio.connecting)
        await fulfillment(of: [failed, closed], timeout: 2)
        XCTAssertFalse(audio.connecting)
        XCTAssertFalse(audio.ready)
        XCTAssertEqual(audio.failure?.code, "calls.audioFailed")
    }

    func testTimeoutAlsoInterruptsAStalledOfferSend() async throws {
        let incoming = DetailFixtureBuffer()
        let outgoing = DetailFixtureBuffer()
        let sending = expectation(description: "offer send started")
        let failed = expectation(description: "stalled offer timed out")
        let audio = CallAudio()
        try audio.prepare()
        defer { audio.stop() }
        audio.onFailure = { _ in failed.fulfill() }
        let socket = BackendSocket(send: { _ in
            sending.fulfill()
            _ = try await outgoing.receive()
        }, receive: { try await incoming.receive() }, close: {
            incoming.close()
            outgoing.close()
        })
        let starting = Task { try await audio.start(socket: socket, ice: [], clientID: "test", timeout: .milliseconds(200)) }
        await fulfillment(of: [sending, failed], timeout: 2)
        do { try await starting.value; XCTFail("A timed-out send must not complete audio startup") }
        catch { XCTAssertTrue(error is CancellationError) }
        XCTAssertFalse(audio.connecting)
    }

    func testStoppedConnectionCannotFailOrModifyTheNextSession() async throws {
        let incoming = DetailFixtureBuffer()
        let gate = SettingsRequestGate()
        let audio = CallAudio()
        try audio.prepare()
        defer { audio.stop() }
        let failed = expectation(description: "stopped connection must not time out")
        failed.isInverted = true
        audio.onFailure = { _ in failed.fulfill() }
        let socket = BackendSocket(send: { _ in await gate.wait() },
                                   receive: { try await incoming.receive() }, close: { incoming.close() })
        let starting = Task { try await audio.start(socket: socket, ice: [], clientID: "test", timeout: .milliseconds(100)) }
        await gate.waitForCaller()
        audio.startLocal()
        await gate.release()
        do { try await starting.value; XCTFail("A late offer send must not revive the stopped session") }
        catch { XCTAssertTrue(error is CancellationError) }
        await fulfillment(of: [failed], timeout: 0.2)
        XCTAssertTrue(audio.ready)
        XCTAssertFalse(audio.connecting)
        XCTAssertNil(audio.failure)
    }
}

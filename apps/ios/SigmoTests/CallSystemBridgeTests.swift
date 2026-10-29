import CallKit
import LiveCommunicationKit
import XCTest
@testable import Sigmo

@MainActor
final class CallSystemBridgeTests: XCTestCase {
    private enum TestFailure: Error { case rejected }

    private var unknownCall: NSError {
        NSError(domain: CXErrorDomainRequestTransaction, code: CXErrorCodeRequestTransactionError.unknownCallUUID.rawValue)
    }

    func testUnknownCallRecoveryWaitsForStartAndSharesRepeatedEndRequests() async throws {
        let error = unknownCall
        let requested = expectation(description: "both rejected end requests")
        requested.expectedFulfillmentCount = 2
        let bridge = CallSystemBridge(enabled: false, submit: { _ in requested.fulfill(); throw error })
        let gate = SettingsRequestGate()
        var received: [CallAction] = []
        bridge.perform = { action in
            received.append(action)
            if case .start = action { await gate.wait() }
        }
        let id = UUID()
        let handle = Handle(type: .phoneNumber, value: "10086", displayName: nil)
        let start = StartConversationAction(conversationUUID: id, handles: [handle], isVideo: false)
        let starting = bridge.execute(start, .start(id, "10086"))
        await gate.waitForCaller()
        let first = Task { try await bridge.request(.end(id)) }
        let second = Task { try await bridge.request(.end(id)) }
        await fulfillment(of: [requested], timeout: 1)
        XCTAssertEqual(received, [.start(id, "10086")])
        await gate.release()
        await starting.value
        try await first.value
        try await second.value
        XCTAssertEqual(received, [.start(id, "10086"), .end(id)])
    }

    func testUnknownCallRecoveryPropagatesBackendFailure() async {
        let error = unknownCall
        let bridge = CallSystemBridge(enabled: false, submit: { _ in throw error })
        bridge.perform = { _ in throw TestFailure.rejected }
        do {
            try await bridge.request(.end(UUID()))
            XCTFail("A rejected backend hangup must remain visible")
        } catch {
            XCTAssertTrue(error is TestFailure)
        }
    }

    func testRecoveryOnlyHandlesSingleEndWithUnknownCallUUID() async {
        let id = UUID()
        let cases: [(NSError, [CallAction])] = [
            (unknownCall, [.answer(id)]),
            (unknownCall, [.end(id), .answer(UUID())]),
            (NSError(domain: CXErrorDomainRequestTransaction, code: CXErrorCodeRequestTransactionError.unentitled.rawValue), [.end(id)]),
            (NSError(domain: NSCocoaErrorDomain, code: unknownCall.code), [.end(id)])
        ]
        for (failure, actions) in cases {
            let bridge = CallSystemBridge(enabled: false, submit: { _ in throw failure })
            bridge.perform = { _ in XCTFail("Unrelated rejection must not execute a backend action") }
            do {
                try await bridge.request(actions)
                XCTFail("Expected the original system rejection")
            } catch {
                XCTAssertEqual((error as NSError).domain, failure.domain)
                XCTAssertEqual((error as NSError).code, failure.code)
            }
        }
    }

    func testResetCancelsRecoveryWhileStartIsPending() async {
        let error = unknownCall
        let rejected = expectation(description: "end rejected by system")
        let bridge = CallSystemBridge(enabled: false, submit: { _ in rejected.fulfill(); throw error })
        let gate = SettingsRequestGate()
        bridge.perform = { action in
            if case .answer = action { await gate.wait() }
            else { XCTFail("Reset must prevent the backend hangup from starting") }
        }
        let id = UUID()
        let answer = JoinConversationAction(conversationUUID: id)
        let answering = bridge.execute(answer, .answer(id))
        await gate.waitForCaller()
        let ending = Task { try await bridge.request(.end(id)) }
        await fulfillment(of: [rejected], timeout: 1)
        bridge.resetFromSystem()
        await gate.release()
        await answering.value
        do { try await ending.value; XCTFail("Recovery must be cancelled by reset") }
        catch { XCTAssertTrue(error is CancellationError) }
    }

    func testNativeConversationActionsMapToExistingCallOperations() async {
        let id = UUID()
        let handle = Handle(type: .phoneNumber, value: "+12025550100", displayName: "Fixture")
        let cases: [(ConversationAction, CallAction)] = [
            (StartConversationAction(conversationUUID: id, handles: [handle], isVideo: false), .start(id, handle.value)),
            (JoinConversationAction(conversationUUID: id), .answer(id)),
            (PauseConversationAction(conversationUUID: id, isPaused: true), .hold(id, true)),
            (MuteConversationAction(conversationUUID: id, isMuted: true), .mute(id, true)),
            (PlayToneAction(conversationUUID: id, digits: "1#", tone: .single), .digits(id, "1#")),
            (EndConversationAction(conversationUUID: id), .end(id))
        ]
        let bridge = CallSystemBridge(enabled: false)
        var received: [CallAction] = []
        bridge.perform = { received.append($0) }
        for (action, _) in cases { await bridge.execute(action)?.value }
        XCTAssertEqual(received, cases.map { $0.1 })
        let video = StartConversationAction(conversationUUID: id, handles: [handle], isVideo: true)
        XCTAssertNil(bridge.execute(video))
        XCTAssertEqual(received.count, cases.count)
    }

    func testSystemResetCancelsPendingWorkAndIgnoresLateResults() async {
        let gate = SettingsRequestGate()
        var completions: [UUID] = []
        let bridge = CallSystemBridge(enabled: false, didComplete: { id, _ in completions.append(id) })
        bridge.perform = { _ in await gate.wait() }
        let action = JoinConversationAction(conversationUUID: UUID())
        let pending = bridge.execute(action, .answer(action.conversationUUID))
        await gate.waitForCaller()
        bridge.resetFromSystem()
        XCTAssertTrue(pending.isCancelled)
        await gate.release()
        await pending.value
        XCTAssertTrue(completions.isEmpty)
    }

    func testLocalMuteDoesNotWaitForRemoteDTMF() async {
        var completions: [UUID: [CallSystemBridge.Completion]] = [:]
        let bridge = CallSystemBridge(enabled: false, didComplete: { id, result in
            completions[id, default: []].append(result)
        })
        let gate = SettingsRequestGate()
        let muted = expectation(description: "mute while DTMF is waiting")
        bridge.perform = { action in
            switch action {
            case .digits: await gate.wait()
            case .mute: muted.fulfill()
            default: XCTFail("Unexpected action")
            }
        }
        let id = UUID()
        let digit = PlayToneAction(conversationUUID: id, digits: "1", tone: .single)
        let remote = bridge.execute(digit, .digits(id, "1"))
        await gate.waitForCaller()
        let mute = MuteConversationAction(conversationUUID: id, isMuted: true)
        let local = bridge.execute(mute, .mute(id, true))
        await fulfillment(of: [muted], timeout: 1)
        await local.value
        XCTAssertEqual(completions[mute.uuid], [.fulfilled])
        XCTAssertNil(completions[digit.uuid])
        await gate.release()
        await remote.value
        XCTAssertEqual(completions[digit.uuid], [.fulfilled])
        XCTAssertEqual(completions[mute.uuid], [.fulfilled])
    }

    func testEndCancelsAnOptionalRemoteControlBeforeRunning() async {
        var completions: [UUID: [CallSystemBridge.Completion]] = [:]
        let bridge = CallSystemBridge(enabled: false, didComplete: { id, result in
            completions[id, default: []].append(result)
        })
        let gate = SettingsRequestGate()
        let ended = expectation(description: "end after cancelled hold")
        bridge.perform = { action in
            switch action {
            case .hold:
                await withTaskCancellationHandler { await gate.wait() } onCancel: {
                    Task { await gate.release() }
                }
                try Task.checkCancellation()
            case .end: ended.fulfill()
            default: XCTFail("Unexpected action")
            }
        }
        let id = UUID()
        let hold = PauseConversationAction(conversationUUID: id, isPaused: true)
        let pending = bridge.execute(hold, .hold(id, true))
        await gate.waitForCaller()
        let end = EndConversationAction(conversationUUID: id)
        let ending = bridge.execute(end, .end(id))
        XCTAssertEqual(completions[hold.uuid], [.failed], "Preemption completes the control before its task exits")
        await fulfillment(of: [ended], timeout: 1)
        await pending.value
        await ending.value
        XCTAssertEqual(completions[hold.uuid], [.failed], "The canceled task must not complete the control again")
        XCTAssertEqual(completions[end.uuid], [.fulfilled])
    }

    func testTimeoutCancelsWorkWithoutCompletingTheSystemActionAgain() async {
        for lateFailure in [false, true] {
            var completions: [UUID: [CallSystemBridge.Completion]] = [:]
            let bridge = CallSystemBridge(enabled: false, didComplete: { id, result in
                completions[id, default: []].append(result)
            })
            let gate = SettingsRequestGate()
            bridge.perform = { _ in
                await gate.wait()
                if lateFailure { throw TestFailure.rejected }
            }
            let id = UUID()
            let action = PauseConversationAction(conversationUUID: id, isPaused: true)
            let pending = bridge.execute(action, .hold(id, true))
            await gate.waitForCaller()
            // Observe the bridge's SDK calls rather than a standalone ConversationAction's
            // system-owned state. No transaction is submitted in these tests.
            bridge.timedOut(action)
            XCTAssertTrue(pending.isCancelled)
            XCTAssertTrue(completions.isEmpty)
            await gate.release()
            await pending.value
            XCTAssertTrue(completions.isEmpty, "Neither a late result nor an error may complete a timed-out action")
        }
    }

    func testRepeatedEndRequestsDoNotCompleteAPreemptedControlAgain() async {
        var completions: [UUID: [CallSystemBridge.Completion]] = [:]
        let bridge = CallSystemBridge(enabled: false, didComplete: { id, result in
            completions[id, default: []].append(result)
        })
        let gate = SettingsRequestGate()
        bridge.perform = { action in
            if case .hold = action {
                await gate.wait()
                throw TestFailure.rejected
            }
        }
        let id = UUID()
        let hold = PauseConversationAction(conversationUUID: id, isPaused: true)
        let pending = bridge.execute(hold, .hold(id, true))
        await gate.waitForCaller()
        let first = EndConversationAction(conversationUUID: id), second = EndConversationAction(conversationUUID: id)
        let firstEnd = bridge.execute(first, .end(id))
        let secondEnd = bridge.execute(second, .end(id))
        XCTAssertTrue(pending.isCancelled)
        XCTAssertEqual(completions[hold.uuid], [.failed], "A slow cancellation must not leave completion ownership behind")
        await gate.release()
        await pending.value
        await firstEnd.value
        await secondEnd.value
        XCTAssertEqual(completions[hold.uuid], [.failed])
        XCTAssertEqual(completions[first.uuid], [.fulfilled])
        XCTAssertEqual(completions[second.uuid], [.fulfilled])
    }

    func testActionFailureCompletesExactlyOnce() async {
        var completions: [UUID: [CallSystemBridge.Completion]] = [:]
        let bridge = CallSystemBridge(enabled: false, didComplete: { id, result in
            completions[id, default: []].append(result)
        })
        bridge.perform = { _ in throw TestFailure.rejected }
        let id = UUID()
        let action = PauseConversationAction(conversationUUID: id, isPaused: true)
        await bridge.execute(action, .hold(id, true)).value
        XCTAssertEqual(completions[action.uuid], [.failed])
        bridge.timedOut(action)
        XCTAssertEqual(completions[action.uuid], [.failed])
    }
}

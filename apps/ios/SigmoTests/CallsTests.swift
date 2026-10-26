import AVFAudio
import Intents
import XCTest
@testable import Sigmo

@MainActor
final class CallsTests: XCTestCase {
    private func setup(_ fixture: CallsFixture, playback: CallRecordingPlayer = CallRecordingPlayer(),
                       recordings: CallRecordings? = nil,
                       diagnostics: DiagnosticLogStore = AppDiagnostics.store,
                       socketOpened: (@Sendable (URLRequest) -> Void)? = nil,
                       afterResponse: (@Sendable (URLRequest) async -> Void)? = nil) async throws -> (CallsState, BackendSession, CallLine) {
        let directory = URL.temporaryDirectory.appending(path: "CallTests-\(UUID())")
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "backends.json")), credentials: MemoryCredentials().store,
            makeAPI: { BackendAPI(configuration: $0, transport: { request in
                let response = try await fixture.respond(request)
                await afterResponse?(request)
                return response
            }, socketFactory: { request in
                let socket = await fixture.socket(request)
                socketOpened?(request)
                return socket
            }) })
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Home", address: "https://home.sigmo.test"))
        let session = try XCTUnwrap(store.sessions[id])
        await session.refresh(force: true)
        let line = CallLine(session: session, modem: try XCTUnwrap(session.modems.first))
        let calls = CallsState(store: store, fixtures: true,
            recordings: recordings ?? CallRecordings(directory: directory.appending(path: "recordings"), playback: playback), diagnostics: diagnostics)
        return (calls, session, line)
    }

    func testEndedPushSnapshotLogsTheBackendStateAndExplicitEndCause() async throws {
        let directory = URL.temporaryDirectory.appending(path: "CallPushDiagnostics-\(UUID())")
        let diagnostics = DiagnosticLogStore(directory: directory, defaults: nil)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        await diagnostics.setEnabled(true)
        let fixture = CallsFixture()
        let (calls, session, line) = try await setup(fixture, diagnostics: diagnostics)
        let record = CallsFixture.record("terminal-push", state: "ended", direction: "incoming", owner: "")
        try await fixture.publish(record)
        let id = try XCTUnwrap(record.callUUID.flatMap(UUID.init(uuidString:)))
        let content = PushContent(kind: "call", backendId: CallsFixture.backend, backendName: "Home",
                                  modemId: line.modemID, profileId: line.profileID, sender: record.number,
                                  callId: record.id, callUUID: record.callUUID, expiresAt: Int64(Date.now.timeIntervalSince1970) + 45)
        let recovery = try XCTUnwrap(calls.receivePush(content: content, id: id, connection: session.configuration) {})
        await recovery.value
        let events = try diagnosticEvents(await diagnostics.snapshot())
        let snapshot = try XCTUnwrap(events.first { $0.event == "voip.snapshot.received" })
        XCTAssertEqual(snapshot.fields["callUUID"], id.uuidString)
        XCTAssertEqual(snapshot.fields["state"], "ended")
        XCTAssertEqual(snapshot.fields["backendReason"], "normal")
        let validation = try XCTUnwrap(events.first { $0.event == "voip.snapshot.validated" })
        XCTAssertEqual(validation.fields["uuidMatches"], "true")
        XCTAssertEqual(snapshot.fields["endedAt"], record.endedAt)
        let ending = try XCTUnwrap(events.first { $0.event == "voip.ended" })
        XCTAssertEqual(ending.fields["callUUID"], id.uuidString)
        XCTAssertEqual(ending.fields["cause"], "backend_terminal")
        XCTAssertEqual(ending.fields["endReason"], "remote_ended")
        XCTAssertFalse(events.contains { $0.event == "voip.processed" })
        XCTAssertFalse(calls.hasCall)
        let updates = await fixture.count(method: "PATCH", suffix: "/calls/terminal-push")
        XCTAssertEqual(updates, 0)
    }

    func testRecordingRequestDoesNotFollowANewCallDuringIndexLoading() async throws {
        let gate = SettingsRequestGate()
        let directory = URL.temporaryDirectory.appending(path: "CallIndexTests-\(UUID())")
        let library = CallRecordings(directory: directory, readIndex: { _ in
            await gate.wait()
            return CallRecordings.Index()
        })
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture, recordings: library)
        await calls.dial(number: "10086", line: line)
        let first = try XCTUnwrap(calls.primary?.id)
        let recording = Task { await calls.toggleRecording() }
        await gate.waitForCaller()
        await calls.act(.end(first))
        let next = CallsFixture.record("second-index-call", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(next)
        calls.ingest(next, line: line)
        let second = try XCTUnwrap(calls.incoming?.id)
        await calls.answer(second)
        XCTAssertTrue(calls.audio.ready)
        await gate.release()
        await recording.value
        XCTAssertNil(library.activeID)
        XCTAssertTrue(library.items.isEmpty)
        XCTAssertNotEqual(first, second)
        await calls.act(.end(second))
    }

    func testTemporaryStatusFailuresPreserveMediaRecordingAndRecovery() async throws {
        for status in [500, 502, 429] {
            let fixture = CallsFixture()
            let (calls, _, line) = try await setup(fixture)
            await calls.dial(number: "10086", line: line)
            let id = try XCTUnwrap(calls.primary?.id)
            let recording = try await beginRecording(calls)
            await fixture.setCallReadFailure(HTTPProblem.response(status: status, code: "temporarily_unavailable", detail: "Retry later"))
            await calls.reconcile()
            XCTAssertEqual(calls.primary?.id, id)
            XCTAssertEqual(calls.recordings.activeID, recording)
            XCTAssertTrue(calls.audio.ready)
            XCTAssertEqual(calls.primary?.failure?.status, status)

            await fixture.setCallReadFailure(nil)
            var recovered = try XCTUnwrap(calls.primary?.record)
            recovered.hold = "remote"
            recovered.updatedAt = Date.now.addingTimeInterval(1).ISO8601Format()
            try await fixture.publish(recovered)
            await calls.reconcile()
            XCTAssertEqual(calls.primary?.record?.hold, "remote", "Healthy state must not be rejected by a terminal cache")
            XCTAssertNil(calls.primary?.failure)
            await calls.act(.end(id))
            await calls.recordings.stop(audio: calls.audio)?.value
            calls.foreground = false
            await calls.reconcile()
        }
    }

    func testEndpointReplacementStartsANewCallMonitorWithTheSameAccessRevision() async throws {
        let first = expectation(description: "old endpoint monitor")
        let second = expectation(description: "replacement endpoint monitor")
        let fixture = CallsFixture()
        let (calls, session, _) = try await setup(fixture, socketOpened: { request in
            if request.url?.host == "home.sigmo.test" { first.fulfill() }
            if request.url?.host == "new.sigmo.test" { second.fulfill() }
        })
        await calls.reconcile()
        await fulfillment(of: [first], timeout: 2)
        XCTAssertEqual(session.accessRevision, 0)
        XCTAssertTrue(calls.store.saveBackend(id: session.id, name: "Home", address: "https://new.sigmo.test"))
        let replacement = try XCTUnwrap(calls.store.sessions[session.id])
        await replacement.refresh(force: true)
        XCTAssertEqual(replacement.accessRevision, 0)
        await calls.reconcile()
        await fulfillment(of: [second], timeout: 2)
        calls.foreground = false
        await calls.reconcile()
    }

    func testProviderResetCannotRestoreAnInvitationFromPendingPushRecovery() async throws {
        let gate = SettingsRequestGate()
        let fixture = CallsFixture()
        let (calls, session, line) = try await setup(fixture, afterResponse: { request in
            if request.url?.path.hasSuffix("/calls/reset-push") == true { await gate.wait() }
        })
        let record = CallsFixture.record("reset-push", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(record)
        let id = try XCTUnwrap(record.callUUID.flatMap(UUID.init(uuidString:)))
        let content = PushContent(kind: "call", backendId: CallsFixture.backend, backendName: "Home",
            modemId: line.modemID, profileId: line.profileID, sender: record.number,
            callId: record.id, callUUID: record.callUUID, expiresAt: Int64(Date.now.timeIntervalSince1970) + 45)
        let recovery = try XCTUnwrap(calls.receivePush(content: content, id: id, connection: session.configuration) {})
        await gate.waitForCaller()
        calls.system.reset?()
        await gate.release()
        await recovery.value
        calls.ingest(record, line: line)
        XCTAssertFalse(calls.hasCall)
        XCTAssertTrue(calls.recordings.playback.allowed)
        XCTAssertNil(calls.failure)
        XCTAssertTrue(session.canAccessDevices, "A system reset does not revoke backend access")
    }

    func testPagedDeviceHistoryLoadsOlderCallsAndFiltersMissedOnTheServer() async throws {
        let fixture = CallsFixture()
        var records: [CallRecord] = []
        for index in 0..<61 {
            var record = CallsFixture.record("history-\(index)", state: "ended", direction: index == 60 ? "incoming" : "outgoing")
            record.startedAt = Date.now.addingTimeInterval(-Double(index) * 60).ISO8601Format()
            if index == 60 { record.answeredAt = "" }
            records.append(record)
        }
        await fixture.setPagedHistory(records)
        let (_, session, _) = try await setup(fixture)
        let history = CallHistoryState(session: session, modemID: "device-1")
        await history.load()
        XCTAssertEqual(history.items.count, 50)
        XCTAssertTrue(history.hasMore)
        await history.load(more: true)
        XCTAssertEqual(history.items.count, 61)
        XCTAssertFalse(history.hasMore)
        history.missedOnly = true
        await history.load()
        XCTAssertEqual(history.items.map(\.id), ["history-60"])
        XCTAssertFalse(history.hasMore)
    }

    private func beginRecording(_ calls: CallsState, audio: Bool = true) async throws -> UUID {
        await calls.toggleRecording()
        let id = try XCTUnwrap(calls.recordings.activeID)
        if audio {
            let samples = (0..<48_000).map { Float(sin(2 * .pi * 440 * Double($0) / 48_000)) * 0.2 }
            calls.audio.emitFixturePCM(CallPCM(samples: samples, sampleRate: 48_000,
                time: ProcessInfo.processInfo.systemUptime + 1), remote: true)
        }
        return id
    }

    func testDialPreservesLocalNumbersWithoutAddingCountryCode() async throws {
        let cases = [
            ("415 555 2671", "4155552671"),
            ("1 415 555 2671", "14155552671"),
            ("44 20 7946 0018", "442079460018"),
            ("020 7946 0018", "02079460018"),
            ("+1 415 555 2671", "+14155552671"),
            ("10086", "10086")
        ]
        for (input, expected) in cases {
            let (calls, _, line) = try await setup(CallsFixture())
            await calls.dial(number: input, line: line)
            let accepted = try XCTUnwrap(calls.primary)
            XCTAssertEqual(accepted.number, expected, input)
            XCTAssertEqual(accepted.record?.number, expected, input)
            await calls.act(.end(accepted.id))
        }
    }

    func testRecordingFinalizesForLocalRemoteAndMediaFirstHangup() async throws {
        for scenario in ["local", "remote", "media-first"] {
            let (calls, _, line) = try await setup(CallsFixture())
            await calls.dial(number: "10086", line: line)
            let call = try XCTUnwrap(calls.primary)
            let id = try await beginRecording(calls)
            if scenario == "media-first" {
                calls.audio.onFailure?(.local("calls.audioFailed"))
                XCTAssertFalse(calls.audio.ready)
            }
            if scenario == "local" { await calls.act(.end(call.id)) }
            else {
                var ended = try XCTUnwrap(call.record)
                ended.state = "ended"
                calls.ingest(ended, line: line)
                calls.ingest(ended, line: line)
            }
            XCTAssertNil(calls.recordings.activeID, scenario)
            // Repeating stop joins the library-owned task instead of scheduling a second mix.
            await calls.recordings.stop(audio: calls.audio)?.value
            let entry = try XCTUnwrap(calls.recordings.items.first)
            XCTAssertEqual(entry.id, id, scenario)
            XCTAssertTrue(entry.complete, scenario)
            XCTAssertGreaterThan(entry.duration, 0.9, scenario)
            XCTAssertNil(calls.recordings.failure, scenario)
            XCTAssertNil(calls.recordings.savingID, scenario)
            XCTAssertEqual(calls.recordings.items.count, 1)
            let file = try AVAudioFile(forReading: calls.recordings.url(for: entry))
            XCTAssertGreaterThan(file.length, 0)
            let reloaded = CallRecordings(directory: calls.recordings.directory)
            await reloaded.load()
            XCTAssertTrue(try XCTUnwrap(reloaded.items.first).complete)
        }
    }

    func testRecordingSaveSurvivesCancelledCallerAndPageDismissal() async throws {
        let (calls, _, line) = try await setup(CallsFixture())
        await calls.dial(number: "10086", line: line)
        let id = try await beginRecording(calls)
        let save = try XCTUnwrap(calls.recordings.stop(audio: calls.audio))
        XCTAssertEqual(calls.recordings.savingID, id)
        XCTAssertTrue(calls.recordings.busy)
        XCTAssertNil(calls.recordings.activeID)
        calls.presented = false
        let action = Task { await save.value }
        action.cancel()
        await action.value
        XCTAssertTrue(try XCTUnwrap(calls.recordings.items.first).complete)
        XCTAssertFalse(calls.recordings.busy)
        XCTAssertNil(calls.recordings.failure)
    }

    func testEmptyRecordingAndFailedOutputKeepRecoverableDescriptors() async throws {
        for empty in [true, false] {
            let (calls, _, line) = try await setup(CallsFixture())
            await calls.dial(number: "10086", line: line)
            let id = try await beginRecording(calls, audio: !empty)
            let entry = try XCTUnwrap(calls.recordings.items.first)
            let output = calls.recordings.directory.appending(path: entry.stem + ".m4a")
            if !empty { try FileManager.default.createDirectory(at: output, withIntermediateDirectories: false) }
            await calls.recordings.stop(audio: calls.audio)?.value
            XCTAssertFalse(try XCTUnwrap(calls.recordings.items.first).complete)
            XCTAssertEqual(calls.recordings.entryFailures[id]?.code,
                empty ? "calls.recordingEmpty" : "calls.recordingFinalizeFailed")
            XCTAssertFalse(calls.recordings.busy)
            XCTAssertTrue(FileManager.default.fileExists(atPath: calls.recordings.directory.appending(path: entry.stem + ".json").path))
            XCTAssertTrue(FileManager.default.fileExists(atPath: calls.recordings.directory.appending(path: entry.stem + ".remote.caf").path))
            if !empty {
                try FileManager.default.removeItem(at: output)
                await calls.recordings.recover(id)
                XCTAssertTrue(try XCTUnwrap(calls.recordings.items.first).complete)
                XCTAssertNil(calls.recordings.entryFailures[id])
            }
        }
    }

    func testEndAndAnswerFinalizesOnlyTheFirstRecording() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await calls.dial(number: "10086", line: line)
        var first = try XCTUnwrap(calls.primary?.record)
        let firstRecording = try await beginRecording(calls)
        let waiting = CallsFixture.record("waiting-recording", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(waiting)
        calls.ingest(waiting, line: line)
        let second = try XCTUnwrap(calls.incoming?.id)
        await calls.answer(second, endCurrent: true)
        await calls.recordings.stop(audio: calls.audio)?.value
        XCTAssertTrue(calls.recordings.items.first { $0.id == firstRecording }?.complete == true)
        let secondRecording = try await beginRecording(calls)
        first.state = "ended"
        calls.ingest(first, line: line)
        XCTAssertEqual(calls.recordings.activeID, secondRecording)
        XCTAssertEqual(calls.recordings.activeCallID, second)
        await calls.act(.end(second))
        await calls.recordings.stop(audio: calls.audio)?.value
        XCTAssertTrue(calls.recordings.items.allSatisfy(\.complete))
    }

    func testPlaybackAvailabilityTracksCallsBackgroundAndCallKitRelease() async throws {
        let (calls, _, line) = try await setup(CallsFixture())
        XCTAssertTrue(calls.recordings.playback.allowed)
        calls.foreground = false
        XCTAssertFalse(calls.recordings.playback.allowed)
        calls.foreground = true
        await calls.dial(number: "10086", line: line)
        XCTAssertFalse(calls.recordings.playback.allowed)
        calls.system.audioActivated?(true)
        await calls.act(.end(try XCTUnwrap(calls.primary?.id)))
        XCTAssertFalse(calls.recordings.playback.allowed)
        calls.system.audioActivated?(false)
        XCTAssertTrue(calls.recordings.playback.allowed)
        calls.ingest(CallsFixture.record("new-incoming", state: "ringing", direction: "incoming", owner: ""), line: line)
        XCTAssertFalse(calls.recordings.playback.allowed)
    }

    func testStartAndAnswerWaitForPlaybackSessionRelease() async throws {
        for incoming in [false, true] {
            let gate = SettingsRequestGate()
            let probe = PlaybackSessionProbe(deactivating: gate)
            let player = CallRecordingPlayer(session: CallPlaybackSession { try await probe.update($0) })
            let fixture = CallsFixture()
            let (seed, _, line) = try await setup(fixture, playback: player)
            await seed.dial(number: "10086", line: line)
            _ = try await beginRecording(seed)
            await seed.act(.end(try XCTUnwrap(seed.primary?.id)))
            await seed.recordings.stop(audio: seed.audio)?.value
            // The fixture reuses its dialed-call ID. Give this scenario fresh
            // tombstones so the seed call cannot reject the next dial.
            let calls = CallsState(store: seed.store, fixtures: true, recordings: seed.recordings)
            let entry = try XCTUnwrap(calls.recordings.items.first)
            await player.play(entry, url: calls.recordings.url(for: entry))?.value
            XCTAssertEqual(player.state, .playing)

            if incoming {
                let waiting = CallsFixture.record("playback-incoming", state: "ringing", direction: "incoming", owner: "")
                try await fixture.publish(waiting)
                calls.ingest(waiting, line: line)
            }
            let incomingID = calls.incoming?.id
            let completed = expectation(description: "Call action waits for playback cleanup")
            completed.isInverted = true
            let action = Task {
                if let incomingID { await calls.answer(incomingID) }
                else { await calls.dial(number: "10010", line: line) }
                completed.fulfill()
            }
            await gate.waitForCaller()
            XCTAssertEqual(player.state, .idle)
            await fulfillment(of: [completed], timeout: 0.1)
            await gate.release()
            await action.value
            XCTAssertNil(calls.failure)
            XCTAssertNotNil(calls.primary?.record)
            await calls.act(.end(try XCTUnwrap(calls.primary?.id)))
        }
    }

    func testDialEventBeforeHTTPResponseKeepsOneSystemIdentity() async throws {
        let gate = SettingsRequestGate()
        let fixture = CallsFixture()
        let (calls, session, line) = try await setup(fixture, afterResponse: { request in
            if request.httpMethod == "POST", request.url?.path.hasSuffix("/calls") == true { await gate.wait() }
        })
        let dial = Task { await calls.dial(number: "10086", line: line) }
        await gate.waitForCaller()
        let original = try XCTUnwrap(calls.primary?.id)
        let records = try await session.callRequest { api, token in try await api.calls(modemID: line.modemID, token: token) }
        let accepted = try XCTUnwrap(records.first { !$0.terminal })
        calls.ingest(accepted, line: line)
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertEqual(calls.primary?.id, original)
        await gate.release()
        await dial.value
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertEqual(calls.primary?.id, original)
        XCTAssertEqual(calls.primary?.record?.id, accepted.id)
    }

    func testLockedKeychainRecoversTheSavedCallOwnerWithoutReplacingIt() async throws {
        let fixture = CallsFixture()
        let (existing, _, line) = try await setup(fixture)
        let memory = PushMemoryPersistence()
        memory.values["call-client"] = Data("restored-client".utf8)
        memory.unavailable = true
        let calls = CallsState(store: existing.store, fixtures: true, clientID: "", recordings: existing.recordings, identity: memory.persistence)
        XCTAssertTrue(calls.clientID.isEmpty)
        XCTAssertEqual(calls.failure?.code, "push.keychainUnavailable")
        XCTAssertFalse(calls.available(line))
        memory.unavailable = false
        await calls.dial(number: "10086", line: line)
        XCTAssertEqual(calls.clientID, "restored-client")
        XCTAssertEqual(calls.primary?.record?.ownerClientId, "restored-client")
        XCTAssertEqual(memory.writes, 0)
        XCTAssertNil(calls.failure)
    }

    func testIncomingPushRestoresSavedOwnerAfterKeychainUnlock() async throws {
        let fixture = CallsFixture()
        let (existing, session, line) = try await setup(fixture)
        let memory = PushMemoryPersistence()
        memory.values["call-client"] = Data("restored-client".utf8)
        memory.unavailable = true
        let calls = CallsState(store: existing.store, fixtures: true, clientID: "", recordings: existing.recordings, identity: memory.persistence)
        XCTAssertTrue(calls.clientID.isEmpty)
        let record = CallsFixture.record("push-after-unlock", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(record)
        let id = try XCTUnwrap(record.callUUID.flatMap(UUID.init(uuidString:)))
        let content = PushContent(kind: "call", backendId: CallsFixture.backend, backendName: "Home",
            modemId: line.modemID, profileId: line.profileID, sender: record.number,
            callId: record.id, callUUID: record.callUUID, expiresAt: Int64(Date.now.timeIntervalSince1970) + 45)
        memory.unavailable = false
        let completion = expectation(description: "PushKit completion before owner recovery")
        calls.receivePush(content: content, id: id, connection: session.configuration) { completion.fulfill() }
        await fulfillment(of: [completion], timeout: 1)
        await calls.answer(id)
        XCTAssertEqual(calls.clientID, "restored-client")
        XCTAssertEqual(calls.primary?.record?.ownerClientId, "restored-client")
        XCTAssertEqual(memory.writes, 0)
        XCTAssertNil(calls.failure)
    }

    func testCallRequestsSurviveBackgroundButRejectLogoutAndCancellation() async throws {
        for action in ["background", "logout", "cancel"] {
            let (_, session, _) = try await setup(CallsFixture())
            let gate = SettingsRequestGate()
            let request = Task { try await session.callRequest { _, _ in await gate.wait(); return "snapshot" } }
            await gate.waitForCaller()
            switch action {
            case "background": session.foreground = false
            case "logout": await session.logout()
            default: request.cancel()
            }
            await gate.release()
            do {
                let value = try await request.value
                XCTAssertEqual(action, "background")
                XCTAssertEqual(value, "snapshot")
            } catch {
                XCTAssertNotEqual(action, "background")
                XCTAssertTrue(error is CancellationError)
            }
        }
    }

    func testIncomingEventsStayIsolatedAfterLineChanges() async throws {
        for action in ["logout", "remove", "address", "sim"] {
            let fixture = CallsFixture()
            let (calls, session, line) = try await setup(fixture)
            switch action {
            case "logout": await session.logout()
            case "remove": calls.store.deleteBackend(session.id)
            case "address":
                XCTAssertTrue(calls.store.saveBackend(id: session.id, name: "Changed", address: "https://new.sigmo.test"))
                await calls.store.refreshAll()
            default:
                await fixture.changeSIM(to: "new-sim")
                await session.refresh(force: true)
            }
            calls.ingest(CallsFixture.record("late", state: "ringing", direction: "incoming", owner: ""), line: line)
            XCTAssertFalse(calls.hasCall, action)
        }
    }

    func testSameCallIDOnDifferentBackendsRemainsIndependent() async throws {
        let (calls, _, firstLine) = try await setup(CallsFixture())
        let secondID = UUID()
        XCTAssertTrue(calls.store.saveBackend(id: secondID, name: "Work", address: "https://work.sigmo.test"))
        let second = try XCTUnwrap(calls.store.sessions[secondID])
        await second.refresh(force: true)
        let secondLine = CallLine(session: second, modem: try XCTUnwrap(second.modems.first))
        var first = CallsFixture.record("same-id", state: "ringing", direction: "incoming", owner: "")
        var other = CallsFixture.record("same-id", state: "ringing", direction: "incoming", owner: "")
        other.backendId = "work-server"
        calls.ingest(first, line: firstLine)
        calls.ingest(other, line: secondLine)
        XCTAssertEqual(calls.calls.count, 2)
        XCTAssertNil(calls.callbackLine(for: "10086"), "The same number on different backends requires line selection")
        first.state = "ended"
        calls.ingest(first, line: firstLine)
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertEqual(calls.primary?.line.backendID, secondID)
        XCTAssertEqual(calls.primary?.record?.callUUID, other.callUUID)
    }

    func testLogoutDuringPushBootstrapCannotRestoreAnInvitation() async throws {
        let gate = SettingsRequestGate()
        let fixture = CallsFixture()
        let (calls, session, line) = try await setup(fixture, afterResponse: { request in
            if request.url?.path.hasSuffix("/calls/push-call") == true { await gate.wait() }
        })
        let record = CallsFixture.record("push-call", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(record)
        let id = try XCTUnwrap(record.callUUID.flatMap(UUID.init(uuidString:)))
        let content = PushContent(kind: "call", backendId: CallsFixture.backend, backendName: "Home",
            modemId: line.modemID, profileId: line.profileID, sender: record.number,
            callId: record.id, callUUID: record.callUUID, expiresAt: Int64(Date.now.timeIntervalSince1970) + 45)
        let completion = expectation(description: "Immediate PushKit completion")
        calls.receivePush(content: content, id: id, connection: session.configuration) { completion.fulfill() }
        await fulfillment(of: [completion], timeout: 1)
        await gate.waitForCaller()
        await session.logout()
        await gate.release()
        // A system action waits for bootstrap before consulting the call table.
        await calls.answer(id)
        XCTAssertFalse(calls.hasCall)
        let count = await fixture.count(method: "PATCH", suffix: "/calls/push-call")
        XCTAssertEqual(count, 0)
    }

    func testLostDialResponseReconcilesWithoutRedialing() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await fixture.configure(lostDial: true)
        await calls.dial(number: "10086", line: line)
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertEqual(calls.primary?.record?.state, "active")
        XCTAssertEqual(calls.primary?.record?.ownerClientId, calls.clientID)
        let sent = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(sent, 1)
    }

    func testLostAnswerHoldAndEndResponsesReconcileWithoutReplay() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await fixture.configure(lostUpdate: true)
        let incoming = CallsFixture.record("accepted-call", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(incoming)
        calls.ingest(incoming, line: line)
        let id = try XCTUnwrap(calls.incoming?.id)
        await calls.answer(id)
        XCTAssertTrue(calls.primary?.ownsCall == true)
        XCTAssertEqual(calls.primary?.record?.state, "active")
        await calls.act(.hold(id, true))
        XCTAssertTrue(calls.primary?.record?.localHold == true)
        await calls.act(.end(id))
        XCTAssertFalse(calls.hasCall)
        XCTAssertNil(calls.failure)
        let count = await fixture.count(method: "PATCH", suffix: "/calls/accepted-call")
        XCTAssertEqual(count, 3)
    }

    func testRepeatedAndLateInvitationsDoNotResurrectEndedCalls() async throws {
        let (calls, _, line) = try await setup(CallsFixture())
        let record = CallsFixture.record("incoming", state: "ringing", direction: "incoming", owner: "")
        calls.ingest(record, line: line); calls.ingest(record, line: line)
        XCTAssertEqual(calls.calls.count, 1)
        var terminal = record
        terminal.state = "ended"; terminal.endedAt = Date.now.ISO8601Format()
        calls.ingest(terminal, line: line)
        calls.ingest(record, line: line)
        XCTAssertFalse(calls.hasCall)
        var wrongSIM = CallsFixture.record("wrong-sim", state: "ringing", direction: "incoming", owner: "")
        wrongSIM.profileId = "a-replacement-sim"
        calls.ingest(wrongSIM, line: line)
        XCTAssertFalse(calls.hasCall)
    }

    func testAnotherClientAnswerEndsOnlyTheLocalPresentation() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        var record = CallsFixture.record("incoming", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(record)
        calls.ingest(record, line: line)
        let id = try XCTUnwrap(calls.incoming?.id)
        record.state = "active"; record.ownerClientId = "web-client"
        try await fixture.publish(record)
        await calls.answer(id)
        XCTAssertFalse(calls.hasCall)
        XCTAssertEqual(calls.failure?.code, "call_in_use")
        let requests = await fixture.requests
        XCTAssertFalse(requests.contains { String(data: $0.httpBody ?? Data(), encoding: .utf8)?.contains("ended") == true })
    }

    func testEndAndAnswerWaitsForConfirmedEnd() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await calls.dial(number: "10086", line: line)
        let first = try XCTUnwrap(calls.primary?.id)
        let waiting = CallsFixture.record("waiting-call", state: "ringing", direction: "incoming", owner: "")
        try await fixture.publish(waiting)
        calls.ingest(waiting, line: line)
        let second = try XCTUnwrap(calls.incoming?.id)
        await fixture.configure(endFailure: true)
        await calls.answer(second, endCurrent: true)
        XCTAssertNotNil(calls.calls[first])
        XCTAssertFalse(calls.calls[second]?.ownsCall ?? true)
        var answered = await fixture.count(method: "PATCH", suffix: "/waiting-call")
        XCTAssertEqual(answered, 0)
        await fixture.configure()
        await calls.answer(second, endCurrent: true)
        XCTAssertNil(calls.calls[first])
        XCTAssertEqual(calls.calls.count, 1)
        XCTAssertTrue(calls.calls[second]?.ownsCall == true)
        answered = await fixture.count(method: "PATCH", suffix: "/waiting-call")
        XCTAssertEqual(answered, 1)
    }

    func testHoldAndMuteGateKeypadActions() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await calls.dial(number: "10086", line: line)
        let id = try XCTUnwrap(calls.primary?.id)
        await calls.act(.mute(id, true))
        XCTAssertTrue(calls.primary?.muted == true)
        await calls.act(.hold(id, true))
        XCTAssertTrue(calls.primary?.record?.localHold == true)
        await calls.act(.digits(id, "9"))
        var sent = await fixture.digits
        XCTAssertEqual(sent, [])
        await calls.act(.hold(id, false))
        await calls.act(.digits(id, "1")); await calls.act(.digits(id, "#"))
        sent = await fixture.digits
        XCTAssertEqual(sent, ["1", "#"])
    }

    func testHistoryFilteringDeletionAndLocalRecordingIdentity() async throws {
        let (calls, session, _) = try await setup(CallsFixture())
        let history = CallHistoryState(session: session, modemID: "device-1")
        await history.load()
        XCTAssertEqual(history.items.count, 2)
        history.missedOnly = true
        XCTAssertEqual(history.visibleItems.map(\.id), ["missed-call"])
        history.search = "12345"
        XCTAssertTrue(history.visibleItems.isEmpty)
        history.search = "100 86"
        let record = try XCTUnwrap(history.visibleItems.first)
        let recording = CallRecording(id: UUID(), backendID: session.id, modemID: "device-1", profileID: CallsFixture.profile,
            callID: record.id, number: record.number, line: "Home", createdAt: .now, receiveOnly: false)
        let metadata = calls.recordings.directory.appending(path: recording.stem + ".json")
        try JSONEncoder().encode(recording).write(to: metadata)
        let deleted = await history.delete(record)
        XCTAssertTrue(deleted)
        XCTAssertTrue(FileManager.default.fileExists(atPath: metadata.path))
    }

    func testHistoryMessageRequiresCurrentSIMAndRecipient() async throws {
        let (_, session, _) = try await setup(CallsFixture())
        let cases: [(name: String, profile: String?, number: String, modemID: String, allowed: Bool)] = [
            ("current SIM", CallsFixture.profile, "10086", "device-1", true),
            ("different SIM", "other-profile", "10086", "device-1", true),
            ("unknown SIM", nil, "10086", "device-1", true),
            ("empty recipient", CallsFixture.profile, "  ", "device-1", false),
            ("missing modem", CallsFixture.profile, "10086", "missing", false),
        ]
        for entry in cases {
            let history = CallHistoryState(session: session, modemID: entry.modemID)
            var record = CallsFixture.record(state: "ended")
            record.profileId = entry.profile
            record.number = entry.number
            XCTAssertEqual(history.canMessage(record), entry.allowed, entry.name)
            let draft = history.messageDraft(for: record)
            XCTAssertEqual(draft != nil, entry.allowed, entry.name)
            if let draft {
                XCTAssertEqual(draft.newRecipient, entry.number)
                XCTAssertEqual(draft.modemID, entry.modemID)
                XCTAssertTrue(draft.session === session)
            }
        }
        let history = CallHistoryState(session: session, modemID: "device-1")
        await session.logout()
        XCTAssertNil(history.messageDraft(for: CallsFixture.record(state: "ended")))
    }

    func testCallMessageDraftRejectsSIMChangeBeforeSending() async throws {
        let fixture = CallsFixture()
        let (_, session, _) = try await setup(fixture)
        let history = CallHistoryState(session: session, modemID: "device-1")
        let record = CallsFixture.record(state: "ended")
        let draft = try XCTUnwrap(history.messageDraft(for: record))
        draft.newDraft = "Reply on the original SIM"

        await fixture.changeSIM(to: "other-profile")
        await session.refresh(force: true)
        XCTAssertTrue(history.canMessage(record))
        let conversation = await draft.sendNew()
        XCTAssertNil(conversation)
        XCTAssertEqual(draft.sendFailure?.code, "messages.inactiveLine")
        XCTAssertEqual(draft.newDraft, "Reply on the original SIM")
        let sends = await fixture.count(method: "POST", suffix: "/messages")
        XCTAssertEqual(sends, 0, "A stale draft must not send from the replacement SIM")
    }

    func testUSSDNeverCreatesACallOrReplaysATimedOutCode() async throws {
        let fixture = CallsFixture()
        let (calls, session, line) = try await setup(fixture)
        await fixture.configure(ussdTimeout: true)
        let ussd = CallUSSD(session: session, line: line)
        ussd.send("*123#", initial: true)
        for _ in 0..<200 where ussd.busy { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertTrue(ussd.ended)
        ussd.send("*123#", initial: true)
        let sent = await fixture.count(method: "POST", suffix: "/ussd")
        XCTAssertEqual(sent, 1)
        XCTAssertFalse(calls.hasCall)
        XCTAssertEqual(ussd.failure?.code, "calls.ussdUnconfirmed")
    }

    private func callbackPerson(_ number: String, type: INPersonHandleType = .phoneNumber) -> INPerson {
        INPerson(personHandle: INPersonHandle(value: number, type: type),
                 nameComponents: nil, displayName: nil, image: nil, contactIdentifier: nil, customIdentifier: nil)
    }

    private func callbackLine(backendID: UUID, modemID: String, profileID: String, address: String) -> CallLine {
        CallLine(backendID: backendID, modemID: modemID, profileID: profileID,
                 backendName: "Home", label: "Line", region: "CN", address: address)
    }

    private func callbackIntent(contacts: [INPerson]? = nil, record: INCallRecord? = nil,
                                capability: INCallCapability = .audioCall) -> INStartCallIntent {
        INStartCallIntent(callRecordFilter: nil, callRecordToCallBack: record,
                          audioRoute: .unknown, destinationType: .normal, contacts: contacts, callCapability: capability)
    }

    func testSystemCallbackQueuesTheNumberBeforeDialing() async throws {
        let fixture = CallsFixture()
        let (calls, _, _) = try await setup(fixture)
        let person = callbackPerson("+86 (138) 0013-8000")
        var callbackID: UUID?
        for capability in [INCallCapability.audioCall, .unknown] {
            calls.requestCallback(callbackIntent(contacts: [person], capability: capability))
            XCTAssertEqual(calls.systemCallback?.number, "+8613800138000")
            if let callbackID {
                XCTAssertEqual(calls.systemCallback?.id, callbackID, "Repeated activities must reuse the pending request")
            } else {
                callbackID = calls.systemCallback?.id
            }
        }
        XCTAssertNil(calls.callbackNumber)
        XCTAssertFalse(calls.hasCall, "Receiving the activity must wait for callback line validation")
        let writes = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(writes, 0)
    }

    func testSystemCallbackAcceptsAudioCallIntents() async throws {
        let (calls, _, _) = try await setup(CallsFixture())
        let person = callbackPerson("+86 (138) 0013-8000")
        XCTAssertTrue(calls.requestCallback(callbackIntent(contacts: [person])))
        let request = try XCTUnwrap(calls.systemCallback)
        XCTAssertEqual(request.number, "+8613800138000")
        XCTAssertTrue(calls.requestCallback(callbackIntent(contacts: [person], capability: .unknown)))
        XCTAssertEqual(calls.systemCallback?.id, request.id)

        for contacts in [[], [person, person], [callbackPerson("*123#")],
                         [callbackPerson("user@example.com", type: .emailAddress)]] {
            XCTAssertFalse(calls.requestCallback(callbackIntent(contacts: contacts)))
            XCTAssertEqual(calls.systemCallback?.id, request.id)
        }
        XCTAssertFalse(calls.requestCallback(NSUserActivity(activityType: INStartCallIntentIdentifier)))
        XCTAssertEqual(calls.systemCallback?.id, request.id)
    }

    func testSystemCallbackBundleUsesAppActivitiesWithoutSiriExtension() throws {
        let activities = try XCTUnwrap(Bundle.main.object(forInfoDictionaryKey: "NSUserActivityTypes") as? [String])
        XCTAssertEqual(activities, [INStartCallIntentIdentifier])
        XCTAssertNil(Bundle.main.object(forInfoDictionaryKey: "NSSiriUsageDescription"))
        if let directory = Bundle.main.builtInPlugInsURL {
            let extensions = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
            XCTAssertFalse(extensions.contains { $0.lastPathComponent == "SigmoIntents.appex" })
        }
    }

    func testSystemCallbackUsesMissedCallRecordWithoutContacts() async throws {
        let (calls, _, _) = try await setup(CallsFixture())
        let record = INCallRecord(identifier: "missed-call", caller: callbackPerson("10086"),
                                  callRecordType: .missed, callCapability: .audioCall)
        let contactsOptions: [[INPerson]?] = [nil, []]
        for contacts in contactsOptions {
            calls.requestCallback(callbackIntent(contacts: contacts, record: record))
            XCTAssertEqual(calls.systemCallback?.number, "10086")
        }
    }

    func testSystemCallbackRejectsMissingNumbersAndServiceCodes() async throws {
        let (calls, _, _) = try await setup(CallsFixture())
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
        let invalid = [callbackIntent(), callbackIntent(contacts: [])]
        for intent in invalid {
            calls.requestCallback(intent)
            XCTAssertEqual(calls.systemCallback?.number, "10086")
        }
        for number in ["", "Unknown", "+", "12+34", " *123#", "#31#10086", String(repeating: "1", count: 129)] {
            let person = callbackPerson(number)
            calls.requestCallback(callbackIntent(contacts: [person]))
            XCTAssertEqual(calls.systemCallback?.number, "10086", "Invalid activities must not replace a pending callback")
        }
        XCTAssertFalse(calls.hasCall)
    }

    func testSystemCallbackRejectsMultipleContactsNonPhoneHandlesAndVideo() async throws {
        let fixture = CallsFixture()
        let (calls, _, _) = try await setup(fixture)
        let person = callbackPerson("10086")
        let unsupported = [
            callbackIntent(contacts: [person, callbackPerson("10010")]),
            callbackIntent(contacts: [callbackPerson("user1234@example.com", type: .emailAddress)]),
            callbackIntent(contacts: [callbackPerson("user1234", type: .unknown)]),
            callbackIntent(contacts: [person], capability: .videoCall),
        ]
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10000")]))
        for intent in unsupported {
            calls.requestCallback(intent)
            XCTAssertEqual(calls.systemCallback?.number, "10000", "Unsupported intents must not change a pending callback")
        }
        XCTAssertFalse(calls.hasCall)
        let writes = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(writes, 0)
    }

    func testSystemCallbackRestoresOriginalLineAfterRelaunch() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        await calls.dial(number: "10086", line: line)
        await calls.act(.end(try XCTUnwrap(calls.primary?.id)))
        calls.store.reloadConfiguration()
        let restored = CallsState(store: calls.store, fixtures: true, recordings: calls.recordings)
        restored.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
        XCTAssertFalse(restored.hasCall)
        let request = try XCTUnwrap(restored.systemCallback)
        guard case .dial(let number, let restoredLine) = await restored.resolveSystemCallback(request) else {
            return XCTFail("A complete saved route must dial directly")
        }
        XCTAssertEqual(restoredLine, line)
        restored.consumeSystemCallback(request)
        XCTAssertNil(restored.systemCallback)
        await restored.dial(number: number, line: restoredLine)
        XCTAssertEqual(restored.primary?.number, "10086")
        let writes = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(writes, 2, "Each user dial must create exactly one backend call")
        await restored.act(.end(try XCTUnwrap(restored.primary?.id)))
    }

    func testSystemCallbackDoesNotRepeatAnActiveCall() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        let person = callbackPerson("10086")
        let intent = callbackIntent(contacts: [person])
        calls.requestCallback(intent)
        let request = try XCTUnwrap(calls.systemCallback)
        await calls.dial(number: request.number, line: line)
        calls.consumeSystemCallback(request)

        XCTAssertTrue(calls.requestCallback(intent))
        XCTAssertTrue(calls.requestCallback(callbackIntent(contacts: [person], capability: .unknown)))
        XCTAssertNil(calls.systemCallback)
        XCTAssertNil(calls.failure)
        let writes = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(writes, 1)

        await calls.act(.end(try XCTUnwrap(calls.primary?.id)))
        XCTAssertTrue(calls.requestCallback(intent), "A later user callback is allowed after the call ends")
        XCTAssertNotNil(calls.systemCallback)
    }

    func testSystemCallbackRetainsLineAmbiguityAfterRelaunch() async throws {
        for change in ["backend", "modem", "sim", "address"] {
            let (calls, _, line) = try await setup(CallsFixture())
            let other = callbackLine(
                backendID: change == "backend" ? UUID() : line.backendID,
                modemID: change == "modem" ? "other-modem" : line.modemID,
                profileID: change == "sim" ? "other-sim" : line.profileID,
                address: change == "address" ? "https://other.sigmo.test" : line.address
            )
            calls.rememberCallbackRoute(number: "10086", line: line, startedAt: "2026-10-25T10:00:00Z")
            // Out-of-order events must still record that more than one line was used.
            calls.rememberCallbackRoute(number: "10086", line: other, startedAt: "2026-10-24T10:00:00Z")
            calls.rememberCallbackRoute(number: "10086", line: line, startedAt: "2026-10-25T11:00:00Z")

            let restored = CallsState(store: calls.store, fixtures: true, recordings: calls.recordings)
            XCTAssertNil(restored.callbackLine(for: "10086"), change)
            restored.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
            guard case .home(let number, let backendID) =
                    await restored.resolveSystemCallback(try XCTUnwrap(restored.systemCallback)) else {
                return XCTFail("An ambiguous \(change) route must require manual selection")
            }
            XCTAssertEqual(number, "10086")
            XCTAssertNil(backendID, "Do not restrict selection to the most recent backend")
            XCTAssertFalse(restored.hasCall)
        }
    }

    func testSystemCallbackWithoutSavedRouteUsesHomeDialer() async throws {
        let (calls, _, _) = try await setup(CallsFixture())
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
        let request = try XCTUnwrap(calls.systemCallback)
        guard case .home(let number, let backendID) = await calls.resolveSystemCallback(request) else {
            return XCTFail("A callback without a saved route must use the home dialer")
        }
        XCTAssertEqual(number, "10086")
        XCTAssertNil(backendID)
    }

    func testSystemCallbackMissingBackendOrModemUsesHomeDialer() async throws {
        let (calls, _, line) = try await setup(CallsFixture())
        let removedBackend = UUID()
        let missingBackendRoute = callbackLine(
            backendID: removedBackend, modemID: line.modemID, profileID: line.profileID, address: line.address
        )
        calls.rememberCallbackRoute(number: "10086", line: missingBackendRoute, startedAt: Date.now.ISO8601Format())
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
        let firstRequest = try XCTUnwrap(calls.systemCallback)
        guard case .home(let number, let backendID) = await calls.resolveSystemCallback(firstRequest) else {
            return XCTFail("A removed backend must use the home dialer")
        }
        XCTAssertEqual(number, "10086")
        XCTAssertNil(backendID)
        calls.consumeSystemCallback(firstRequest)

        let missingModemRoute = callbackLine(
            backendID: line.backendID, modemID: "missing-modem", profileID: line.profileID, address: line.address
        )
        calls.rememberCallbackRoute(number: "10010", line: missingModemRoute,
                                    startedAt: Date.now.addingTimeInterval(1).ISO8601Format())
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10010")]))
        guard case .home(_, let retainedBackend) = await calls.resolveSystemCallback(try XCTUnwrap(calls.systemCallback)) else {
            return XCTFail("A removed modem must use the home dialer")
        }
        XCTAssertEqual(retainedBackend, line.backendID)
    }

    func testSystemCallbackInactiveOrMissingLineUsesModemDialer() async throws {
        let fixture = CallsFixture()
        let (calls, _, line) = try await setup(fixture)
        let inactive = callbackLine(
            backendID: line.backendID, modemID: line.modemID, profileID: "old-profile", address: line.address
        )
        calls.rememberCallbackRoute(number: "10086", line: inactive, startedAt: Date.now.ISO8601Format())
        calls.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
        guard case .modem(let number, let backendID, let modemID) =
                await calls.resolveSystemCallback(try XCTUnwrap(calls.systemCallback)) else {
            return XCTFail("An unavailable saved line must use the modem dialer")
        }
        XCTAssertEqual(number, "10086")
        XCTAssertEqual(backendID, line.backendID)
        XCTAssertEqual(modemID, line.modemID)
        let writes = await fixture.count(method: "POST", suffix: "/calls")
        XCTAssertEqual(writes, 0)
    }

    func testSystemCallbackLockedOrOfflineModemDoesNotAutoDial() async throws {
        for mode in ["locked", "offline"] {
            let fixture = CallsFixture()
            let (calls, session, line) = try await setup(fixture)
            calls.rememberCallbackRoute(number: "10086", line: line, startedAt: Date.now.ISO8601Format())
            if mode == "locked" {
                await fixture.setModemState("locked")
                await session.refresh(force: true)
            } else {
                await fixture.setOffline(true)
            }
            calls.requestCallback(callbackIntent(contacts: [callbackPerson("10086")]))
            guard case .modem = await calls.resolveSystemCallback(try XCTUnwrap(calls.systemCallback)) else {
                return XCTFail("\(mode) modem must use the modem dialer")
            }
            XCTAssertFalse(calls.hasCall, mode)
        }
    }

    func testStaleSnapshotAndOriginalLineCallback() async throws {
        let (calls, session, line) = try await setup(CallsFixture())
        await calls.dial(number: "10086", line: line)
        var stale = try XCTUnwrap(calls.primary?.record)
        stale.state = "ringing"; stale.updatedAt = "2000-01-01T00:00:00Z"
        calls.ingest(stale, line: line)
        XCTAssertEqual(calls.primary?.record?.state, "active")
        XCTAssertEqual(calls.callbackLine(for: "10086"), line)
        await session.logout()
        XCTAssertNil(calls.callbackLine(for: "10086"))
        await calls.act(.hold(try XCTUnwrap(calls.primary?.id), true))
        XCTAssertEqual(calls.failure?.code, "calls.lineUnavailable")
    }

    func testPushCompletionOnInvalidAndDuplicateInvitation() async throws {
        let (calls, session, _) = try await setup(CallsFixture())
        let finished = expectation(description: "PushKit completion")
        finished.expectedFulfillmentCount = 2
        let id = UUID()
        calls.receivePush(content: nil, id: id, connection: session.configuration) { finished.fulfill() }
        calls.receivePush(content: nil, id: id, connection: session.configuration) { finished.fulfill() }
        await fulfillment(of: [finished], timeout: 1)
        XCTAssertFalse(calls.hasCall)
    }
}

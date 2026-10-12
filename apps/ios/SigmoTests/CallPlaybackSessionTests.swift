import AVFAudio
import Synchronization
import XCTest
@testable import Sigmo

@MainActor
final class CallPlaybackSessionTests: XCTestCase {
    private func audio() async throws -> PlaybackAudioProbe {
        let directory = URL.temporaryDirectory.appending(path: "PlaybackSessionTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let writer = try CallRecordingWriter(directory: directory, stem: "audio", origin: 1000) { XCTFail("Write audio") }
        writer.append(CallPCM(samples: Array(repeating: 0.1, count: 4800), sampleRate: 48_000, time: 1000.1), remote: true)
        _ = try await writer.finish()
        return try PlaybackAudioProbe(contentsOf: directory.appending(path: "audio.m4a"))
    }

    func testActiveSessionStillPreparesPlayerOffMainThread() async throws {
        let probe = PlaybackSessionProbe()
        let session = CallPlaybackSession { try await probe.update($0) }
        let player = try await audio()
        try await session.activate().value
        try await session.activate(preparing: player).value
        try await session.deactivate(stopping: player).value
        XCTAssertEqual(player.calls.preparations, 1)
        XCTAssertEqual(player.calls.stops, 1)
        XCTAssertFalse(player.calls.usedMainThread)
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false])
    }

    func testCancellationAfterActivationSkipsPreparationAndReleasesSession() async throws {
        let gate = SettingsRequestGate()
        let probe = PlaybackSessionProbe(activating: gate)
        let session = CallPlaybackSession { try await probe.update($0) }
        let player = try await audio()
        let activation = session.activate(preparing: player)
        await gate.waitForCaller()
        activation.cancel()
        let release = session.deactivate(stopping: player)
        await gate.release()
        do {
            try await activation.value
            XCTFail("Expected cancellation after the system activation completes")
        } catch { XCTAssertTrue(error is CancellationError) }
        try await release.value
        XCTAssertEqual(player.calls.preparations, 0)
        XCTAssertEqual(player.calls.stops, 1)
        XCTAssertFalse(player.calls.usedMainThread)
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false], "A completed system activation must still be balanced")
    }

    func testPreparationFailureStillReleasesSession() async throws {
        let probe = PlaybackSessionProbe()
        let session = CallPlaybackSession { try await probe.update($0) }
        let player = try await audio()
        player.rejectPreparation()
        do {
            try await session.activate(preparing: player).value
            XCTFail("Expected preparation failure")
        } catch { XCTAssertEqual((error as? BackendFailure)?.code, "calls.recordingPlaybackFailed") }
        try await session.deactivate(stopping: player).value
        XCTAssertEqual(player.calls.stops, 1)
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false])
    }

    func testCancelledDeactivationStillStopsPlayer() async throws {
        let probe = PlaybackSessionProbe()
        let session = CallPlaybackSession { try await probe.update($0) }
        let player = try await audio()
        try await session.activate(preparing: player).value
        let release = session.deactivate(stopping: player)
        release.cancel()
        try await release.value
        XCTAssertEqual(player.calls.stops, 1)
        XCTAssertFalse(player.calls.usedMainThread)
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false])
    }

    func testCancelledPendingActivationDoesNotTouchTheSystemSession() async throws {
        let probe = PlaybackSessionProbe()
        let session = CallPlaybackSession { try await probe.update($0) }
        let activation = session.activate()
        activation.cancel()
        try await session.deactivate().value
        let changes = await probe.changes
        XCTAssertEqual(changes, [])
    }

    func testInFlightActivationFinishesBeforeReleaseAndNextActivation() async throws {
        let activating = SettingsRequestGate()
        let deactivating = SettingsRequestGate()
        let probe = PlaybackSessionProbe(activating: activating, deactivating: deactivating)
        let session = CallPlaybackSession { try await probe.update($0) }
        let first = session.activate()
        await activating.waitForCaller()
        first.cancel()
        let release = session.deactivate()
        let next = session.activate()
        let duringActivation = await probe.changes
        XCTAssertEqual(duringActivation, [true])
        await activating.release()
        await deactivating.waitForCaller()
        let duringDeactivation = await probe.changes
        XCTAssertEqual(duringDeactivation, [true, false])
        await deactivating.release()
        try await release.value
        try await next.value
        try await session.deactivate().value
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false, true, false])
    }

    func testFailedActivationDoesNotPoisonLaterPlayback() async throws {
        let probe = PlaybackSessionProbe(failing: true)
        let session = CallPlaybackSession { try await probe.update($0) }
        do {
            try await session.activate().value
            XCTFail("Expected activation failure")
        } catch { XCTAssertEqual((error as? BackendFailure)?.code, "calls.recordingPlaybackFailed") }
        try await session.deactivate().value
        try await session.activate().value
        try await session.deactivate().value
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, true, false])
    }
}

/// Records boundary calls without depending on the simulator's audio hardware.
/// Its additional mutable state is protected by a mutex for cross-thread assertions.
private final class PlaybackAudioProbe: AVAudioPlayer, @unchecked Sendable {
    struct Calls {
        var preparations = 0
        var stops = 0
        var usedMainThread = false
        var rejectPreparation = false
    }

    private let state = Mutex(Calls())
    var calls: Calls { state.withLock { $0 } }
    func rejectPreparation() { state.withLock { $0.rejectPreparation = true } }

    override func prepareToPlay() -> Bool {
        let reject = state.withLock {
            $0.preparations += 1
            $0.usedMainThread = $0.usedMainThread || Thread.isMainThread
            return $0.rejectPreparation
        }
        return !reject
    }

    override func stop() {
        state.withLock {
            $0.stops += 1
            $0.usedMainThread = $0.usedMainThread || Thread.isMainThread
        }
    }
}

/// Models system operations that finish even if their caller is cancelled.
actor PlaybackSessionProbe {
    private(set) var changes: [Bool] = []
    private var activating: SettingsRequestGate?
    private var deactivating: SettingsRequestGate?
    private var failing: Bool?

    init(activating: SettingsRequestGate? = nil, deactivating: SettingsRequestGate? = nil, failing: Bool? = nil) {
        self.activating = activating
        self.deactivating = deactivating
        self.failing = failing
    }

    func update(_ active: Bool) async throws {
        changes.append(active)
        let gate = active ? activating : deactivating
        if active { activating = nil } else { deactivating = nil }
        await gate?.wait()
        if failing == active {
            failing = nil
            throw BackendFailure.local("calls.recordingPlaybackFailed")
        }
    }
}

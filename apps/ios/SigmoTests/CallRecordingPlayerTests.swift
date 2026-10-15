import AVFAudio
import XCTest
@testable import Sigmo

@MainActor
final class CallRecordingPlayerTests: XCTestCase {
    private func recording(seconds: Double = 4) async throws -> (CallRecordings, CallRecording) {
        let directory = URL.temporaryDirectory.appending(path: "PlayerTests-\(UUID())")
        let library = CallRecordings(directory: directory)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        var entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "10086", line: "Test", createdAt: .now, receiveOnly: true)
        let writer = try CallRecordingWriter(directory: directory, stem: entry.stem, origin: 1000) { XCTFail("Write audio") }
        let count = Int(seconds * 48_000)
        let samples = (0..<count).map { Float(sin(2 * .pi * 440 * Double($0) / 48_000)) * 0.1 }
        writer.append(CallPCM(samples: samples, sampleRate: 48_000, time: 1000 + seconds), remote: true)
        entry.duration = try await writer.finish()
        entry.complete = true
        try JSONEncoder().encode(entry).write(to: directory.appending(path: entry.stem + ".json"))
        let loaded = CallRecordings(directory: library.directory)
        await loaded.load()
        return (loaded, entry)
    }

    func testPauseResumeSeekAndStopUseTheAudioTimeline() async throws {
        let (library, entry) = try await recording()
        let player = library.playback
        defer { player.stop() }
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertEqual(player.state, .playing, player.failure?.message ?? "")
        XCTAssertEqual(player.selectedID, entry.id)
        XCTAssertGreaterThan(player.duration, 3.9)
        player.seek(to: 1)
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertEqual(player.state, .playing)
        XCTAssertEqual(player.position, 1, accuracy: 0.1)
        player.pause()
        let paused = player.position
        try await Task.sleep(for: .milliseconds(250))
        XCTAssertEqual(player.position, paused)
        player.seek(to: 2)
        XCTAssertEqual(player.state, .paused)
        XCTAssertEqual(player.position, 2, accuracy: 0.05)
        player.seek(to: -.infinity)
        XCTAssertEqual(player.position, 2, accuracy: 0.05)
        player.seek(to: -1)
        XCTAssertEqual(player.position, 0, accuracy: 0.05)
        player.seek(to: 100)
        XCTAssertEqual(player.position, player.duration, accuracy: 0.1)
        player.seek(to: 1)
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertEqual(player.state, .playing)
        try await Task.sleep(for: .milliseconds(450))
        XCTAssertGreaterThan(player.position, 1)
        player.stop()
        XCTAssertEqual(player.state, .idle)
        XCTAssertNil(player.selectedID)
        XCTAssertEqual(player.position, 0)
        XCTAssertEqual(player.duration, 0)
    }

    func testSwitchDeleteAndAvailabilityReleasePlayback() async throws {
        let (library, first) = try await recording()
        let (other, second) = try await recording()
        let player = library.playback
        defer { player.stop() }
        await player.play(first, url: library.url(for: first))?.value
        player.pause()
        await player.play(second, url: other.url(for: second))?.value
        XCTAssertEqual(player.selectedID, second.id)
        XCTAssertEqual(player.state, .playing)
        player.allowed = false
        XCTAssertNil(player.selectedID)
        await player.play(first, url: library.url(for: first))?.value
        XCTAssertEqual(player.state, .idle)
        player.allowed = true
        await player.play(first, url: library.url(for: first))?.value
        library.delete(first)
        XCTAssertNil(player.selectedID)
        XCTAssertTrue(library.items.isEmpty)
    }

    func testNaturalEndResetsPlaybackAndAllowsReplay() async throws {
        let (library, entry) = try await recording(seconds: 0.1)
        let player = library.playback
        defer { player.stop() }
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertEqual(player.state, .playing)
        for _ in 0..<30 {
            if player.state == .idle { break }
            try await Task.sleep(for: .milliseconds(100))
        }
        XCTAssertEqual(player.state, .idle)
        XCTAssertNil(player.selectedID)
        XCTAssertNil(player.failure)
        XCTAssertEqual(player.position, 0)
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertEqual(player.state, .playing)
    }

    func testPlayerCanBeReleasedDuringPlaybackOrPause() async throws {
        let (library, entry) = try await recording()
        for pause in [false, true] {
            let notifications = NotificationCenter()
            var player: CallRecordingPlayer? = CallRecordingPlayer(notifications: notifications)
            weak let released = player
            await player?.play(entry, url: library.url(for: entry))?.value
            XCTAssertEqual(player?.state, .playing)
            if pause { player?.pause() }
            player = nil
            XCTAssertNil(released)
            notifications.post(name: AVAudioSession.interruptionNotification, object: nil,
                userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue])
            await Task.yield()
        }
    }

    func testMissingAndCorruptFilesReportTheSelectedEntry() async throws {
        let (library, entry) = try await recording()
        let player = library.playback
        defer { player.stop() }
        let missing = library.directory.appending(path: "missing.m4a")
        await player.play(entry, url: missing)?.value
        XCTAssertEqual(player.failureID, entry.id)
        XCTAssertEqual(player.failure?.code, "calls.recordingFileMissing")
        try Data("invalid audio".utf8).write(to: missing)
        await player.play(entry, url: missing)?.value
        XCTAssertEqual(player.failure?.code, "calls.recordingPlaybackFailed")
        XCTAssertNil(player.selectedID)
        await player.play(entry, url: library.url(for: entry))?.value
        XCTAssertNil(player.failure)
        XCTAssertEqual(player.state, .playing)
    }

    func testAudioInterruptionStopsWithoutAutomaticResume() async throws {
        let (library, entry) = try await recording()
        // Synthetic interruptions must not invoke WebRTC's process-wide session
        // observers or change hardware state outside the player under test.
        let notifications = NotificationCenter()
        let player = CallRecordingPlayer(notifications: notifications)
        defer { player.stop() }
        await player.play(entry, url: library.url(for: entry))?.value
        notifications.post(name: AVAudioSession.interruptionNotification, object: nil,
            userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue])
        player.stop()
        await player.play(entry, url: library.url(for: entry))?.value
        await Task.yield()
        XCTAssertEqual(player.state, .playing, "A queued interruption must not stop a newer playback of the same recording")
        notifications.post(name: AVAudioSession.interruptionNotification, object: nil,
            userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue])
        for _ in 0..<20 {
            if player.state == .idle { break }
            await Task.yield()
        }
        XCTAssertEqual(player.state, .idle)
        XCTAssertNil(player.selectedID)
        notifications.post(name: AVAudioSession.interruptionNotification, object: nil,
            userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.ended.rawValue])
        await Task.yield()
        XCTAssertEqual(player.state, .idle)
    }

    func testStopAndDisallowDuringActivationNeverStartPlayback() async throws {
        let (library, entry) = try await recording()
        for disallow in [false, true] {
            let gate = SettingsRequestGate()
            let probe = PlaybackSessionProbe(activating: gate)
            let player = CallRecordingPlayer(session: CallPlaybackSession { try await probe.update($0) })
            let playback = player.play(entry, url: library.url(for: entry))
            await gate.waitForCaller()
            XCTAssertEqual(player.state, .preparing)
            if disallow { player.allowed = false }
            let stopped = player.stop()
            XCTAssertEqual(player.state, .idle)
            XCTAssertNil(player.selectedID)
            await gate.release()
            await playback?.value
            await stopped?.value
            XCTAssertEqual(player.state, .idle)
            XCTAssertNil(player.failure)
            let changes = await probe.changes
            XCTAssertEqual(changes, [true, false])
        }
    }

    func testSwitchDuringActivationPlaysOnlyTheNewestRecording() async throws {
        let (library, first) = try await recording()
        let (other, second) = try await recording()
        let gate = SettingsRequestGate()
        let probe = PlaybackSessionProbe(activating: gate)
        let player = CallRecordingPlayer(session: CallPlaybackSession { try await probe.update($0) })
        let oldPlayback = player.play(first, url: library.url(for: first))
        await gate.waitForCaller()
        let newPlayback = player.play(second, url: other.url(for: second))
        XCTAssertEqual(player.selectedID, second.id)
        XCTAssertEqual(player.state, .preparing)
        await gate.release()
        await oldPlayback?.value
        await newPlayback?.value
        XCTAssertEqual(player.selectedID, second.id)
        XCTAssertEqual(player.state, .playing)
        await player.stop()?.value
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false, true, false])
    }

    func testPlayerCanBeReleasedWhileActivationIsPending() async throws {
        let (library, entry) = try await recording()
        let gate = SettingsRequestGate()
        let probe = PlaybackSessionProbe(activating: gate)
        let session = CallPlaybackSession { try await probe.update($0) }
        var player: CallRecordingPlayer? = CallRecordingPlayer(session: session)
        weak let released = player
        let playback = player?.play(entry, url: library.url(for: entry))
        await gate.waitForCaller()
        player = nil
        XCTAssertNil(released)
        await gate.release()
        await playback?.value
        try await session.deactivate().value
        let changes = await probe.changes
        XCTAssertEqual(changes, [true, false])
    }

    func testLateReleaseFailureDoesNotReplaceNewPlayback() async throws {
        let (library, first) = try await recording()
        let (other, second) = try await recording()
        let gate = SettingsRequestGate()
        let probe = PlaybackSessionProbe(deactivating: gate, failing: false)
        let player = CallRecordingPlayer(session: CallPlaybackSession { try await probe.update($0) })
        await player.play(first, url: library.url(for: first))?.value
        let stopped = player.stop()
        await gate.waitForCaller()
        let playback = player.play(second, url: other.url(for: second))
        await gate.release()
        await stopped?.value
        await playback?.value
        XCTAssertEqual(player.selectedID, second.id)
        XCTAssertEqual(player.state, .playing)
        XCTAssertNil(player.failure)
        await player.stop()?.value
    }
}

import AVFAudio
import XCTest
@testable import Sigmo

@MainActor
final class CallRecordingTests: XCTestCase {
    func testBackendScopeSeparatesRecordingsWithMatchingCallIDs() throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let firstBackend = UUID(), secondBackend = UUID()
        let first = CallRecording(id: UUID(), backendID: firstBackend, modemID: "modem", profileID: "sim", callID: "call",
                                 number: "10086", line: "First", createdAt: .now, receiveOnly: false)
        let second = CallRecording(id: UUID(), backendID: secondBackend, modemID: "modem", profileID: "sim", callID: "call",
                                  number: "10086", line: "Second", createdAt: .now, receiveOnly: false)
        for entry in [first, second] {
            try JSONEncoder().encode(entry).write(to: directory.appending(path: "\(entry.stem).json"))
        }
        let library = CallRecordings(directory: directory)
        XCTAssertNil(library.failure)
        XCTAssertEqual(library.items(for: firstBackend).map(\.id), [first.id])
        XCTAssertEqual(library.items(for: secondBackend).map(\.id), [second.id])
        XCTAssertTrue(library.items(for: UUID()).isEmpty)
        library.delete(first)
        XCTAssertTrue(library.items(for: firstBackend).isEmpty)
        XCTAssertEqual(library.items(for: secondBackend).map(\.id), [second.id])
        XCTAssertTrue(FileManager.default.fileExists(atPath: directory.appending(path: "\(second.stem).json").path))
    }

    func testRecordingPreservesRegionForNumberDisplay() throws {
        let entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "2025550123", line: "Work", createdAt: .now, receiveOnly: false, regionCode: "US")
        let restored = try JSONDecoder().decode(CallRecording.self, from: JSONEncoder().encode(entry))
        XCTAssertEqual(restored.regionCode, "US")
        XCTAssertEqual(restored.number, "2025550123")
        XCTAssertEqual(PhoneNumberDisplay.format(restored.number, regionCode: restored.regionCode), "+1 (202) 555-0123")
    }

    func testCallbackJitterAndBatchesPreserveEverySampleInBothTracks() async throws {
        let schedules: [(name: String, delays: [Double])] = [
            ("regular", [0, 0]),
            ("jitter", [0, 0.002]),
            ("batched", [0.010, 0.0001]),
        ]
        for schedule in schedules {
            let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            addTeardownBlock { try FileManager.default.removeItem(at: directory) }
            let writer = try CallRecordingWriter(directory: directory, stem: schedule.name, origin: 1000) {
                XCTFail("Write \(schedule.name) audio")
            }
            let local = (0..<30_720).map { Float(sin(2 * .pi * 440 * Double($0) / 48_000)) * 0.4 }
            let remote = (0..<30_720).map { Float(sin(2 * .pi * 880 * Double($0) / 48_000)) * 0.4 }
            for frame in 0..<64 {
                let time = 1000 + Double(frame + 1) / 100 + schedule.delays[frame % 2]
                let range = (frame * 480)..<((frame + 1) * 480)
                writer.append(CallPCM(samples: Array(local[range]), sampleRate: 48_000, time: time), remote: false)
                writer.append(CallPCM(samples: Array(remote[range]), sampleRate: 48_000, time: time + 0.05), remote: true)
            }
            _ = try await writer.finish()
            for (suffix, samples, delay) in [("local", local, 0.0), ("remote", remote, 0.05)] {
                let leading = Int(((schedule.delays[0] + delay) * 48_000).rounded())
                let expected = Array(repeating: Float.zero, count: leading) + samples
                let actual = try readPCM(directory.appending(path: "\(schedule.name).\(suffix).caf"))
                XCTAssertEqual(actual.count, expected.count, "\(schedule.name) \(suffix) duration")
                let error = zip(actual, expected).map { abs($0 - $1) }.max() ?? 0
                XCTAssertLessThan(error, 0.000001, "\(schedule.name) \(suffix) must preserve the waveform")
            }
        }
    }

    func testDeviceRestartPreservesGapMuteAndSampleRateChanges() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let writer = try CallRecordingWriter(directory: directory, stem: "restart", origin: 1000) {
            XCTFail("Write restarted audio")
        }
        let frames: [CallPCM] = [
            CallPCM(samples: Array(repeating: 0.2, count: 480), sampleRate: 48_000, time: 1000.010),
            CallPCM(samples: Array(repeating: 0, count: 480), sampleRate: 48_000, time: 1000.022),
            CallPCM(samples: Array(repeating: 0.3, count: 160), sampleRate: 16_000, time: 1000.110, discontinuity: true),
            CallPCM(samples: Array(repeating: 0.4, count: 320), sampleRate: 32_000, time: 1000.1101),
            // An early callback after another reinitialization must not erase
            // valid samples already buffered by the device.
            CallPCM(samples: Array(repeating: 0.5, count: 80), sampleRate: 8000, time: 1000.1102, discontinuity: true),
        ]
        for frame in frames { writer.append(frame, remote: true) }
        let duration = try await writer.finish()
        let samples = try readPCM(directory.appending(path: "restart.remote.caf"))
        XCTAssertEqual(duration, 0.13, accuracy: 1 / 48_000)
        XCTAssertEqual(samples.count, 6240)
        for (range, value) in [(0..<480, Float(0.2)), (480..<4800, 0), (4800..<5280, 0.3),
                               (5280..<5760, 0.4), (5760..<6240, 0.5)] {
            let error = samples.dropFirst(range.lowerBound).prefix(range.count).map { abs($0 - value) }.max() ?? 0
            XCTAssertLessThan(error, 0.000001, "Samples \(range)")
        }
    }

    func testConcurrentCaptureAndRenderDrainBeforeFinish() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let queue = DispatchQueue(label: "RecordingTests.concurrent")
        let writer = try CallRecordingWriter(directory: directory, stem: "concurrent", origin: 1000, queue: queue) {
            XCTFail("Two streams within the queue limit must not overflow")
        }
        // Keep every frame queued while both audio threads contend for admission.
        // finish must drain both directions, including their final partial block.
        queue.suspend()
        DispatchQueue.concurrentPerform(iterations: 2) { direction in
            for frame in 0..<90 {
                writer.append(CallPCM(samples: Array(repeating: Float(direction + 1) / 10, count: 480),
                    sampleRate: 48_000, time: 1000 + Double(frame + 1) / 100), remote: direction == 1)
            }
        }
        queue.resume()
        _ = try await writer.finish()
        for (suffix, value) in [("local", Float(0.1)), ("remote", Float(0.2))] {
            let samples = try readPCM(directory.appending(path: "concurrent.\(suffix).caf"))
            XCTAssertEqual(samples.count, 43_200, suffix)
            XCTAssertTrue(samples.allSatisfy { abs($0 - value) < 0.000001 }, suffix)
        }
    }

    private func readPCM(_ url: URL) throws -> [Float] {
        let file = try AVAudioFile(forReading: url)
        let buffer = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: file.processingFormat,
            frameCapacity: 4096))
        var result: [Float] = []
        while file.framePosition < file.length {
            try file.read(into: buffer)
            guard buffer.frameLength > 0 else {
                XCTFail("PCM read stopped at \(file.framePosition) of \(file.length)")
                break
            }
            let samples = try XCTUnwrap(buffer.floatChannelData?[0])
            result.append(contentsOf: UnsafeBufferPointer(start: samples, count: Int(buffer.frameLength)))
        }
        XCTAssertEqual(AVAudioFramePosition(result.count), file.length, "Read the complete PCM track")
        return result
    }

    func testQueueOverflowCannotBeCommittedAsACompleteRecording() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let queue = DispatchQueue(label: "RecordingTests.overflow")
        let writer = try CallRecordingWriter(directory: directory, stem: "overflow", origin: 1000, queue: queue) {}
        // Hold disk work so admission pressure is deterministic. finish must
        // detect overflow even when no UI callback has requested stop yet.
        queue.suspend()
        for frame in 0..<201 {
            writer.append(CallPCM(samples: [0.1], sampleRate: 48_000,
                time: 1000 + Double(frame + 1) / 48_000), remote: true)
        }
        queue.resume()
        do {
            _ = try await writer.finish()
            XCTFail("Queue overflow must leave the recording pending recovery")
        } catch {
            XCTAssertEqual(error as? CallRecordingError, .writeFailed)
        }
        XCTAssertFalse(FileManager.default.fileExists(atPath: directory.appending(path: "overflow.m4a").path))
        let duration = try await CallRecordingWriter.recover(directory: directory, stem: "overflow")
        XCTAssertGreaterThan(duration, 0)
    }

    func testRecoveryReportsTheCurrentFailureAndSupportsOneSurvivingTrack() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "10086", line: "Test", createdAt: .now, receiveOnly: true)
        try JSONEncoder().encode(entry).write(to: directory.appending(path: entry.stem + ".json"))
        let library = CallRecordings(directory: directory)
        await library.recover(entry.id)
        XCTAssertEqual(library.entryFailures[entry.id]?.code, "calls.recordingEmpty")
        let remote = directory.appending(path: entry.stem + ".remote.caf")
        try Data("corrupt track".utf8).write(to: remote)
        await library.recover(entry.id)
        XCTAssertEqual(library.entryFailures[entry.id]?.code, "calls.recordingFinalizeFailed")
        XCTAssertFalse(try XCTUnwrap(library.items.first).complete)
        try FileManager.default.removeItem(at: remote)
        let writer = try CallRecordingWriter(directory: directory, stem: entry.stem, origin: 1000) { XCTFail("Write audio") }
        writer.append(CallPCM(samples: Array(repeating: 0.1, count: 4800), sampleRate: 48_000, time: 1000.1), remote: true)
        _ = try await writer.finish()
        // Older captures can contain only one CAF; preserve that audio on recovery.
        try FileManager.default.removeItem(at: directory.appending(path: entry.stem + ".local.caf"))
        try FileManager.default.removeItem(at: directory.appending(path: entry.stem + ".m4a"))
        await library.recover(entry.id)
        let completed = try XCTUnwrap(library.items.first)
        XCTAssertTrue(completed.complete)
        XCTAssertGreaterThan(completed.duration, 0)
        XCTAssertNil(library.entryFailures[entry.id])
        XCTAssertNil(library.failure)
        let audio = try AVAudioFile(forReading: library.url(for: completed))
        XCTAssertGreaterThan(audio.length, 0)
    }

    func testCancelledRecoveryKeepsPendingRecordingForRetry() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        let library = CallRecordings(directory: directory)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "10086", line: "Test", createdAt: .now, receiveOnly: false)
        try JSONEncoder().encode(entry).write(to: directory.appending(path: entry.stem + ".json"))
        let pending = CallRecordings(directory: library.directory)

        let task = Task { await pending.recover() }
        task.cancel()
        await task.value

        XCTAssertEqual(pending.items.count, 1)
        XCTAssertFalse(try XCTUnwrap(pending.items.first).complete)
        XCTAssertFalse(pending.busy)
        XCTAssertNil(pending.failure)
        XCTAssertTrue(FileManager.default.fileExists(atPath: directory.appending(path: entry.stem + ".json").path))
    }

    func testTwoWayPCMResamplingMixAndRecovery() async throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        let library = CallRecordings(directory: directory)
        let entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "10086", line: "Test", createdAt: .now, receiveOnly: false)
        let writer = try CallRecordingWriter(directory: directory, stem: entry.stem, origin: 1000) { XCTFail("PCM queue overflow or disk error") }
        // Distinct frequencies prove both directions survive resampling. Muting
        // the microphone for the last half must leave the remote voice intact.
        for frame in 0..<100 {
            let time = 1000 + Double(frame + 1) / 100
            let local = (0..<80).map { sample in Float(sin(2 * .pi * 440 * Double(frame * 80 + sample) / 8000)) * (frame < 50 ? 0.4 : 0) }
            let remote = (0..<480).map { sample in Float(sin(2 * .pi * 880 * Double(frame * 480 + sample) / 48000)) * 0.4 }
            writer.append(CallPCM(samples: local, sampleRate: 8000, time: time), remote: false)
            writer.append(CallPCM(samples: remote, sampleRate: 48000, time: time), remote: true)
        }
        let duration = try await writer.finish()
        XCTAssertEqual(duration, 1, accuracy: 0.03)
        let audio = try AVAudioFile(forReading: directory.appending(path: entry.stem + ".m4a"))
        let buffer = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: audio.processingFormat, frameCapacity: AVAudioFrameCount(audio.length)))
        try audio.read(into: buffer)
        let samples = try XCTUnwrap(buffer.floatChannelData?[0])
        func amplitude(_ frequency: Double, from: Int, to: Int) -> Double {
            var real = 0.0, imaginary = 0.0
            for sample in from..<to {
                let angle = 2 * .pi * frequency * Double(sample) / audio.processingFormat.sampleRate
                real += Double(samples[sample]) * cos(angle)
                imaginary += Double(samples[sample]) * sin(angle)
            }
            return hypot(real, imaginary) * 2 / Double(to - from)
        }
        XCTAssertGreaterThan(amplitude(440, from: 5000, to: 18000), 0.1)
        XCTAssertGreaterThan(amplitude(880, from: 5000, to: 18000), 0.1)
        XCTAssertLessThan(amplitude(440, from: 30000, to: 43000), 0.02)
        XCTAssertGreaterThan(amplitude(880, from: 30000, to: 43000), 0.1)
        // Simulate a process exit after CAF capture but before descriptor commit.
        try JSONEncoder().encode(entry).write(to: directory.appending(path: entry.stem + ".json"))
        try FileManager.default.removeItem(at: library.url(for: CallRecording(id: entry.id, backendID: entry.backendID, modemID: entry.modemID,
            profileID: entry.profileID, callID: entry.callID, number: entry.number, line: entry.line, createdAt: entry.createdAt, complete: true, receiveOnly: false)))
        let recovered = CallRecordings(directory: directory)
        await recovered.recover()
        XCTAssertTrue(recovered.items.first?.complete == true)
        XCTAssertEqual(recovered.items.first?.duration ?? 0, 1, accuracy: 0.03)
        let excluded = try directory.resourceValues(forKeys: [.isExcludedFromBackupKey])
        XCTAssertTrue(excluded.isExcludedFromBackup == true)
        recovered.delete(try XCTUnwrap(recovered.items.first))
        XCTAssertTrue(recovered.items.isEmpty)
        XCTAssertFalse(FileManager.default.fileExists(atPath: directory.appending(path: entry.stem + ".m4a").path))
    }

    func testDeletionFailureKeepsTheEntryAndCanBeRetried() throws {
        let directory = URL.temporaryDirectory.appending(path: "RecordingTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock {
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
            try FileManager.default.removeItem(at: directory)
        }
        let entry = CallRecording(id: UUID(), backendID: UUID(), modemID: "modem", profileID: "sim", callID: "call",
            number: "10086", line: "Test", createdAt: .now, complete: true, receiveOnly: true)
        try JSONEncoder().encode(entry).write(to: directory.appending(path: entry.stem + ".json"))
        let library = CallRecordings(directory: directory)
        try Data("audio placeholder".utf8).write(to: library.url(for: entry))
        library.playback.play(entry, url: directory.appending(path: "missing.m4a"))
        XCTAssertEqual(library.failure(for: entry.id)?.code, "calls.recordingFileMissing")
        XCTAssertNil(library.failure(for: UUID()))
        // A read-only directory permits listing and playback but rejects unlink.
        try FileManager.default.setAttributes([.posixPermissions: 0o500], ofItemAtPath: directory.path)
        library.delete(entry)
        XCTAssertEqual(library.items.map(\.id), [entry.id])
        XCTAssertNotNil(library.entryFailures[entry.id])
        XCTAssertNotNil(library.failure)
        XCTAssertEqual(library.failure(for: entry.id), library.entryFailures[entry.id])
        XCTAssertNotEqual(library.failure(for: entry.id), library.playback.failure)
        XCTAssertNil(library.failure(for: UUID()), "Entry errors must not leak to another recording")
        XCTAssertTrue(FileManager.default.fileExists(atPath: library.url(for: entry).path))

        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
        library.delete(entry)
        XCTAssertTrue(library.items.isEmpty)
        XCTAssertNil(library.entryFailures[entry.id])
        XCTAssertNil(library.failure)
        XCTAssertFalse(FileManager.default.fileExists(atPath: library.url(for: entry).path))
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: directory.path).isEmpty)
    }
}

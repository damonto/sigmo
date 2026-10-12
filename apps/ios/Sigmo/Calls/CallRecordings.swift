import AVFAudio
import Foundation
import Observation

struct CallRecording: Codable, Identifiable, Sendable {
    let id: UUID
    let backendID: UUID
    let modemID: String
    let profileID: String
    let callID: String
    let number: String
    let line: String
    let createdAt: Date
    var duration: Double = 0
    var complete = false
    var receiveOnly: Bool
    var backendInstanceID: String?
    var callUUID: String?
    var regionCode: String?
    var stem: String { id.uuidString }
}

@MainActor @Observable
final class CallRecordings {
    private(set) var items: [CallRecording] = []
    private(set) var activeID: UUID?
    private(set) var activeCallID: UUID?
    private(set) var savingID: UUID?
    private(set) var recoveringID: UUID?
    private(set) var entryFailures: [UUID: BackendFailure] = [:]
    var busy: Bool { savingID != nil || recoveringID != nil }
    let playback: CallRecordingPlayer
    var failure: BackendFailure?
    let directory: URL
    @ObservationIgnored private var writer: CallRecordingWriter?
    @ObservationIgnored private var finalization: Task<Void, Never>?

    init(directory: URL? = nil, playback: CallRecordingPlayer = CallRecordingPlayer()) {
        self.playback = playback
        self.directory = directory ?? URL.applicationSupportDirectory.appending(path: "Sigmo/Recordings", directoryHint: .isDirectory)
        do {
            try FileManager.default.createDirectory(at: self.directory, withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication])
            var url = self.directory
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try url.setResourceValues(values)
            try load()
        } catch { failure = .wrapping(error) }
    }

    private func load() throws {
        let files = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
        var recordings: [CallRecording] = []
        for file in files where file.pathExtension == "json" {
            do { recordings.append(try JSONDecoder().decode(CallRecording.self, from: Data(contentsOf: file))) }
            catch { failure = .local("calls.recordingRecoveryFailed") }
        }
        items = recordings.sorted { $0.createdAt > $1.createdAt }
    }

    func start(call: ActiveCall, audio: CallAudio) {
        guard !busy, activeID == nil, let record = call.record, record.hasMedia, call.ownsCall, audio.ready else { return }
        stopPlayback()
        let entry = CallRecording(id: UUID(), backendID: call.line.backendID, modemID: call.line.modemID,
            profileID: call.line.profileID, callID: record.callID, number: call.number,
            line: "\(call.line.backendName) · \(call.line.label)", createdAt: .now, receiveOnly: audio.receiveOnly,
            backendInstanceID: record.backendId, callUUID: record.callUUID, regionCode: call.line.region)
        do {
            try save(entry)
            // Keep a visible recovery entry even if opening one PCM file fails.
            items.insert(entry, at: 0)
            let writer = try CallRecordingWriter(directory: directory, stem: entry.stem) { [weak self, weak audio] in
                Task { @MainActor in
                    guard let self, self.activeID == entry.id else { return }
                    self.stop(audio: audio)
                }
            }
            self.writer = writer
            activeID = entry.id
            activeCallID = call.id
            failure = nil
            audio.record(capture: { writer.append($0, remote: false) }, render: { writer.append($0, remote: true) },
                onFailure: { writer.fail() })
        } catch { recordFailure(.local("calls.recordingWriteFailed"), id: entry.id) }
    }

    // Detach synchronously, before the media session or its call identity disappears.
    // The library owns finalization so dismissing/cancelling a UI action cannot lose audio.
    @discardableResult
    func stop(audio: CallAudio?) -> Task<Void, Never>? {
        guard let writer, let id = activeID else { return finalization }
        audio?.record(capture: nil, render: nil)
        self.writer = nil
        activeID = nil
        activeCallID = nil
        savingID = id
        let task = Task {
            defer { savingID = nil; finalization = nil }
            do { try complete(id: id, duration: await writer.finish()) }
            catch { recordFailure(finalizationFailure(error), id: id) }
        }
        finalization = task
        return task
    }

    func recover(_ id: UUID? = nil) async {
        guard !busy, activeID == nil, !Task.isCancelled else { return }
        defer { recoveringID = nil }
        let pending = items.filter { !$0.complete && (id == nil || $0.id == id) }
        for entry in pending {
            recoveringID = entry.id
            entryFailures[entry.id] = nil
            failure = nil
            do {
                let duration = try await CallRecordingWriter.recover(directory: directory, stem: entry.stem)
                try Task.checkCancellation()
                try complete(id: entry.id, duration: duration)
            } catch {
                guard !Task.isCancelled, !(error is CancellationError) else { return }
                recordFailure(finalizationFailure(error), id: entry.id)
            }
        }
    }

    private func complete(id: UUID, duration: Double) throws {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        var entry = items[index]
        let audio = try AVAudioFile(forReading: directory.appending(path: entry.stem + ".m4a"))
        guard audio.length > 0 else { throw CallRecordingError.empty }
        entry.duration = duration
        entry.complete = true
        try save(entry)
        items[index] = entry
        for suffix in ["local.caf", "remote.caf"] {
            let url = directory.appending(path: entry.stem + "." + suffix)
            if FileManager.default.fileExists(atPath: url.path) {
                do { try FileManager.default.removeItem(at: url) }
                catch { recordFailure(.local("calls.recordingCleanupFailed"), id: id) }
            }
        }
    }

    private func recordFailure(_ error: BackendFailure, id: UUID) {
        entryFailures[id] = error
        failure = error
    }

    private func finalizationFailure(_ error: Error) -> BackendFailure {
        switch error as? CallRecordingError {
        case .empty: .local("calls.recordingEmpty")
        case .writeFailed: .local("calls.recordingWriteFailed")
        case nil: .local("calls.recordingFinalizeFailed")
        }
    }

    private func save(_ value: CallRecording) throws {
        try JSONEncoder().encode(value).write(to: directory.appending(path: value.stem + ".json"), options: .atomic)
    }

    func url(for entry: CallRecording) -> URL {
        directory.appending(path: entry.stem + (entry.complete ? ".m4a" : ".remote.caf"))
    }

    func failure(for id: UUID) -> BackendFailure? {
        // A storage operation can fail after playback has already failed. Keep
        // the actionable deletion/recovery error visible until it is resolved.
        if let failure = entryFailures[id] { return failure }
        guard playback.failureID == id else { return nil }
        return playback.failure
    }

    func delete(_ entry: CallRecording) {
        guard activeID != entry.id, !busy else { return }
        failure = nil
        entryFailures[entry.id] = nil
        if playback.selectedID == entry.id { stopPlayback() }
        do {
            for suffix in ["m4a", "local.caf", "remote.caf", "json"] {
                let file = directory.appending(path: entry.stem + "." + suffix)
                if FileManager.default.fileExists(atPath: file.path) { try FileManager.default.removeItem(at: file) }
            }
            items.removeAll { $0.id == entry.id }
        } catch { recordFailure(.wrapping(error), id: entry.id) }
    }

    func stopPlayback() { playback.stop() }
}

enum CallRecordingError: Error { case empty, writeFailed }

// Files are confined to queue; the lock covers admission and enqueue order.
// Only tap queues call append; the native audio threads never acquire this lock.
// At most 200 owned frames can be queued for disk I/O.
final class CallRecordingWriter: @unchecked Sendable {
    private let queue: DispatchQueue
    private let lock = NSLock()
    private var queued = 0
    private var accepting = true
    private var local: AVAudioFile?
    private var remote: AVAudioFile?
    private var writeError: Error?
    private let directory: URL
    private let stem: String
    private let origin: TimeInterval
    private let format: AVAudioFormat
    private let onFailure: @Sendable () -> Void
    static let sampleRate = 48_000.0

    init(directory: URL, stem: String, origin: TimeInterval = ProcessInfo.processInfo.systemUptime,
         queue: DispatchQueue = DispatchQueue(label: "com.damon.sigmo.call-recording", qos: .utility),
         onFailure: @escaping @Sendable () -> Void) throws {
        self.directory = directory; self.stem = stem; self.origin = origin; self.queue = queue; self.onFailure = onFailure
        guard let format = AVAudioFormat(standardFormatWithSampleRate: Self.sampleRate, channels: 1) else { throw CallRecordingError.writeFailed }
        self.format = format
        local = try AVAudioFile(forWriting: directory.appending(path: stem + ".local.caf"), settings: format.settings)
        remote = try AVAudioFile(forWriting: directory.appending(path: stem + ".remote.caf"), settings: format.settings)
    }

    func append(_ pcm: CallPCM, remote: Bool) {
        // Contention is not a missing audio frame. Only admission/enqueue state
        // is locked; disk I/O and failure callbacks must stay outside this lock.
        lock.lock()
        guard accepting else { lock.unlock(); return }
        guard queued < 200 else {
            accepting = false
            // Store the failure before finish can be enqueued, even if the UI's
            // stop callback arrives after the call has already ended.
            queue.async { self.writeError = CallRecordingError.writeFailed }
            lock.unlock()
            onFailure()
            return
        }
        queued += 1
        // Enqueue before releasing admission so finish cannot overtake this frame.
        queue.async { [self] in
            defer { lock.lock(); queued -= 1; lock.unlock() }
            guard writeError == nil, let file = remote ? self.remote : local else { return }
            do { try write(pcm, to: file) }
            catch { writeError = error; onFailure() }
        }
        lock.unlock()
    }

    // A tap overflow must leave a recoverable recording, never a complete file
    // with an undisclosed gap. Its already-admitted audio drains before the error.
    func fail() {
        lock.lock()
        defer { lock.unlock() }
        guard accepting else { return }
        accepting = false
        queue.async { [self] in
            guard writeError == nil else { return }
            writeError = CallRecordingError.writeFailed
            onFailure()
        }
    }

    private func write(_ pcm: CallPCM, to file: AVAudioFile) throws {
        guard pcm.sampleRate > 0, !pcm.samples.isEmpty else { return }
        let count = max(1, Int((Double(pcm.samples.count) * Self.sampleRate / pcm.sampleRate).rounded()))
        // WebRTC can deliver consecutive 10ms frames in one device callback.
        // Advance by samples, not callback arrival times, or ordinary scheduling
        // jitter becomes inserted silence and discarded audio. Re-anchor only
        // on startup or an explicit audio-device/session discontinuity.
        var gap: AVAudioFramePosition = 0
        if file.length == 0 || pcm.discontinuity {
            let start = AVAudioFramePosition(((pcm.time - origin) * Self.sampleRate).rounded()) - AVAudioFramePosition(count)
            gap = max(0, start - file.length)
        }
        while gap > 0 {
            let size = AVAudioFrameCount(min(4096, gap))
            let silence = try Self.buffer(format: format, count: size)
            try file.write(from: silence)
            gap -= AVAudioFramePosition(size)
        }
        let buffer = try Self.buffer(format: format, count: AVAudioFrameCount(count))
        guard let destination = buffer.floatChannelData?[0] else { throw CallRecordingError.writeFailed }
        for index in 0..<Int(buffer.frameLength) {
            let offset = Double(index) * pcm.sampleRate / Self.sampleRate
            let lower = min(pcm.samples.count - 1, Int(offset))
            let upper = min(pcm.samples.count - 1, lower + 1)
            let fraction = Float(offset - Double(lower))
            destination[index] = pcm.samples[lower] * (1 - fraction) + pcm.samples[upper] * fraction
        }
        try file.write(from: buffer)
    }

    func finish() async throws -> Double {
        try await withCheckedThrowingContinuation { continuation in
            lock.lock()
            accepting = false
            queue.async { [self] in
                // Finalize PCM headers before the mixer opens the same files;
                // releasing an Objective-C object can wait for an autorelease pool.
                local?.close(); remote?.close()
                local = nil; remote = nil
                do {
                    if writeError != nil { throw CallRecordingError.writeFailed }
                    continuation.resume(returning: try Self.mix(directory: directory, stem: stem))
                }
                catch { continuation.resume(throwing: error) }
            }
            lock.unlock()
        }
    }

    // Recovery belongs to its caller; finalizing a live capture above must drain
    // the queue even when the control that requested Stop leaves the screen.
    @concurrent static func recover(directory: URL, stem: String) async throws -> Double {
        try mix(directory: directory, stem: stem)
    }

    private static func mix(directory: URL, stem: String) throws -> Double {
        try Task.checkCancellation()
        let local = try track(directory.appending(path: stem + ".local.caf"))
        let remote = try track(directory.appending(path: stem + ".remote.caf"))
        let length = max(local?.length ?? 0, remote?.length ?? 0)
        guard length > 0, let format = AVAudioFormat(standardFormatWithSampleRate: sampleRate, channels: 1) else { throw CallRecordingError.empty }
        let settings: [String: Any] = [AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: sampleRate,
            AVNumberOfChannelsKey: 1, AVEncoderBitRateKey: 64_000]
        let file = try AVAudioFile(forWriting: directory.appending(path: stem + ".m4a"), settings: settings,
            commonFormat: .pcmFormatFloat32, interleaved: false)
        defer { file.close() }
        var position: AVAudioFramePosition = 0
        while position < length {
            try Task.checkCancellation()
            let size = AVAudioFrameCount(min(4096, length - position))
            let left = try buffer(format: format, count: size)
            let right = try buffer(format: format, count: size)
            if let local, local.framePosition < local.length { try local.read(into: left, frameCount: size) }
            if let remote, remote.framePosition < remote.length { try remote.read(into: right, frameCount: size) }
            let mixed = try buffer(format: format, count: size)
            guard let target = mixed.floatChannelData?[0], let a = left.floatChannelData?[0], let b = right.floatChannelData?[0] else { throw CallRecordingError.writeFailed }
            for index in 0..<Int(size) {
                target[index] = max(-1, min(1, ((index < left.frameLength ? a[index] : 0) + (index < right.frameLength ? b[index] : 0)) * 0.5))
            }
            try file.write(from: mixed)
            position += AVAudioFramePosition(size)
        }
        return Double(length) / sampleRate
    }

    private static func track(_ url: URL) throws -> AVAudioFile? {
        // A capture may have opened only one track before being interrupted.
        // Missing tracks are silence; a corrupt existing track still needs attention.
        guard FileManager.default.fileExists(atPath: url.path) else { return nil }
        return try AVAudioFile(forReading: url)
    }

    private static func buffer(format: AVAudioFormat, count: AVAudioFrameCount) throws -> AVAudioPCMBuffer {
        guard let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: count), let samples = buffer.floatChannelData?[0] else { throw CallRecordingError.writeFailed }
        buffer.frameLength = count
        for index in 0..<Int(count) { samples[index] = 0 }
        return buffer
    }
}

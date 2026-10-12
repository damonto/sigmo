import AVFAudio
import Foundation
import Observation

/// One foreground player shared by the library and call details.
@MainActor @Observable
final class CallRecordingPlayer: NSObject, AVAudioPlayerDelegate {
    enum State { case idle, preparing, playing, paused }

    private(set) var selectedID: UUID?
    private(set) var state: State = .idle
    private(set) var position: TimeInterval = 0
    private(set) var duration: TimeInterval = 0
    private(set) var failure: BackendFailure?
    private(set) var failureID: UUID?
    var allowed = true {
        didSet { if !allowed { stop() } }
    }

    @ObservationIgnored private var player: AVAudioPlayer?
    @ObservationIgnored private var progress: Task<Void, Never>?
    @ObservationIgnored private var interruptionObserver: NSObjectProtocol?
    @ObservationIgnored private var routeObserver: NSObjectProtocol?
    @ObservationIgnored private let session: CallPlaybackSession
    @ObservationIgnored private let notifications: NotificationCenter
    @ObservationIgnored private var activation: Task<Void, Error>?
    @ObservationIgnored private var preparation: Task<Void, Never>?
    @ObservationIgnored private var release: Task<Void, Never>?
    @ObservationIgnored private var generation = UUID()

    init(session: CallPlaybackSession = .shared, notifications: NotificationCenter = .default) {
        self.session = session
        self.notifications = notifications
        super.init()
    }

    // Explicit stop handles page/call transitions; also release observers and
    // the audio session if the library itself is discarded while playing.
    isolated deinit {
        let player = player
        resetPlayback()
        if let activation {
            activation.cancel()
            // Cleanup outlives this player; there is no UI left to report errors to.
            _ = session.deactivate(stopping: player)
        }
    }

    @discardableResult
    func play(_ entry: CallRecording, url: URL) -> Task<Void, Never>? {
        guard allowed, entry.complete else { return nil }
        if selectedID == entry.id {
            resume()
            return preparation
        }
        stop()
        clearFailure()
        guard FileManager.default.fileExists(atPath: url.path) else {
            fail("calls.recordingFileMissing", id: entry.id)
            return nil
        }
        do {
            let player = try AVAudioPlayer(contentsOf: url)
            guard player.duration.isFinite, player.duration > 0 else {
                fail("calls.recordingEmpty", id: entry.id)
                return nil
            }
            self.player = player
            selectedID = entry.id
            duration = player.duration
            player.delegate = self
            state = .preparing
            observeInterruptions()
            let current = generation
            let activation = session.activate(preparing: player)
            self.activation = activation
            preparation = Task { [weak self] in
                do {
                    try await activation.value
                    guard let self, self.generation == current else { return }
                    self.state = .paused
                    self.resume()
                } catch {
                    guard let self, self.generation == current else { return }
                    self.fail("calls.recordingPlaybackFailed", id: entry.id)
                }
            }
        } catch { fail("calls.recordingPlaybackFailed", id: entry.id) }
        return preparation
    }

    func pause() {
        guard state == .playing, let player else { return }
        player.pause()
        position = player.currentTime
        state = .paused
        progress?.cancel(); progress = nil
    }

    func resume() {
        guard allowed, state == .paused, let player, let id = selectedID else { return }
        if position >= duration { player.currentTime = 0; position = 0 }
        guard player.play() else { fail("calls.recordingPlaybackFailed", id: id); return }
        state = .playing
        clearFailure()
        updateProgress()
    }

    func seek(to time: TimeInterval) {
        guard allowed, state != .preparing, time.isFinite, let player else { return }
        let target = min(duration, max(0, time))
        if target == duration, state == .playing { stop(); return }
        player.currentTime = target
        // AVAudioPlayer can wrap currentTime to zero at EOF, even while paused.
        position = target
    }

    @discardableResult
    func stop() -> Task<Void, Never>? {
        let id = selectedID
        let player = player
        resetPlayback()
        guard let activation else { return release }
        // Cancellation skips an activation that has not started. An in-flight
        // system operation must finish before its matching deactivation.
        activation.cancel()
        self.activation = nil
        let current = generation
        let deactivation = session.deactivate(stopping: player)
        release = Task { [weak self] in
            do { try await deactivation.value }
            catch {
                guard let self, self.generation == current else { return }
                self.failureID = id
                self.failure = .local("calls.recordingPlaybackFailed")
            }
        }
        return release
    }

    private func resetPlayback() {
        generation = UUID()
        progress?.cancel(); progress = nil
        if let interruptionObserver { notifications.removeObserver(interruptionObserver) }
        if let routeObserver { notifications.removeObserver(routeObserver) }
        interruptionObserver = nil; routeObserver = nil
        // Preparation has exclusive access on the session queue. Once ready,
        // pause immediately; the queued stop releases hardware without blocking UI.
        if state != .preparing { player?.pause() }
        player = nil
        selectedID = nil; state = .idle; position = 0; duration = 0
        preparation = nil
    }

    private func fail(_ key: String, id: UUID) {
        stop()
        failureID = id
        failure = .local(key)
    }

    private func clearFailure() { failure = nil; failureID = nil }

    private func updateProgress() {
        progress?.cancel()
        progress = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
                guard let self, let player = self.player, self.state == .playing else { return }
                self.position = player.currentTime
            }
        }
    }

    private func observeInterruptions() {
        // A queued notification must not stop a new playback, even of the same file.
        let current = generation
        interruptionObserver = notifications.addObserver(
            forName: AVAudioSession.interruptionNotification, object: nil, queue: .main
        ) { [weak self] notification in
            guard let type = notification.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt,
                  type == AVAudioSession.InterruptionType.began.rawValue else { return }
            Task { @MainActor [weak self] in
                guard let self, self.generation == current else { return }
                self.stop()
            }
        }
        routeObserver = notifications.addObserver(
            forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main
        ) { [weak self] notification in
            guard let reason = notification.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt,
                  reason == AVAudioSession.RouteChangeReason.oldDeviceUnavailable.rawValue else { return }
            Task { @MainActor [weak self] in
                guard let self, self.generation == current else { return }
                self.stop()
            }
        }
    }

    func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        guard self.player === player, let id = selectedID else { return }
        if flag { stop() } else { fail("calls.recordingPlaybackFailed", id: id) }
    }

    func audioPlayerDecodeErrorDidOccur(_ player: AVAudioPlayer, error: Error?) {
        guard self.player === player, let id = selectedID else { return }
        fail("calls.recordingPlaybackFailed", id: id)
    }
}

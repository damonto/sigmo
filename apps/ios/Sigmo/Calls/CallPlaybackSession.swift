import AVFAudio
import Foundation

/// Serializes playback's session changes, including cleanup after a player is released.
@MainActor
final class CallPlaybackSession {
    static let shared = CallPlaybackSession()

    private let update: @Sendable (Bool) async throws -> Void
    private var pending: Task<Void, Error>?
    private var active = false
    private nonisolated static let queue = DispatchQueue(label: "sigmo.playback-session")

    init(update: @escaping @Sendable (Bool) async throws -> Void = CallPlaybackSession.updateSystemSession) {
        self.update = update
    }

    func activate(preparing player: AVAudioPlayer? = nil) -> Task<Void, Error> {
        let audio = player.map(Audio.init)
        return enqueue {
            try Task.checkCancellation()
            if !self.active {
                try await self.update(true)
                self.active = true
            }
            // Record activation before checking cancellation so queued cleanup
            // can balance it, but skip hardware preparation for an obsolete request.
            try Task.checkCancellation()
            try await audio?.prepare()
        }
    }

    func deactivate(stopping player: AVAudioPlayer? = nil) -> Task<Void, Error> {
        let audio = player.map(Audio.init)
        return enqueue {
            // Cleanup must finish even if its caller is cancelled.
            await audio?.stop()
            guard self.active else { return }
            // AVAudioSession can become inactive even when deactivation throws.
            self.active = false
            try await self.update(false)
        }
    }

    private func enqueue(_ operation: @escaping @MainActor () async throws -> Void) -> Task<Void, Error> {
        let previous = pending
        let task = Task {
            // A failed operation must not prevent the next request from running.
            _ = await previous?.result
            try await operation()
        }
        pending = task
        return task
    }

    // The main actor lends the player exclusively to this queue while preparing
    // or after stopping. UI controls cannot access it during either transition.
    // AVAudioPlayer.play() otherwise activates synchronously, even when the
    // session is already active; preparing here avoids that main-thread work.
    private struct Audio: @unchecked Sendable {
        let player: AVAudioPlayer
        init(_ player: AVAudioPlayer) { self.player = player }

        func prepare() async throws {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                queue.async { [self] in
                    if player.prepareToPlay() { continuation.resume() }
                    else { continuation.resume(throwing: BackendFailure.local("calls.recordingPlaybackFailed")) }
                }
            }
        }

        func stop() async {
            await withCheckedContinuation { continuation in
                queue.async { [self] in
                    player.delegate = nil
                    player.stop()
                    continuation.resume()
                }
            }
        }
    }

    private nonisolated static func updateSystemSession(_ active: Bool) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            queue.async {
                do {
                    let session = AVAudioSession.sharedInstance()
                    // A previous call used voiceChat; recordings need the default mode.
                    if active { try session.setCategory(.playback, mode: .default) }
                    if #available(iOS 27.0, *) {
                        let completion: @Sendable (Bool, Error?) -> Void = { success, error in
                            if let error { continuation.resume(throwing: error) }
                            else if success { continuation.resume() }
                            else { continuation.resume(throwing: BackendFailure.local("calls.recordingPlaybackFailed")) }
                        }
                        if active { session.activate(options: [], completionHandler: completion) }
                        else { session.deactivate(options: .notifyOthersOnDeactivation, completionHandler: completion) }
                    } else {
                        // iOS 26 has only the blocking API. Keep it off the main
                        // actor and Swift's cooperative executor.
                        try session.setActive(active, options: active ? [] : .notifyOthersOnDeactivation)
                        continuation.resume()
                    }
                } catch { continuation.resume(throwing: error) }
            }
        }
    }
}

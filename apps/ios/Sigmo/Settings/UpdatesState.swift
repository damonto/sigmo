import Foundation
import Observation

@MainActor @Observable
final class UpdatesState {
    let session: BackendSession
    private(set) var snapshot: UpdateSnapshot?
    private(set) var failure: BackendFailure?
    private(set) var working = false
    private(set) var recovering = false
    private(set) var timedOut = false
    @ObservationIgnored private let clock: BackendClock
    @ObservationIgnored private var revision = 0

    init(session: BackendSession, clock: BackendClock = BackendClock()) {
        self.session = session
        self.clock = clock
    }

    var shouldPoll: Bool {
        !timedOut && (recovering || snapshot?.busy == true)
    }

    var canCheck: Bool {
        snapshot != nil && !working && snapshot?.busy == false && !recovering
    }

    var canInstall: Bool {
        canCheck && snapshot?.selfUpdateSupported == true && snapshot?.updateAvailable == true
    }

    func load() async {
        guard !working, !Task.isCancelled else { return }
        timedOut = false
        await perform { current in
            let snapshot = try await self.session.protectedRequest { api, token in
                try await api.updates(token: token)
            }
            try self.check(current)
            self.snapshot = snapshot
            self.recovering = false
        }
    }

    func save(automatic: Bool, channel: String) async {
        guard let snapshot, canCheck else { return }
        guard snapshot.selfUpdateSupported || automatic == snapshot.settings.automatic,
              channel == "stable" || (snapshot.current.edition == "pro" && channel == "dev") else { return }
        let channelChanged = snapshot.settings.channel != channel
        let preferences = UpdatePreferences(automatic: automatic, channel: channel)
        await perform { current in
            let saved = try await self.session.protectedRequest { api, token in
                try await api.saveUpdates(preferences, token: token)
            }
            try self.check(current)
            self.snapshot = saved
            if channelChanged {
                let checked = try await self.session.protectedRequest { api, token in
                    try await api.checkUpdates(token: token)
                }
                try self.check(current)
                self.snapshot = checked
            }
        }
    }

    func check() async {
        guard canCheck else { return }
        await perform { current in
            let snapshot = try await self.session.protectedRequest { api, token in
                try await api.checkUpdates(token: token)
            }
            try self.check(current)
            self.snapshot = snapshot
        }
    }

    func install() async {
        guard canInstall else { return }
        await perform { current in
            do {
                let snapshot = try await self.session.protectedRequest { api, token in
                    try await api.installUpdate(token: token)
                }
                try self.check(current)
                self.snapshot = snapshot
            } catch {
                try self.check(current)
                guard Self.isRestartInterruption(error) else { throw error }
                self.recovering = true
            }
        }
    }

    // The visible Updates page owns this loop. Re-entry reads the server before
    // observing again; it never repeats an installation POST to recover a reply.
    func observe() async {
        let current = revision
        var restartStarted: Date?
        var delay = 1
        while session.foreground, current == revision, shouldPoll, !Task.isCancelled {
            do {
                try await clock.sleep(.seconds(recovering ? delay : 1))
                try check(current)
                guard session.foreground else { return }
                if recovering {
                    if restartStarted == nil { restartStarted = clock.now() }
                    if let restartStarted, clock.now().timeIntervalSince(restartStarted) >= 120 {
                        timedOut = true
                        failure = .local("settings.native.restartTimeout")
                        return
                    }
                    _ = try await session.protectedRequest { api, token in try await api.app(token: token) }
                    try check(current)
                }
                let next = try await session.protectedRequest { api, token in
                    try await api.installation(token: token)
                }
                try check(current)
                if next.state == "idle" {
                    // Publishing idle cancels the view's polling task, so refresh
                    // backend information before publishing the final snapshot.
                    await session.refreshAfterSettingsChange()
                    try check(current)
                    snapshot = next
                    recovering = false
                    failure = nil
                    return
                }
                snapshot = next
                recovering = next.state == "restarting"
                failure = nil
                if next.state == "failed" { return }
                if !recovering {
                    restartStarted = nil
                    delay = 1
                }
            } catch {
                guard current == revision else { return }
                if !session.canAccessDevices { clear() }
                guard !Task.isCancelled, !(error is CancellationError) else { return }
                guard Self.isRestartInterruption(error) else {
                    failure = .wrapping(error)
                    timedOut = true
                    return
                }
                delay = recovering ? min(delay * 2, 5) : 1
                recovering = true
                if restartStarted == nil { restartStarted = clock.now() }
            }
        }
    }

    func clear() {
        revision += 1
        working = false
        snapshot = nil
        failure = nil
        recovering = false
        timedOut = false
    }

    private static func isRestartInterruption(_ error: Error) -> Bool {
        let failure = BackendFailure.wrapping(error)
        if let code = failure.transportCode {
            switch code {
            case .notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost,
                 .cannotFindHost, .dnsLookupFailed, .timedOut:
                return true
            default:
                return false
            }
        }
        return [502, 503, 504].contains(failure.status ?? 0)
    }

    private func check(_ current: Int) throws {
        try Task.checkCancellation()
        guard current == revision else { throw CancellationError() }
    }

    private func perform(_ operation: (Int) async throws -> Void) async {
        guard !working, !Task.isCancelled else { return }
        let current = revision
        working = true
        failure = nil
        defer { if current == revision { working = false } }
        do {
            try await operation(current)
        } catch {
            guard current == revision else { return }
            if !session.canAccessDevices { clear() }
            guard !Task.isCancelled, !(error is CancellationError) else { return }
            failure = .wrapping(error)
        }
    }
}

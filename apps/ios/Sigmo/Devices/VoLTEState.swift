import Foundation
import Observation

@MainActor @Observable
final class VoLTEState {
    let scope: ProfileConnectionScope
    private(set) var settings: VoLTESettings?
    private(set) var loading = false
    private(set) var updating = false
    private(set) var loadFailure: BackendFailure?
    private(set) var observedAt = Date()
    var failure: BackendFailure?
    @ObservationIgnored private var readRevision = 0
    @ObservationIgnored private var readInFlight: Int?

    init(detail: ModemDetailState, profileID: String?) {
        scope = ProfileConnectionScope(detail: detail, profileID: profileID, capability: "volte")
    }

    var canChange: Bool { scope.radioAvailable && settings?.knownDataPath == true && loadFailure == nil && !updating && !scope.detail.busy }
    var canChangeDataPath: Bool { canChange && settings?.enabled == false && settings?.dataPath != "mbim" }

    func load(reconciling: Bool = false, refreshModem: Bool = true) async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, reconciling || (!updating && !scope.detail.busy) else { return }
        guard reconciling || readInFlight == nil else { return }
        readRevision += 1
        let revision = readRevision
        readInFlight = revision
        loading = settings == nil
        defer {
            if readInFlight == revision { readInFlight = nil }
            if revision == readRevision { loading = false }
            clearIfUnavailable()
        }
        do {
            if refreshModem { try await scope.refreshModem() }
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.volteSettings(id, token: token) }
            guard revision == readRevision else { return }
            settings = value
            observedAt = Date()
            loadFailure = nil
        } catch {
            guard revision == readRevision, scope.available, !Task.isCancelled else { return }
            loadFailure = .wrapping(error)
        }
    }

    func reload() async {
        defer { clearIfUnavailable() }
        guard scope.available, !Task.isCancelled, readInFlight == nil else { return }
        await load()
        guard !Task.isCancelled else { return }
        failure = loadFailure
    }

    func observePending(interval: Duration = .seconds(2)) async {
        await scope.observePending(interval: interval, needsRefresh: {
            self.readInFlight == nil && !self.updating && !self.scope.detail.busy
                && self.settings?.enabled == true && self.settings?.state == "connecting"
        }, refresh: { await self.reload() })
    }

    func setEnabled(_ enabled: Bool) async {
        guard canChange, let settings, settings.enabled != enabled else { return }
        await update(.init(enabled: enabled, dataPath: settings.dataPath == "mbim" ? nil : settings.dataPath))
    }

    func selectDataPath(_ path: VoLTEDataPath) async {
        guard canChangeDataPath, settings?.dataPath != path.rawValue else { return }
        await update(.init(enabled: false, dataPath: path.rawValue))
    }

    private func update(_ value: VoLTESubmission) async {
        readRevision += 1
        loading = false
        updating = true
        failure = nil
        defer { updating = false; clearIfUnavailable() }
        var writeFailure: BackendFailure?
        do {
            let id = scope.modemID
            try await scope.write { api, token in try await api.updateVoLTE(id, settings: value, token: token) }
        } catch {
            guard scope.available, !Task.isCancelled else { return }
            writeFailure = .wrapping(error)
        }
        await load(reconciling: true, refreshModem: writeFailure != nil)
        guard scope.available, !Task.isCancelled else { return }
        failure = writeFailure ?? loadFailure
    }

    private func clearIfUnavailable() {
        guard !scope.available else { return }
        readRevision += 1
        readInFlight = nil
        settings = nil
        loadFailure = nil
        failure = nil
        loading = false
    }
}

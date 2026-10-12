import Foundation
import Observation

@MainActor @Observable
final class InternetState {
    let scope: ProfileConnectionScope
    private(set) var connection: DetailInternet?
    var draft = InternetPreferences.automatic
    private(set) var loading = false
    private(set) var updating = false
    private(set) var loadFailure: BackendFailure?
    private(set) var publicInfo: InternetPublicInfo?
    private(set) var publicFailure: BackendFailure?
    private(set) var publicLoading = false
    private(set) var observedAt = Date()
    var failure: BackendFailure?
    @ObservationIgnored private var readRevision = 0
    @ObservationIgnored private var publicRevision = 0
    @ObservationIgnored private var publicConnectionID: String?
    @ObservationIgnored private var draftBaseline: InternetPreferences?

    init(detail: ModemDetailState, profileID: String) {
        scope = ProfileConnectionScope(detail: detail, profileID: profileID)
    }

    var connected: Bool { connection?.connected == true }
    var busy: Bool { updating || scope.detail.busy }
    var canChange: Bool { scope.radioAvailable && connection?.knownStatus == true && loadFailure == nil && !busy }
    var canEditAPN: Bool { scope.available && connection?.knownStatus == true && loadFailure == nil && !busy && !connected }
    var canEditOptions: Bool { canEditAPN || canChange }
    var options: InternetConnectionOptions { connected ? connection?.preferences.connectionOptions ?? draft.connectionOptions : draft.connectionOptions }
    var statusKey: String {
        guard let connection, connection.knownStatus else { return "common.unknown" }
        return connection.connected ? "common.connected" : "common.disconnected"
    }

    func load(reconciling: Bool = false, acceptPreferences: Bool = false) async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, reconciling || !busy else { return }
        readRevision += 1
        let revision = readRevision
        loading = connection == nil
        defer { if revision == readRevision { loading = false }; clearIfUnavailable() }
        do {
            try await scope.refreshModem()
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.detailInternet(id, token: token) }
            guard revision == readRevision else { return }
            // Refreshes can update counters while an offline APN/options draft remains untouched.
            if acceptPreferences || draftBaseline == nil || draft == draftBaseline {
                draft = value.preferences
                draftBaseline = value.preferences
            }
            if connection?.connectionID != value.connectionID || !value.connected {
                publicRevision += 1
                publicInfo = nil
                publicFailure = nil
                publicConnectionID = nil
                publicLoading = false
            }
            connection = value
            observedAt = Date()
            loadFailure = nil
        } catch {
            guard revision == readRevision, scope.available, !Task.isCancelled else { return }
            loadFailure = .wrapping(error)
        }
    }

    func reload() async {
        defer { clearIfUnavailable() }
        guard scope.available, !Task.isCancelled else { return }
        await load()
        guard !Task.isCancelled else { return }
        failure = loadFailure
        if connected, publicConnectionID == nil, loadFailure == nil, !busy { await loadPublic() }
    }

    func loadPublic() async {
        guard scope.available, !Task.isCancelled, !publicLoading, let key = connection?.connectionID else { return }
        publicRevision += 1
        let revision = publicRevision
        publicLoading = true
        defer { if revision == publicRevision { publicLoading = false }; clearIfUnavailable() }
        do {
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.internetPublicInfo(id, token: token) }
            guard revision == publicRevision, connection?.connectionID == key else { return }
            publicInfo = value
            publicConnectionID = key
            publicFailure = nil
        } catch {
            guard revision == publicRevision, scope.available, !Task.isCancelled, connection?.connectionID == key else { return }
            publicFailure = .wrapping(error)
            failure = publicFailure
        }
    }

    func setConnected(_ enabled: Bool) async {
        guard canChange, enabled != connected else { return }
        if enabled, !draft.valid { failure = .local("connection.invalidAPN"); return }
        let id = scope.modemID
        let preferences = draft.submission
        await change(acceptPreferences: enabled) { api, token in
            if enabled {
                _ = try await api.connectInternet(id, preferences: preferences, token: token)
            } else {
                try await api.disconnectInternet(id, token: token)
            }
        }
    }

    func updateOptions(_ options: InternetConnectionOptions) async {
        guard canEditOptions else { return }
        if !connected {
            draft.defaultRoute = options.defaultRoute
            draft.proxyEnabled = options.proxyEnabled
            draft.alwaysOn = options.alwaysOn
            return
        }
        let id = scope.modemID
        await change { api, token in _ = try await api.internetOptions(id, value: options, token: token) }
    }

    private func change(acceptPreferences: Bool = false,
                        _ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Void) async {
        readRevision += 1
        publicRevision += 1
        publicLoading = false
        loading = false
        updating = true
        failure = nil
        defer { updating = false; clearIfUnavailable() }
        var writeFailure: BackendFailure?
        do { try await scope.write(operation) }
        catch {
            guard scope.available, !Task.isCancelled else { return }
            writeFailure = .wrapping(error)
        }
        await load(reconciling: true, acceptPreferences: acceptPreferences && writeFailure == nil)
        guard scope.available, !Task.isCancelled else { return }
        failure = writeFailure ?? loadFailure
    }

    private func clearIfUnavailable() {
        guard !scope.available else { return }
        readRevision += 1
        publicRevision += 1
        connection = nil
        draft = .automatic
        draftBaseline = nil
        publicConnectionID = nil
        publicInfo = nil
        publicFailure = nil
        loadFailure = nil
        failure = nil
        loading = false
        publicLoading = false
    }
}

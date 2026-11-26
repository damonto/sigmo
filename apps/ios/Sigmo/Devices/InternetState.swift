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
    private(set) var speedTestFailure: BackendFailure?
    private(set) var speedTestReadFailed = false
    private(set) var speedTesting = false
    private(set) var observedAt = Date()
    var failure: BackendFailure?
    @ObservationIgnored private var readRevision = 0
    @ObservationIgnored private var publicRevision = 0
    @ObservationIgnored private var publicSessionID: String?
    @ObservationIgnored private var draftBaseline: InternetPreferences?
    private var speedTestStatus: InternetSpeedTestStatus?
    private var speedTestLoading = false

    init(detail: ModemDetailState, profileID: String?) {
        scope = ProfileConnectionScope(detail: detail, profileID: profileID)
    }

    var connected: Bool { connection?.connected == true }
    var busy: Bool { updating || scope.detail.busy }
    var canChange: Bool { scope.radioAvailable && connection?.knownStatus == true && loadFailure == nil && !busy }
    var canEditAPN: Bool { scope.available && connection?.knownStatus == true && loadFailure == nil && !busy && !connected }
    var canEditOptions: Bool { canEditAPN || canChange }
    var speedTest: InternetSpeedTestResult? { speedTestStatus?.result }
    var canRunSpeedTest: Bool {
        connected && canChange && speedTestStatus != nil && !speedTestReadFailed && !speedTestLoading && !speedTesting
    }
    var options: InternetConnectionOptions { connected ? connection?.preferences.connectionOptions ?? draft.connectionOptions : draft.connectionOptions }
    var status: ConnectionStatus {
        guard let connection else { return loadFailure == nil ? .loading : .unknown }
        guard connection.knownStatus else { return .unknown }
        return connection.connected ? .connected : .disconnected
    }

    func load(reconciling: Bool = false, acceptPreferences: Bool = false, refreshModem: Bool = true) async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, reconciling || !busy else { return }
        loading = true
        readRevision += 1
        await loadSnapshot(revision: readRevision, acceptPreferences: acceptPreferences, refreshModem: refreshModem)
    }

    func reload() async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, !busy, !loading else { return }
        loading = true
        readRevision += 1
        let revision = readRevision
        await loadSnapshot(revision: revision)
        guard revision == readRevision, !Task.isCancelled else { return }
        failure = loadFailure
        if connected, publicSessionID == nil, loadFailure == nil, !busy { await loadPublic() }
    }

    private func loadSnapshot(revision: Int, acceptPreferences: Bool = false, refreshModem: Bool = true) async {
        defer {
            if revision == readRevision { loading = false }
            clearIfUnavailable()
        }
        guard revision == readRevision else { return }
        do {
            if refreshModem { try await scope.refreshModem() }
            guard revision == readRevision else { return }
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.detailInternet(id, token: token) }
            guard revision == readRevision else { return }
            // Refreshes can update counters while an offline APN/options draft remains untouched.
            if acceptPreferences || draftBaseline == nil || draft == draftBaseline {
                draft = value.preferences
                draftBaseline = value.preferences
            }
            if connection?.sessionID != value.sessionID || !value.connected {
                publicRevision += 1
                publicInfo = nil
                publicFailure = nil
                publicSessionID = nil
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

    func loadPublic() async {
        guard scope.available, !Task.isCancelled, !publicLoading, let key = connection?.sessionID,
              key != publicSessionID else { return }
        publicRevision += 1
        let revision = publicRevision
        publicLoading = true
        defer { if revision == publicRevision { publicLoading = false }; clearIfUnavailable() }
        do {
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.internetPublicInfo(id, token: token) }
            guard revision == publicRevision, connection?.sessionID == key else { return }
            publicInfo = value
            publicSessionID = key
            publicFailure = nil
        } catch {
            guard revision == publicRevision, scope.available, !Task.isCancelled, connection?.sessionID == key else { return }
            publicFailure = .wrapping(error)
        }
    }

    func loadSpeedTest() async {
        guard scope.available, !Task.isCancelled, !speedTestLoading, !speedTesting,
              let iccid = scope.detail.simIdentity?.iccid, !iccid.isEmpty else { return }
        speedTestLoading = true
        defer { speedTestLoading = false }
        do {
            let id = scope.modemID
            let status = try await scope.read { api, token in try await api.latestInternetSpeedTest(id, token: token) }
            speedTestStatus = status.iccid == iccid ? status : nil
            speedTestReadFailed = false
        } catch {
            guard scope.available, !Task.isCancelled else { return }
            speedTestReadFailed = true
        }
    }

    func runSpeedTest(size: InternetSpeedTestSize) async {
        guard canRunSpeedTest, !Task.isCancelled, let iccid = scope.detail.simIdentity?.iccid else { return }
        speedTesting = true
        speedTestFailure = nil
        do {
            let id = scope.modemID
            // A speed test is cancellable and does not mutate the connection settings.
            let status = try await scope.read { api, token in
                try await api.runInternetSpeedTest(id, iccid: iccid, size: size, token: token)
            }
            guard status.iccid == iccid, status.result != nil else { throw BackendFailure.local("api.invalidResponse") }
            speedTestStatus = status
        } catch {
            if scope.available, !Task.isCancelled { speedTestFailure = .wrapping(error) }
        }
        speedTesting = false
        if speedTestFailure != nil { await loadSpeedTest() }
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
        await load(reconciling: true, acceptPreferences: acceptPreferences && writeFailure == nil, refreshModem: writeFailure != nil)
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
        publicSessionID = nil
        publicInfo = nil
        publicFailure = nil
        loadFailure = nil
        failure = nil
        loading = false
        publicLoading = false
        speedTestStatus = nil
        speedTestFailure = nil
        speedTestReadFailed = false
    }
}

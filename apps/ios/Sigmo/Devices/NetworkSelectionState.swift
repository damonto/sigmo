import Foundation
import Observation

@MainActor @Observable
final class NetworkSelectionState {
    let scope: ProfileConnectionScope
    private(set) var registration: NetworkRegistration?
    private(set) var networks: [NetworkOperator] = []
    private(set) var loading = false
    private(set) var scanning = false
    private(set) var updating = false
    private(set) var pendingOperator: String?
    private(set) var loadFailure: BackendFailure?
    private(set) var scanFailure: BackendFailure?
    var failure: BackendFailure?
    private var browsingManual = false
    private var scanned = false
    @ObservationIgnored private var readRevision = 0
    @ObservationIgnored private var scanRevision = 0

    init(detail: ModemDetailState, profileID: String?) {
        scope = ProfileConnectionScope(detail: detail, profileID: profileID)
    }

    var manual: Bool { browsingManual || registration?.automatic == false }
    var canChange: Bool {
        scope.radioAvailable && registration?.automatic != nil && loadFailure == nil && !updating && !scope.detail.busy
    }
    var selectedOperator: String? {
        registration?.automatic == false ? registration?.operatorCode : scope.detail.modem?.registeredOperator.code
    }

    func load(reconciling: Bool = false) async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, reconciling || (!updating && !scope.detail.busy) else { return }
        readRevision += 1
        let revision = readRevision
        loading = registration == nil
        defer { if revision == readRevision { loading = false }; clearIfUnavailable() }
        do {
            try await scope.refreshModem()
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.networkRegistration(id, token: token) }
            guard revision == readRevision else { return }
            registration = value
            loadFailure = value.automatic == nil ? .local("connection.unknownSelectionMode") : nil
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
        if manual, !scanned, canChange { await scan() }
    }

    func setAutomatic(_ automatic: Bool) async {
        guard canChange else { return }
        if !automatic {
            browsingManual = true
            await scan()
            return
        }
        stopScan()
        if registration?.automatic == true {
            browsingManual = false
            return
        }
        await apply(.init(mode: "automatic", operatorCode: nil))
    }

    func select(_ network: NetworkOperator) async {
        guard canChange, manual, !scanning, networks.contains(where: { $0.id == network.id }) else { return }
        await apply(.init(mode: "manual", operatorCode: network.operatorCode))
    }

    private func apply(_ value: NetworkRegistration) async {
        readRevision += 1
        stopScan()
        loading = false
        updating = true
        pendingOperator = value.operatorCode
        failure = nil
        defer { updating = false; pendingOperator = nil; clearIfUnavailable() }
        var writeFailure: BackendFailure?
        do {
            let id = scope.modemID
            try await scope.write { api, token in try await api.setNetworkRegistration(id, value: value, token: token) }
            browsingManual = false
        } catch {
            guard scope.available, !Task.isCancelled else { return }
            writeFailure = .wrapping(error)
        }
        // Even a timed-out write may have reached the modem. Read before offering another action.
        await load(reconciling: true)
        guard scope.available, !Task.isCancelled else { return }
        failure = writeFailure ?? loadFailure
    }

    func scan() async {
        guard canChange, manual, !scanning else { return }
        scanRevision += 1
        let revision = scanRevision
        scanning = true
        scanFailure = nil
        defer { if revision == scanRevision { scanning = false }; clearIfUnavailable() }
        do {
            try await scope.refreshModem()
            guard scope.radioAvailable else { throw BackendFailure.local("connection.airplaneHint") }
            let id = scope.modemID
            var scan = try await scope.read { api, token in try await api.startNetworkScan(id, token: token) }
            guard !scan.id.isEmpty else { throw BackendFailure.local("api.invalidResponse") }
            let scanID = scan.id
            let deadline = ContinuousClock.now.advanced(by: .seconds(300))
            var polls = 0
            while scan.status == "running" {
                guard revision == scanRevision else { return }
                guard ContinuousClock.now < deadline else { throw BackendFailure.local("connection.scanTimeout") }
                try await Task.sleep(for: .seconds(1))
                guard revision == scanRevision else { return }
                if polls % 5 == 0 { try await scope.refreshModem() }
                guard scope.radioAvailable else { throw BackendFailure.local("connection.airplaneHint") }
                scan = try await scope.read { api, token in try await api.networkScan(id, id: scanID, token: token) }
                guard scan.id == scanID else { throw BackendFailure.local("api.invalidResponse") }
                polls += 1
            }
            guard revision == scanRevision else { return }
            guard scan.status == "completed" else {
                let key = scan.errorCode == "network_scan_timeout" ? "connection.scanTimeout" : "connection.scanFailed"
                throw BackendFailure.local(key)
            }
            networks = NetworkOperator.merged(scan.networks ?? [])
            scanned = true
        } catch {
            guard revision == scanRevision, scope.available, !Task.isCancelled else { return }
            scanFailure = .wrapping(error)
            failure = scanFailure
        }
    }

    private func stopScan() {
        scanRevision += 1
        scanning = false
    }

    func leave() {
        readRevision += 1
        stopScan()
        loading = false
        browsingManual = false
        scanned = false
        networks = []
        scanFailure = nil
        clearIfUnavailable()
    }

    private func clearIfUnavailable() {
        guard !scope.available else { return }
        readRevision += 1
        scanRevision += 1
        registration = nil
        networks = []
        loadFailure = nil
        scanFailure = nil
        failure = nil
        scanning = false
        loading = false
        browsingManual = false
        scanned = false
    }
}

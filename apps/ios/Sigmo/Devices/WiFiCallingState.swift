import Foundation
import Observation

struct WiFiUplink: Identifiable {
    let underlay: WiFiUnderlay
    let name: String
    let available: Bool
    var id: WiFiUnderlay { underlay }
}

struct ConnectionWebsheet: Identifiable, Sendable {
    let sheet: CarrierWebsheet
    let url: URL
    var id: String { sheet.id }
}

@MainActor @Observable
final class WiFiCallingState {
    let scope: ProfileConnectionScope
    private(set) var settings: DetailWiFiSettings?
    private(set) var loading = false
    private(set) var updating = false
    private(set) var loadFailure: BackendFailure?
    private(set) var observedAt = Date()
    private(set) var websheet: ConnectionWebsheet?
    private(set) var sendingCallback = false
    var failure: BackendFailure?
    @ObservationIgnored private var readRevision = 0
    @ObservationIgnored private var presentationRevision = 0
    @ObservationIgnored private var showSetupAfterEnable = false
    @ObservationIgnored private var callbacks: Set<CarrierCallback> = []

    init(detail: ModemDetailState, profileID: String) {
        scope = ProfileConnectionScope(detail: detail, profileID: profileID, capability: "wifiCalling")
    }

    var busy: Bool { updating || scope.detail.busy || sendingCallback }
    var canChange: Bool { scope.available && settings != nil && loadFailure == nil && !busy }
    var setupRequired: Bool { settings?.state == "websheet_required" || settings?.websheet != nil }
    var uplinks: [WiFiUplink] {
        var options = [WiFiUplink(underlay: .system, name: String(localized: "connection.systemNetwork"), available: true)]
        let modem = scope.detail.modem
        if modem?.internetConnected == true || settings?.underlay.mode == "self" {
            options.append(.init(underlay: .current, name: String(localized: "connection.currentDevice"), available: modem?.internetConnected == true))
        }
        for modem in scope.detail.session.modems where modem.id != scope.modemID && modem.internetConnected == true {
            options.append(.init(underlay: .init(mode: "modem", modemId: modem.id), name: modem.displayName, available: true))
        }
        if let selected = settings?.underlay, !options.contains(where: { $0.underlay == selected }) {
            let name = scope.detail.session.modems.first { $0.id == selected.modemId }?.displayName ?? selected.modemId ?? selected.mode
            options.append(.init(underlay: selected, name: name, available: false))
        }
        return options
    }
    var uplinkName: String { uplinks.first { $0.underlay == settings?.underlay }?.name ?? String(localized: "common.unknown") }

    func load(reconciling: Bool = false) async {
        clearIfUnavailable()
        guard scope.available, !Task.isCancelled, reconciling || !busy else { return }
        readRevision += 1
        let revision = readRevision
        loading = settings == nil
        defer { if revision == readRevision { loading = false }; clearIfUnavailable() }
        do {
            try await scope.refreshModem()
            let id = scope.modemID
            let value = try await scope.read { api, token in try await api.detailWiFi(id, token: token) }
            guard revision == readRevision else { return }
            settings = value
            observedAt = Date()
            loadFailure = nil
            if !value.enabled || value.connected { showSetupAfterEnable = false }
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
        await presentSetupIfNeeded()
    }

    func setEnabled(_ enabled: Bool) async {
        guard canChange, let settings, settings.enabled != enabled else { return }
        showSetupAfterEnable = enabled
        await update(.init(enabled: enabled, underlay: settings.underlay))
        await presentSetupIfNeeded()
    }

    func selectUplink(_ underlay: WiFiUnderlay) async {
        guard canChange, let settings, settings.underlay != underlay,
              uplinks.contains(where: { $0.available && $0.underlay == underlay }) else { return }
        await update(.init(enabled: settings.enabled, underlay: underlay))
    }

    private func update(_ value: WiFiSubmission) async {
        let id = scope.modemID
        await change { api, token in try await api.updateWiFiCalling(id, settings: value, token: token) }
    }

    func reconnect() async {
        guard canChange, settings?.enabled == true, settings?.state != "connecting" else { return }
        let id = scope.modemID
        await change { api, token in try await api.reconnectWiFiCalling(id, token: token) }
    }

    func disconnect() async {
        guard canChange, settings?.enabled == true else { return }
        let id = scope.modemID
        await change { api, token in try await api.disconnectWiFiCalling(id, token: token) }
    }

    private func change(_ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Void) async {
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
            showSetupAfterEnable = false
        }
        await load(reconciling: true)
        guard scope.available, !Task.isCancelled else { return }
        failure = writeFailure ?? loadFailure
    }

    private func presentSetupIfNeeded() async {
        guard showSetupAfterEnable, setupRequired, !busy, websheet == nil else { return }
        showSetupAfterEnable = false
        await openWebsheet()
    }

    func openWebsheet(emergencyAddress: Bool = false) async {
        guard canChange, websheet == nil,
              emergencyAddress ? settings?.emergencyAddressUpdateAvailable == true : setupRequired else { return }
        updating = true
        failure = nil
        presentationRevision += 1
        let revision = presentationRevision
        defer { updating = false; clearIfUnavailable() }
        do {
            let id = scope.modemID
            let presentation = try await scope.read { api, token in
                let sheet = try await api.wifiCallingWebsheet(id, emergencyAddress: emergencyAddress, token: token)
                return ConnectionWebsheet(sheet: sheet, url: try CarrierWebsheetAddress.url(sheet, api: api, token: token))
            }
            guard revision == presentationRevision else { return }
            callbacks = []
            websheet = presentation
        } catch {
            guard revision == presentationRevision, scope.available, !Task.isCancelled else { return }
            failure = .wrapping(error)
        }
    }

    func closeWebsheet() {
        presentationRevision += 1
        showSetupAfterEnable = false
        websheet = nil
        callbacks = []
    }

    func completeWebsheet(_ callback: CarrierCallback) async {
        guard scope.available, let websheet, !sendingCallback, !callbacks.contains(callback), !Task.isCancelled else { return }
        let revision = presentationRevision
        sendingCallback = true
        failure = nil
        defer { sendingCallback = false; clearIfUnavailable() }
        do {
            try await scope.read { api, token in try await api.carrierCallback(websheet.id, callback: callback, token: token) }
            guard revision == presentationRevision else { return }
            callbacks.insert(callback)
            if Self.terminalCallback(callback) {
                closeWebsheet()
                await load(reconciling: true)
                if !Task.isCancelled { failure = loadFailure }
            }
        } catch {
            guard revision == presentationRevision, scope.available, !Task.isCancelled else { return }
            failure = .wrapping(error)
        }
    }

    // These are the terminal events accepted by the backend's Wi-Fi Calling Websheet flow.
    static func terminalCallback(_ callback: CarrierCallback) -> Bool {
        let terminal: Set<String> = [
            "dismissflow", "cancel", "cancelbuttonclicked", "cancelbuttonpressed", "entitlementchanged",
            "finishflow", "done", "success", "phoneservicesaccountstatuschanged", "closewebview",
            "onclosewebview", "workflowcompleted", "workflowabandoned"
        ]
        return [callback.event, callback.method, callback.resultCode].compactMap { $0 }.contains {
            terminal.contains($0.lowercased().filter { $0.isASCII && ($0.isLetter || $0.isNumber) })
        }
    }

    private func clearIfUnavailable() {
        guard !scope.available else { return }
        readRevision += 1
        closeWebsheet()
        settings = nil
        loadFailure = nil
        failure = nil
        loading = false
    }
}

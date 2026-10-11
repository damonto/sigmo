import SwiftUI

struct WiFiCallingView: View {
    private enum Action { case enabled(Bool), reconnect, disconnect, setup, emergencyAddress }
    private struct PendingCallback: Identifiable {
        let id = UUID()
        let sheetID: String
        let callback: CarrierCallback
    }
    @State private var state: WiFiCallingState
    @State private var action: ConnectionAction<Action>?
    @State private var confirmEnable = false
    @State private var refreshID = 0
    @State private var callbacks: [PendingCallback] = []

    init(detail: ModemDetailState, profileID: String) {
        _state = State(initialValue: WiFiCallingState(detail: detail, profileID: profileID))
    }

    var body: some View {
        form
            .navigationTitle("capability.wifiCalling")
            .modifier(ConnectionLifecycle(scope: state.scope, refreshID: refreshID,
                                          reload: { await state.reload() }, cancelAction: {
                action = nil
                confirmEnable = false
                callbacks = []
                state.closeWebsheet()
            }))
            .task(id: action?.id) { await perform() }
            .task(id: callbacks.first?.id) {
                guard let pending = callbacks.first else { return }
                if state.websheet?.id == pending.sheetID { await state.completeWebsheet(pending.callback) }
                if !Task.isCancelled, callbacks.first?.id == pending.id { callbacks.removeFirst() }
            }
            .refreshable { await state.reload() }
            .alert("connection.enableWiFiCalling", isPresented: $confirmEnable) {
                Button("connection.enable") { action = .init(value: .enabled(true)) }
                    .accessibilityIdentifier("confirmWiFiCalling")
                Button("common.cancel", role: .cancel) {}
            } message: { Text("connection.wifiEnableHint") }
            .sheet(isPresented: Binding(get: { state.websheet != nil }, set: { if !$0 { state.closeWebsheet() } }),
                   onDismiss: { callbacks = []; refreshID += 1 }) {
                if let websheet = state.websheet {
                    NavigationStack {
                        CarrierWebsheetView(url: websheet.url, callback: { callback in
                            callbacks.append(.init(sheetID: websheet.id, callback: callback))
                        }, failure: { state.failure = $0 }, fixtureHTML: carrierFixtureHTML)
                        .id(websheet.id)
                        .navigationTitle(websheet.sheet.title ?? detailText("capability.wifiCalling"))
                        .navigationBarTitleDisplayMode(.inline)
                        .toolbar {
                            ToolbarItem(placement: .cancellationAction) {
                                Button("common.cancel") { state.closeWebsheet() }
                                    .accessibilityIdentifier("closeWiFiWebsheet")
                            }
                        }
                        .detailErrorAlert($state.failure, retry: {
                            if let callback = state.failedCallback {
                                callbacks.append(.init(sheetID: websheet.id, callback: callback))
                            }
                        })
                    }
                    .presentationDetents([.large])
                }
            }
            .detailErrorAlert($state.failure, retry: { refreshID += 1 })
    }

    private var form: some View {
        Form {
            ConnectionReadFailure(failure: state.loadFailure, retry: { refreshID += 1 })
            Section {
                if let settings = state.settings {
                    Toggle("connection.wifiCallingOnLine", isOn: Binding(
                        get: { settings.enabled },
                        set: { enabled in
                            if enabled { confirmEnable = true }
                            else { action = .init(value: .enabled(false)) }
                        }
                    ))
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("wifiCallingEnabled")
                } else { ConnectionInitialValue(loading: state.loadFailure == nil) }
                if state.updating { ConnectionProgress() }
            } footer: { Text("connection.wifiHint") }
            if let settings = state.settings {
                Section {
                    DetailValue(title: "connection.status", value: detailText(settings.statusKey))
                        .accessibilityIdentifier("wifiCallingStatus")
                    ConnectionDurationRow(seconds: settings.durationSeconds, observedAt: state.observedAt,
                                          running: settings.enabled && settings.connected)
                    if settings.enabled, !settings.connected, let error = settings.lastError, !error.isEmpty {
                        Text(error).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                    if settings.enabled {
                        Button("connection.reconnect") { action = .init(value: .reconnect) }
                            .disabled(!state.canChange || action != nil || settings.state == "connecting")
                            .accessibilityIdentifier("wifiCallingReconnect")
                        Button("connection.disconnect") { action = .init(value: .disconnect) }
                            .disabled(!state.canChange || action != nil || settings.state == "disconnected" || settings.state == "idle")
                            .accessibilityIdentifier("wifiCallingDisconnect")
                    }
                }
                if state.setupRequired {
                    Section {
                        Button("connection.continueSetup") { action = .init(value: .setup) }
                            .disabled(!state.canChange || action != nil)
                            .accessibilityIdentifier("wifiCallingSetup")
                    } footer: { Text("connection.setupHint") }
                }
                Section {
                    NavigationLink { WiFiUplinkView(state: state) } label: {
                        LabeledContent("connection.uplink", value: state.uplinkName)
                    }
                    .disabled(state.busy || action != nil)
                    .accessibilityIdentifier("wifiCallingUplink")
                    if settings.emergencyAddressUpdateAvailable {
                        Button("connection.emergencyAddress") { action = .init(value: .emergencyAddress) }
                            .disabled(!state.canChange || action != nil)
                            .accessibilityIdentifier("wifiCallingEmergencyAddress")
                    }
                }
            }
        }
    }

    private func perform() async {
        guard let action else { return }
        switch action.value {
        case .enabled(let enabled): await state.setEnabled(enabled)
        case .reconnect: await state.reconnect()
        case .disconnect: await state.disconnect()
        case .setup: await state.openWebsheet()
        case .emergencyAddress: await state.openWebsheet(emergencyAddress: true)
        }
        if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
    }

    private var carrierFixtureHTML: String? {
        #if DEBUG
        if ProcessInfo.processInfo.environment["SIGMO_UI_SCENARIO"] == "detail-connection-setup" {
            return ConnectionFixture.carrierHTML
        }
        #endif
        return nil
    }
}

private struct WiFiUplinkView: View {
    @Bindable var state: WiFiCallingState
    @State private var action: ConnectionAction<WiFiUnderlay>?
    @State private var refreshID = 0

    var body: some View {
        Form {
            ConnectionReadFailure(failure: state.loadFailure, retry: { refreshID += 1 })
            Section {
                ForEach(state.uplinks) { uplink in
                    Button { action = .init(value: uplink.underlay) } label: {
                        HStack(spacing: 12) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(uplink.name).foregroundStyle(.primary)
                                if !uplink.available { Text("connection.uplinkUnavailable").font(.caption).foregroundStyle(.secondary) }
                            }
                            Spacer(minLength: 8)
                            if state.settings?.underlay == uplink.underlay { Image(systemName: "checkmark").foregroundStyle(.tint) }
                        }
                    }
                    .disabled(!state.canChange || action != nil || !uplink.available)
                    .accessibilityIdentifier("wifiUplink-" + uplink.underlay.mode + (uplink.underlay.modemId.map { "-" + $0 } ?? ""))
                    .accessibilityAddTraits(state.settings?.underlay == uplink.underlay ? .isSelected : [])
                }
            } footer: { Text("connection.uplinkHint") }
            if state.updating { Section { ConnectionProgress() } }
        }
        .navigationTitle("connection.uplink")
        .modifier(ConnectionLifecycle(scope: state.scope, refreshID: refreshID,
                                      reload: { await state.reload() }, cancelAction: { action = nil }))
        .task(id: action?.id) {
            guard let action else { return }
            await state.selectUplink(action.value)
            if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
        }
        .detailErrorAlert($state.failure, retry: { refreshID += 1 })
    }
}

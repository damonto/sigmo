import SwiftUI

struct WiFiCallingView: View {
    private enum Action: Equatable {
        case enabled(Bool), reconnect, disconnect, setup, emergencyAddress
        case uplink(WiFiUnderlay)
    }
    private struct PendingCallback: Identifiable {
        let id = UUID()
        let sheetID: String
        let callback: CarrierCallbackDelivery
    }
    @State private var state: WiFiCallingState
    @State private var action: ConnectionAction<Action>?
    @State private var confirmEnable = false
    @State private var refreshID = 0
    @State private var callbacks: [PendingCallback] = []

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: WiFiCallingState(detail: detail, profileID: profileID))
    }

    var body: some View {
        form
            .navigationTitle("capability.wifiCalling")
            .modifier(ConnectionLifecycle(scope: state.scope, refreshID: refreshID, titleDisplayMode: .large,
                                          reload: { await state.reload() }, cancelAction: {
                action = nil
                confirmEnable = false
                callbacks = []
                state.closeWebsheet()
            }, observe: { await state.observePending() }))
            .task(id: action?.id) { [action] in
                if let action { await perform(action) }
            }
            .task(id: callbacks.first?.id) { [pending = callbacks.first] in
                guard let pending, callbacks.first?.id == pending.id, !Task.isCancelled else { return }
                if state.websheet?.id == pending.sheetID {
                    await state.completeWebsheet(pending.callback.callback, delivery: pending.callback.status)
                }
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
                        }, failure: { state.failure = $0 }, fixtureHTML: carrierFixtureHTML, session: state.scope.detail.session)
                        .id(websheet.id)
                        .navigationTitle(websheet.sheet.title ?? detailText("capability.wifiCalling"))
                        .navigationBarTitleDisplayMode(.inline)
                        .toolbar {
                            ToolbarItem(placement: .cancellationAction) {
                                Button("common.cancel") { state.closeWebsheet() }
                                    .accessibilityIdentifier("closeWiFiWebsheet")
                            }
                        }
                        .detailErrorAlert($state.failure)
                    }
                    .presentationDetents([.large])
                }
            }
            .detailErrorAlert($state.failure)
    }

    private var form: some View {
        Form {
            let settings = state.settings
            Section {
                IMSConnectionStatusCard(access: .wifiCalling,
                                        status: settings?.status ?? (state.loadFailure == nil ? .loading : .unknown),
                                        enabled: settings?.enabled == true, progress: settings?.progress,
                                        seconds: settings?.durationSeconds, observedAt: state.observedAt,
                                        detail: settings.map { _ in state.uplinkName })
                if let settings { sessionActions(settings) }
            }
            if let settings {
                Section {
                    Toggle(isOn: Binding(
                        get: { settings.enabled },
                        set: { enabled in
                            if enabled { confirmEnable = true }
                            else { action = .init(value: .enabled(false)) }
                        }
                    )) {
                        InlineProgressLabel(isWorking: changingEnabled) { Text("connection.wifiCallingOnLine") }
                    }
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("wifiCallingEnabled")
                } header: {
                    Text("connection.callSettings")
                } footer: {
                    Text("connection.wifiHint")
                }
                if settings.emergencyAddressUpdateAvailable {
                    Section {
                        actionButton("connection.emergencyAddress", action: .emergencyAddress)
                            .disabled(!state.canChange || action != nil)
                            .accessibilityIdentifier("wifiCallingEmergencyAddress")
                    }
                }
                Section {
                    uplinkMenu
                } header: {
                    Text("connection.networkConfiguration")
                } footer: {
                    Text(state.usesProxy ? "connection.wifiProxy.directUplinkHint" : "connection.uplinkHint")
                }
            }
            proxySection
        }
    }

    private var proxySection: some View {
        let value = ConnectionFormat.enabled(state.settings.map { _ in state.usesProxy })
        return Section {
            NavigationLink {
                WiFiProxyView(connection: state)
            } label: {
                LabeledContent("connection.wifiProxy.title", value: value)
            }
            .disabled(action != nil)
            .accessibilityLabel(Text("connection.wifiProxy.title"))
            .accessibilityValue(value)
            .accessibilityIdentifier("wifiProxySettings")
        }
    }

    @ViewBuilder private func sessionActions(_ settings: DetailWiFiSettings) -> some View {
        if state.setupRequired {
            actionButton("connection.continueSetup", action: .setup)
                .disabled(!state.canChange || action != nil)
                .accessibilityIdentifier("wifiCallingSetup")
            Text("connection.setupHint").font(.footnote).foregroundStyle(.secondary)
        } else if settings.enabled {
            if !settings.connected, settings.progress == nil, let error = settings.lastError, !error.isEmpty {
                Text(error).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            actionButton("connection.reconnect", action: .reconnect)
                .disabled(!state.canChange || action != nil || settings.state == "connecting")
                .accessibilityIdentifier("wifiCallingReconnect")
            actionButton("connection.disconnect", action: .disconnect)
                .disabled(!state.canChange || action != nil || settings.state == "disconnected" || settings.state == "idle")
                .accessibilityIdentifier("wifiCallingDisconnect")
        }
    }

    private var uplinkMenu: some View {
        Menu {
            ForEach(state.uplinks) { uplink in
                Button { action = .init(value: .uplink(uplink.underlay)) } label: {
                    if state.isDirectUplink(uplink.underlay) { Label(uplink.name, systemImage: "checkmark") }
                    else { Text(uplink.name) }
                    if !uplink.available { Text("connection.uplinkUnavailable") }
                }
                .disabled(!uplink.available)
                .accessibilityIdentifier("wifiUplink-" + uplink.underlay.mode + (uplink.underlay.modemId.map { "-" + $0 } ?? ""))
                .accessibilityAddTraits(state.isDirectUplink(uplink.underlay) ? .isSelected : [])
            }
        } label: {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    Text("connection.uplink").fixedSize()
                    Spacer(minLength: 0)
                    uplinkValue.fixedSize()
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("connection.uplink")
                    uplinkValue
                }
            }
            .lineLimit(nil)
            .foregroundStyle(.primary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .disabled(!state.canChange || action != nil || state.usesProxy)
        .accessibilityIdentifier("wifiCallingUplink")
        .accessibilityLabel(Text("connection.uplink"))
        .accessibilityValue(state.uplinkName)
    }

    private var uplinkValue: some View {
        HStack(spacing: 6) {
            Text(state.uplinkName)
                .fixedSize(horizontal: false, vertical: true)
            if case .uplink = action?.value {
                ProgressView().controlSize(.small)
            } else if !state.usesProxy {
                Image(systemName: "chevron.up.chevron.down")
                    .imageScale(.small)
                    .accessibilityHidden(true)
            }
        }
        .foregroundStyle(.secondary)
    }

    private var changingEnabled: Bool {
        if case .enabled = action?.value { return true }
        return false
    }

    private func actionButton(_ title: String, action value: Action) -> some View {
        LoadingButton(isWorking: action?.value == value) {
            action = .init(value: value)
        } label: {
            Text(detailText(title))
        }
    }

    private func perform(_ action: ConnectionAction<Action>) async {
        guard self.action?.id == action.id, !Task.isCancelled else { return }
        switch action.value {
        case .enabled(let enabled): await state.setEnabled(enabled)
        case .reconnect: await state.reconnect()
        case .disconnect: await state.disconnect()
        case .setup: await state.openWebsheet()
        case .emergencyAddress: await state.openWebsheet(emergencyAddress: true)
        case .uplink(let uplink): await state.selectUplink(uplink)
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

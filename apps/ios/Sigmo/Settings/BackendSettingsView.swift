import SwiftUI

struct BackendSettingsView: View {
    let session: BackendSession
    @State private var state: SettingsState
    @State private var mcp: MCPState
    @State private var updates: UpdatesState

    init(session: BackendSession) {
        self.session = session
        _state = State(initialValue: SettingsState(session: session))
        _mcp = State(initialValue: MCPState(session: session))
        _updates = State(initialValue: UpdatesState(session: session))
    }

    var body: some View {
        SettingsAccessView(session: session) {
            Form {
                Section {
                    LabeledContent("backends.backend", value: session.configuration.name)
                }
                Section {
                    NavigationLink { SettingsFormView(state: state, section: .authentication) } label: {
                        SettingsLabel(title: "settings.authTitle", symbol: "lock.shield.fill", color: .blue)
                    }.accessibilityIdentifier("settings-auth")
                    NavigationLink { SettingsFormView(state: state, section: .proxy) } label: {
                        SettingsLabel(title: "settings.proxyTitle", symbol: "network", color: .green)
                    }.accessibilityIdentifier("settings-proxy")
                    NavigationLink { NotificationSettingsView(state: state) } label: {
                        SettingsLabel(title: "settings.notificationTitle", symbol: "paperplane.fill", color: .orange)
                    }.accessibilityIdentifier("settings-notifications")
                    NavigationLink { NativePushSettingsView(session: session) } label: {
                        SettingsLabel(title: "push.thisDevice", symbol: "bell.fill", color: .red)
                    }.accessibilityIdentifier("settings-native-push")
                    NavigationLink { MCPSettingsView(state: mcp) } label: {
                        SettingsLabel(title: "settings.mcp.title", symbol: "puzzlepiece.extension.fill", color: .purple)
                    }.accessibilityIdentifier("settings-mcp")
                    NavigationLink { UpdatesSettingsView(state: updates) } label: {
                        SettingsLabel(title: "settings.updates.title", symbol: "arrow.triangle.2.circlepath", color: .gray)
                    }.accessibilityIdentifier("settings-updates")
                }
            }
        }
        .navigationTitle("common.settings")
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: session.accessRevision) { _, _ in clearSettings() }
        .onChange(of: session.canAccessDevices) { _, accessible in
            if !accessible { clearSettings() }
        }
    }

    private func clearSettings() {
        state.clear()
        mcp.clear()
        updates.clear()
    }
}

struct SettingsFormView: View {
    @Bindable var state: SettingsState
    let section: SettingsSection

    private var channelSchema: NotificationSchema? {
        guard case .notification(let channel) = section else { return nil }
        return state.channelSchemas.first { $0.key == channel }
    }

    private var title: String {
        switch section {
        case .authentication: "settings.authTitle"
        case .proxy: "settings.proxyTitle"
        case .notifications: "settings.businessChannels"
        case .notification(let channel): channelSchema?.label ?? channel
        }
    }

    private func binding(_ key: String) -> Binding<SettingValue> {
        Binding(
            get: { state[section][key] ?? .null },
            set: { state[section][key] = $0 }
        )
    }

    var body: some View {
        SettingsAccessView(session: state.session) {
            Form {
                if state.snapshot == nil {
                    if state.busy { ProgressView() }
                } else {
                    ForEach(state.fields(for: section)) { field in
                        SettingsFieldRow(field: field, value: binding(field.key), channels: state.configuredChannels, stacked: section == .authentication && field.control != .number)
                    }
                    if channelSchema == nil, let validation = state.validation(for: section) { Section { Text(validation).foregroundStyle(.secondary) } }
                    Section {
                        if section == .authentication && state.auth["mode"]?.text == "otp" {
                            AsyncActionButton(title: "settings.test", id: "test-auth") { await state.testAuth() }
                                .disabled((state.auth["otpChannels"]?.strings ?? []).isEmpty)
                        }
                        AsyncActionButton(title: "common.save", id: "save-settings") { await state.save(section) }
                            .disabled(state.validation(for: section) != nil)
                    }
                }
                SettingsFeedback(message: state.message)
            }
            .disabled(state.busy)
            .detailErrorAlert(state.failure)
            .refreshable { await state.load(force: true) }
            .foregroundTask { await state.load() }
        }
        .navigationTitle(settingsText(title)).navigationBarTitleDisplayMode(.inline)
    }
}

struct NotificationSettingsView: View {
    @Bindable var state: SettingsState
    var body: some View {
        SettingsAccessView(session: state.session) {
            Form {
                Section {
                    NavigationLink { SettingsFormView(state: state, section: .notifications) } label: { Text("settings.businessChannels") }
                }
                ForEach(state.channelSchemas) { channel in
                    NavigationLink { SettingsFormView(state: state, section: .notification(channel.key)) } label: {
                        LabeledContent(settingsText(channel.label)) {
                            Text(state.snapshot?.values.channels?[channel.key] != nil ? "settings.native.configured" : "settings.native.notConfigured")
                        }
                    }.accessibilityIdentifier("channel-\(channel.key)")
                }
                if state.snapshot == nil {
                    if state.busy { ProgressView() }
                }
            }
            .detailErrorAlert(state.failure)
            .refreshable { await state.load(force: true) }
            .foregroundTask { await state.load() }
        }
        .navigationTitle("settings.notificationTitle").navigationBarTitleDisplayMode(.inline)
    }
}

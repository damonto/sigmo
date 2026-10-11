import SwiftUI

struct ModemSettingsView: View {
    let state: ModemSettingsState
    @State private var pending: AirplaneChange?
    @State private var presentedFailure: BackendFailure?
    @Environment(\.scenePhase) private var scenePhase

    private struct AirplaneChange: Identifiable {
        let id = UUID()
        let enabled: Bool
    }

    var body: some View {
        ModemSettingsForm(state: state) {
            airplaneSection
            Section {
                NavigationLink {
                    ModemNetworkModeView(state: state)
                } label: {
                    LabeledContent {
                        Text(modeSummary)
                    } label: {
                        SettingsLabel(title: "modemSettings.mode", symbol: "antenna.radiowaves.left.and.right", color: .green)
                    }
                }
                .disabled(!state.radioAvailable || state.modes == nil || state.failures[.mode] != nil)
                .accessibilityIdentifier("modemNetworkMode")
                .modifier(ModemSettingsLoading(loading: state.modes == nil && state.failures[.mode] == nil))
                ModemSettingsReadError(state: state, section: .mode)

                NavigationLink {
                    ModemFrequencyBandsView(state: state)
                } label: {
                    LabeledContent {
                        Text(bandsSummary)
                    } label: {
                        SettingsLabel(title: "modemSettings.bands", symbol: "waveform", color: .blue)
                    }
                }
                .disabled(!state.radioAvailable || state.bands == nil || state.failures[.bands] != nil)
                .accessibilityIdentifier("modemFrequencyBands")
                .modifier(ModemSettingsLoading(loading: state.bands == nil && state.failures[.bands] == nil))
                ModemSettingsReadError(state: state, section: .bands)
            } header: {
                Text("modemSettings.network")
            } footer: {
                if state.airplane?.enabled == true { Text("modemSettings.radioOff") }
            }
            Section {
                NavigationLink {
                    ModemDeviceSettingsView(state: state)
                } label: {
                    LabeledContent {
                        EmptyView()
                    } label: {
                        SettingsLabel(title: "modemSettings.device", symbol: "gearshape.fill", color: .gray)
                    }
                }
                .disabled(state.busy || state.device == nil || state.failures[.device] != nil)
                .accessibilityIdentifier("modemDeviceSettings")
                .modifier(ModemSettingsLoading(loading: state.device == nil && state.failures[.device] == nil))
                ModemSettingsReadError(state: state, section: .device)
            }
        }
        .detailErrorAlert($presentedFailure)
        .task(id: pending?.id) {
            guard let change = pending else { return }
            _ = await state.save(.airplane(change.enabled))
            guard pending?.id == change.id, !Task.isCancelled else { return }
            pending = nil
            presentedFailure = state.failure
        }
        .onDisappear { pending = nil }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { pending = nil }
        }
    }

    private var airplaneSection: some View {
        Section {
            Toggle(isOn: Binding(
                get: { pending?.enabled ?? state.airplane?.enabled ?? false },
                set: { if state.canChangeAirplane, pending == nil { pending = AirplaneChange(enabled: $0) } }
            )) {
                HStack {
                    SettingsLabel(title: "modemSettings.airplane", symbol: "airplane", color: .orange)
                    if pending != nil { ProgressView().accessibilityLabel(Text("modemSettings.saving")) }
                }
            }
            .tint(.green)
            .disabled(!state.canChangeAirplane || pending != nil)
            .accessibilityIdentifier("modemAirplaneMode")
            .modifier(ModemSettingsLoading(loading: state.airplane == nil && state.failures[.airplane] == nil))
            ModemSettingsReadError(state: state, section: .airplane)
        } footer: {
            Text(state.airplane?.supported == false ? "modemSettings.airplaneUnsupported" : "modemSettings.airplaneHint")
        }
    }

    private var modeSummary: String {
        guard let modes = state.modes, !modes.supported.isEmpty else { return detailText("common.unknown") }
        return modes.current.label
    }

    private var bandsSummary: String {
        guard let bands = state.bands, !bands.supported.isEmpty else { return detailText("common.unknown") }
        return String(format: detailText("modemSettings.selectedCount"), bands.current.count)
    }
}

// Each visible page owns its reads. Pushing an editor preserves the shared
// snapshots while SwiftUI cancels the previous page's outstanding work.
struct ModemSettingsForm<Content: View>: View {
    let state: ModemSettingsState
    @ViewBuilder var content: () -> Content
    @State private var refreshID = 0
    @Environment(\.scenePhase) private var scenePhase

    private struct RefreshIdentity: Equatable {
        let background: Bool
        let accessRevision: Int
        let hasAccess: Bool
        let refreshID: Int
    }

    var body: some View {
        Form {
            if state.hasAccess {
                content()
                if state.needsConfirmation, !state.busy {
                    Section {
                        Text(state.writeAcknowledged ? "modemSettings.savedRefreshPending" : "modemSettings.confirmationPending")
                            .foregroundStyle(.secondary)
                        RetryButton(id: "retryModemSettingsConfirmation") {
                            await state.load()
                        }
                        .disabled(!state.loading.isEmpty)
                    }
                }
                if let failure = state.session.detailFailures[state.modemID], !state.needsConfirmation {
                    Section {
                        Text(failure.message).foregroundStyle(.secondary)
                        RetryButton { refreshID += 1 }
                    }
                }
            } else {
                ContentUnavailableView("devices.unavailable", systemImage: "simcard",
                                       description: Text(state.session.canAccessDevices ? "devices.removed" : "devices.accessRequired"))
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .task(id: RefreshIdentity(background: scenePhase == .background, accessRevision: state.session.accessRevision,
                                  hasAccess: state.hasAccess, refreshID: refreshID)) {
            guard scenePhase != .background else { return }
            await state.load()
        }
        .refreshable { await state.load() }
        .onChange(of: state.session.accessRevision) { _, _ in state.clear() }
        .onChange(of: state.hasAccess) { _, value in if !value { state.clear() } }
        .onChange(of: state.busy) { _, busy in
            if !busy, state.needsConfirmation, scenePhase != .background { refreshID += 1 }
        }
        .onChange(of: state.modem?.airplaneMode) { _, value in
            if !state.busy, value != state.airplane?.enabled { refreshID += 1 }
        }
    }
}

struct ModemSettingsLoading: ViewModifier {
    let loading: Bool

    func body(content: Content) -> some View {
        if loading { content.detailSkeleton("modemSettings.loading") }
        else { content }
    }
}

struct ModemSettingsReadError: View {
    let state: ModemSettingsState
    let section: ModemSettingsSection

    var body: some View {
        if let failure = state.failures[section] {
            VStack(alignment: .leading, spacing: 8) {
                Text(failure.message).font(.footnote).foregroundStyle(.secondary)
                RetryButton(id: "retryModemSettings-" + section.rawValue) {
                    await state.load([section])
                }
                .disabled(state.busy || state.loading.contains(section))
            }
        }
    }
}

import SwiftUI

struct ESIMProfileActionsView: View {
    let state: ModemDetailState
    let profileID: String

    private enum Sheet { case number, rename, reminder, sim }
    private enum Confirmation { case enable, delete }
    private enum Operation { case enable, delete }

    private struct PendingOperation: Identifiable {
        let id = UUID()
        let kind: Operation
        let profile: ESIMProfile
    }

    private struct ObservationIdentity: Equatable {
        let background: Bool
        let accessRevision: Int
        let enabled: Bool
        let sim: ModemSIMIdentity?
    }

    @State private var sheet: Sheet?
    @State private var confirmation: Confirmation?
    @State private var operation: PendingOperation?
    @State private var presentedFailure: BackendFailure?
    @State private var refreshID = 0
    @State private var sim: SIMApplicationState
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    init(state: ModemDetailState, profileID: String) {
        self.state = state
        self.profileID = profileID
        _sim = State(initialValue: SIMApplicationState(session: state.session, modemID: state.modemID))
    }

    private var profile: ESIMProfile? { state.currentProfile(profileID) }
    private var busy: Bool { state.busy || operation != nil }
    private var observationIdentity: ObservationIdentity {
        ObservationIdentity(
            background: scenePhase == .background,
            accessRevision: state.session.accessRevision,
            enabled: profile?.enabled == true,
            sim: state.simIdentity
        )
    }

    private var presentedForm: some View {
        form
            .sheet(isPresented: Binding(get: { sheet != nil }, set: { if !$0 { sheet = nil } })) {
                NavigationStack {
                    if let profile, let sheet {
                        switch sheet {
                        case .number: DetailTextForm(state: state, kind: .number)
                        case .rename: DetailTextForm(state: state, kind: .rename(profile))
                        case .reminder: DetailReminderForm(state: state, profile: profile)
                        case .sim: SIMApplicationView(state: sim)
                        }
                    }
                }
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
            }
            .alert(
                confirmation == .delete ? "detail.deleteProfile" : "detail.enableProfile",
                isPresented: Binding(get: { confirmation != nil }, set: { if !$0 { confirmation = nil } }),
                presenting: confirmation
            ) { confirmation in
                Button(
                    confirmation == .delete ? "detail.deleteESIM" : "detail.confirm",
                    role: confirmation == .delete ? .destructive : nil
                ) {
                    begin(confirmation == .delete ? .delete : .enable)
                    self.confirmation = nil
                }
                .accessibilityIdentifier("confirmDetailAction")
                Button("common.cancel", role: .cancel) { self.confirmation = nil }
            } message: { confirmation in
                if confirmation == .delete {
                    Text("detail.deleteESIMWarning")
                } else if let profile {
                    let message = String(format: detailText("detail.enableProfileMessage"), profile.name)
                    Text(message + "\n\n" + profile.iccid)
                }
            }
    }

    var body: some View {
        presentedForm
            .task(id: operation?.id) { await performOperation() }
            .task(id: refreshID) {
                if refreshID > 0 { await reload() }
            }
            .task(id: observationIdentity) {
                guard scenePhase != .background else { return }
                await reload()
            }
            .task(id: observationIdentity) {
                guard scenePhase != .background, profile?.enabled == true else {
                    sim.stop()
                    return
                }
                await sim.observe()
            }
            .onChange(of: sim.presented) { _, value in
                if value, sheet == nil { sheet = .sim }
                if !value, sheet == .sim { sheet = nil }
            }
            .onChange(of: sheet) { _, value in
                if value == nil, sim.presented { sheet = .sim }
            }
            .onChange(of: sim.failure) { _, failure in
                if sheet == nil { presentedFailure = failure }
            }
            .onChange(of: state.session.accessRevision) { _, _ in
                sheet = nil
                confirmation = nil
                operation = nil
                presentedFailure = nil
                sim.stop()
                dismiss()
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .background {
                    operation = nil
                    sim.stop()
                }
            }
            .onDisappear {
                operation = nil
                sim.stop()
            }
    }

    private var form: some View {
        Form {
            if let profile {
                identitySection(profile)
                lineSection(profile)
                if state.isActiveProfile(profileID) { connectionSection }
                servicesSection(profile)
                if !profile.enabled {
                    Section {
                        Button(role: .destructive) { confirmation = .delete } label: {
                            Text("detail.deleteESIM").frame(maxWidth: .infinity)
                        }
                    }
                }
            } else {
                ContentUnavailableView("devices.unavailable", systemImage: "simcard")
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .disabled(busy)
        .navigationTitle(profile?.name ?? detailText("detail.profileActions"))
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await reload() }
        .detailErrorAlert($presentedFailure, retry: { refreshID += 1 })
    }

    private func identitySection(_ profile: ESIMProfile) -> some View {
        Section {
            editRow("detail.lineLabel", value: profile.name) { sheet = .rename }
            if profile.enabled {
                editRow(
                    "modem.number",
                    value: PhoneNumberDisplay.format(state.modem?.number ?? "", regionCode: state.modem?.sim.regionCode)
                ) { sheet = .number }
            }
        }
    }

    private func lineSection(_ profile: ESIMProfile) -> some View {
        Section {
            Toggle("detail.useLine", isOn: Binding(
                get: { profile.enabled },
                set: { if $0 { confirmation = .enable } }
            ))
            .disabled(profile.enabled || busy || state.needsPIN)
            .accessibilityIdentifier("profileEnabled-" + profile.id)
        } footer: {
            if profile.enabled { Text("detail.activeLineHint") }
        }
    }

    @ViewBuilder private var connectionSection: some View {
        Section {
            NavigationLink {
                NetworkSelectionView(detail: state, profileID: profileID)
            } label: {
                LabeledContent("connection.networkSelection", value: state.modem?.networkSelectionName ?? detailText("common.unknown"))
            }
            .accessibilityIdentifier("profileNetworkSelection")
            NavigationLink {
                InternetView(detail: state, profileID: profileID)
            } label: {
                LabeledContent("connection.internet", value: state.modem?.internetConnected.map {
                    detailText($0 ? "common.connected" : "common.disconnected")
                } ?? detailText("common.unknown"))
            }
            .accessibilityIdentifier("profileInternet")
            if state.canUseWiFi {
                NavigationLink {
                    WiFiCallingView(detail: state, profileID: profileID)
                } label: {
                    LabeledContent("capability.wifiCalling", value: ConnectionFormat.enabled(state.modem?.wifiCallingEnabled))
                }
                .accessibilityIdentifier("profileWiFiCalling")
            }
            if state.session.capabilities?.contains("volte") == true {
                NavigationLink {
                    VoLTEView(detail: state, profileID: profileID)
                } label: {
                    LabeledContent("capability.volte", value: ConnectionFormat.enabled(state.modem?.volteEnabled))
                }
                .accessibilityIdentifier("profileVoLTE")
            }
        }
        .disabled(state.needsPIN)
    }

    @ViewBuilder private func servicesSection(_ profile: ESIMProfile) -> some View {
        Section {
            editRow("detail.reminder") { sheet = .reminder }
        } footer: {
            if let reminder = profile.reminder { ReminderSummary(reminder: reminder) }
        }
        Section {
            if profile.enabled, sim.available, sim.profileICCID == profile.iccid {
                editRow("detail.simApplication") {
                    sim.open()
                    sheet = .sim
                }
            }
            NavigationLink {
                ESIMProfileInfoView(state: state, profileID: profileID)
            } label: {
                Text("detail.profileDetails")
            }
            .accessibilityIdentifier("profileManagementInfo")
        }
    }

    private func editRow(_ title: String, value: String = "", action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 8) {
                LabeledContent {
                    if !value.isEmpty {
                        Text(value).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                    }
                } label: {
                    Text(detailText(title))
                }
                .frame(maxWidth: .infinity)
                Image(systemName: "chevron.right")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .foregroundStyle(.primary)
        .accessibilityLabel(Text(detailText(title)))
        .accessibilityValue(value)
    }

    private func begin(_ kind: Operation) {
        guard !busy, let profile else { return }
        operation = PendingOperation(kind: kind, profile: profile)
    }

    private func performOperation() async {
        guard let operation else { return }
        var deleted = false
        switch operation.kind {
        case .enable: _ = await state.enable(operation.profile)
        case .delete: deleted = await state.delete(operation.profile)
        }
        guard self.operation?.id == operation.id, !Task.isCancelled else { return }
        self.operation = nil
        presentedFailure = state.failure
        if deleted { dismiss() }
    }

    private func reload() async {
        await state.load()
        guard !Task.isCancelled else { return }
        presentedFailure = state.failure ?? state.profilesFailure
    }
}

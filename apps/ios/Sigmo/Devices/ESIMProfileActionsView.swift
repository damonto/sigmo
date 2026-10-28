import SwiftUI

struct ESIMProfileOperation: Identifiable {
    enum Kind { case enable, delete }

    // A late completion must not clear a later action on the same profile.
    let id = UUID()
    let kind: Kind
    let profile: ESIMProfile
}

struct ESIMProfileActionsView: View {
    let state: ModemDetailState
    let profileID: String
    // Lists own these tasks; standalone reminder destinations run them locally.
    let onOperation: ((ESIMProfileOperation) -> Void)?

    private enum Sheet { case number, rename, reminder, sim }

    private struct ObservationIdentity: Equatable {
        let accessRevision: Int
        let enabled: Bool
        let sim: ModemSIMIdentity?
    }

    private struct SIMObservationIdentity: Equatable {
        let profile: ObservationIdentity
        let suspended: Bool
    }

    @State private var sheet: Sheet?
    @State private var confirmation: ESIMProfileOperation.Kind?
    @State private var operation: ESIMProfileOperation?
    @State private var presentedFailure: BackendFailure?
    @State private var sim: SIMApplicationState
    @State private var pinState: SIMPINState
    @Environment(\.dismiss) private var dismiss

    init(state: ModemDetailState, profileID: String, onOperation: ((ESIMProfileOperation) -> Void)? = nil) {
        self.state = state
        self.profileID = profileID
        self.onOperation = onOperation
        _sim = State(initialValue: SIMApplicationState(session: state.session, modemID: state.modemID))
        _pinState = State(initialValue: SIMPINState(session: state.session, modemID: state.modemID))
    }

    private var profile: ESIMProfile? { state.currentProfile(profileID) }
    private var busy: Bool { state.loading || state.busy || operation != nil }
    private var observationIdentity: ObservationIdentity {
        ObservationIdentity(
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
                    begin(confirmation)
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
            .sheet(isPresented: Binding(
                get: { onOperation == nil && state.deletedHistoryProfileID != nil },
                set: { if !$0 { state.deletedHistoryProfileID = nil } }
            ), onDismiss: { dismiss() }) {
                if let id = state.deletedHistoryProfileID {
                    HistoryAssignmentView(session: state.session, sourceID: id)
                }
            }
            .task(id: observationIdentity) { pinState.reset(); if profile?.enabled == true { await pinState.load() } }
            .task(id: operation?.id) { await performOperation() }
            .foregroundTask(id: observationIdentity) {
                await reload()
            }
            .task(id: SIMObservationIdentity(profile: observationIdentity, suspended: sim.observationSuspended)) {
                guard !sim.observationSuspended, profile?.enabled == true else {
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
                if state.isActiveProfile(profileID) { LineConnectionSection(state: state, profileID: profileID) }
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
        .detailErrorAlert($presentedFailure)
    }

    private func identitySection(_ profile: ESIMProfile) -> some View {
        Section {
            DetailEditRow(title: "detail.lineLabel", value: profile.name) { sheet = .rename }
            if profile.enabled {
                DetailEditRow(
                    title: "modem.number",
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
            .disabled(profile.enabled || busy || state.needsSIMUnlock)
            .accessibilityIdentifier("profileEnabled-" + profile.id)
        } footer: {
            if profile.enabled { Text("detail.activeLineHint") }
        }
    }

    @ViewBuilder private func servicesSection(_ profile: ESIMProfile) -> some View {
        Section("detail.reminder") {
            ReminderRow(reminder: profile.reminder) { sheet = .reminder }
        }
        Section {
            if profile.enabled, pinState.available, state.modem?.sim.identifier == profile.iccid {
                NavigationLink("simPin.title") { SIMPINView(state: pinState) }
                    .accessibilityIdentifier("profileSIMPIN")
            }
            if profile.enabled, sim.available, sim.profileICCID == profile.iccid {
                DetailEditRow(title: "detail.simApplication") {
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

    private func begin(_ kind: ESIMProfileOperation.Kind) {
        guard !busy, let profile else { return }
        let operation = ESIMProfileOperation(kind: kind, profile: profile)
        if let onOperation {
            onOperation(operation)
            dismiss()
            return
        }
        self.operation = operation
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
        if deleted, state.deletedHistoryProfileID == nil { dismiss() }
    }

    private func reload() async {
        await state.load()
        guard !Task.isCancelled else { return }
        presentedFailure = state.failure ?? state.profilesFailure
    }
}

import SwiftUI

struct ModemDetailView: View {
    let session: BackendSession
    var initialConversationID: String?
    let close: () -> Void
    @State private var selectedID: String

    private struct ScreenIdentity: Hashable {
        let session: ObjectIdentifier
        let modemID: String
    }

    init(session: BackendSession, modemID: String, initialConversationID: String? = nil, close: @escaping () -> Void) {
        self.session = session
        self.close = close
        self.initialConversationID = initialConversationID
        _selectedID = State(initialValue: modemID)
    }
    var body: some View {
        ModemDetailScreen(session: session, modemID: selectedID, initialConversationID: initialConversationID, close: close, selectModem: { selectedID = $0 })
            .id(ScreenIdentity(session: ObjectIdentifier(session), modemID: selectedID))
    }
}

private enum DetailSheet {
    case pin, number, reminder, sim
}

private enum DetailTab {
    case overview, messages, calls, settings
}

private struct SlotChange: Identifiable {
    let id = UUID()
    let slot: SIMSlot
}

private struct ModemDetailScreen: View {
    let session: BackendSession
    let close: () -> Void
    let selectModem: (String) -> Void
    @State private var state: ModemDetailState
    @State private var settings: ModemSettingsState
    @State private var sim: SIMApplicationState
    @State private var messages: MessagesState
    @State private var messageConversation: SMSConversation?
    @State private var composingMessage = false
    @State private var callHistory: CallHistoryState
    @State private var callRecord: CallRecord?
    @State private var dialing = false
    @State private var recordingsPresented = false
    let initialConversationID: String?
    @State private var sheet: DetailSheet?
    @State private var confirmation: SIMSlot?
    @State private var pending: SlotChange?
    @State private var profileOperation: ESIMProfileOperation?
    @State private var presentedFailure: BackendFailure?
    @State private var preparingFlow: ESIMFlowState?
    @State private var availableFlow: ESIMFlowState?
    @State private var flow: ESIMFlowState?
    @State private var showDiscoveries = false
    @State private var pinDismissed = false
    @State private var refreshID = 0
    @State private var tab = DetailTab.overview
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(session: BackendSession, modemID: String, initialConversationID: String?, close: @escaping () -> Void, selectModem: @escaping (String) -> Void) {
        self.session = session
        self.close = close
        self.selectModem = selectModem
        self.initialConversationID = initialConversationID
        _tab = State(initialValue: initialConversationID == nil ? .overview : .messages)
        _state = State(initialValue: ModemDetailState(session: session, modemID: modemID))
        _settings = State(initialValue: ModemSettingsState(session: session, modemID: modemID))
        _sim = State(initialValue: SIMApplicationState(session: session, modemID: modemID))
        _messages = State(initialValue: MessagesState(session: session, modemID: modemID))
        _callHistory = State(initialValue: CallHistoryState(session: session, modemID: modemID))
    }
    private struct RefreshIdentity: Equatable {
        let accessRevision: Int
        let refreshID: Int
        let tab: DetailTab
    }

    private struct SIMObservationIdentity: Equatable {
        let refresh: RefreshIdentity
        let suspended: Bool
        let canAccessDevices: Bool
        let sim: ModemSIMIdentity?
        let installing: Bool
    }

    private var refreshIdentity: RefreshIdentity {
        RefreshIdentity(
            accessRevision: session.accessRevision,
            refreshID: refreshID,
            tab: tab
        )
    }

    private var simObservationIdentity: SIMObservationIdentity {
        SIMObservationIdentity(
            refresh: refreshIdentity,
            suspended: sim.observationSuspended,
            canAccessDevices: session.canAccessDevices,
            sim: state.simIdentity,
            installing: addingESIM
        )
    }

    private var addingESIM: Bool { preparingFlow != nil || availableFlow != nil || flow != nil }
    private var isPhysicalSIM: Bool { state.modem?.simKind == "physical" }
    private var showsNetworkBar: Bool { !state.needsPIN && (state.esim || isPhysicalSIM) }

    private var tabs: some View {
        TabView(selection: $tab) {
            Tab("detail.tab.overview", systemImage: "simcard", value: DetailTab.overview) {
                tabContent(.overview) { overview }
            }
            Tab("detail.tab.messages", systemImage: "message", value: DetailTab.messages) {
                tabContent(.messages) {
                    MessagesView(state: messages, initialConversationID: initialConversationID, pushed: $messageConversation, composing: $composingMessage)
                        .searchable(text: $messages.search, placement: .navigationBarDrawer(displayMode: .always), prompt: "messages.search")
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .navigationDestination(item: $messageConversation) { conversation in
                            MessageThreadView(state: messages.thread(conversation)).id(conversation.id)
                        }
                }
            }
            .badge(session.unreadCounts[state.modemID] ?? 0)
            Tab("detail.tab.calls", systemImage: "phone", value: DetailTab.calls) {
                tabContent(.calls) {
                    CallsView(state: callHistory, selected: $callRecord, dialing: $dialing, recordingsPresented: $recordingsPresented)
                        .searchable(text: $callHistory.search, placement: .navigationBarDrawer(displayMode: .always), prompt: "calls.search")
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .navigationDestination(item: $callRecord) { record in
                            CallDetailView(record: record, history: callHistory)
                        }
                }
            }
            Tab("common.settings", systemImage: "gearshape", value: DetailTab.settings) {
                tabContent(.settings) { ModemSettingsView(state: settings) }
            }
        }
        // Keep native tabs at the bottom on iPad; each page restores its real size class.
        .environment(\.horizontalSizeClass, .compact)
    }
    private func tabContent<Content: View>(_ tab: DetailTab, @ViewBuilder content: () -> Content) -> some View {
        NavigationStack {
            content()
                .environment(\.horizontalSizeClass, horizontalSizeClass)
                .navigationTitle(state.modem?.displayName ?? detailText("devices.title"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbarVisibility(.visible, for: .navigationBar)
                .toolbar { toolbar(for: tab) }
        }
    }
    private var presentedContent: some View {
        tabs
        // The list owns profile operations so dismissing their detail page does not cancel them.
        .task(id: profileOperation?.id) {
            guard let operation = profileOperation else { return }
            switch operation.kind {
            case .enable: _ = await state.enable(operation.profile)
            case .delete: _ = await state.delete(operation.profile)
            }
            guard profileOperation?.id == operation.id, !Task.isCancelled else { return }
            profileOperation = nil
            presentedFailure = state.failure ?? state.elementsFailure ?? state.profilesFailure
        }
        .task(id: pending?.id) {
            guard let change = pending else { return }
            _ = await state.switchSlot(change.slot.slot)
            guard pending?.id == change.id, !Task.isCancelled else { return }
            pending = nil
            presentedFailure = state.failure ?? state.elementsFailure ?? state.profilesFailure
        }
        .alert(
            "detail.switchSlot",
            isPresented: Binding(get: { confirmation != nil }, set: { if !$0 { confirmation = nil } }),
            presenting: confirmation
        ) { slot in
            Button("detail.confirm") {
                pending = SlotChange(slot: slot)
                confirmation = nil
            }
            .accessibilityIdentifier("confirmDetailAction")
            Button("common.cancel", role: .cancel) { confirmation = nil }
        } message: { slot in
            Text(String(format: detailText("detail.switchSlotMessage"), slot.slot, slot.operatorName))
        }
        .alert(
            "detail.discoveriesAvailable",
            isPresented: Binding(get: { availableFlow != nil }, set: { if !$0 { availableFlow = nil } }),
            presenting: availableFlow
        ) { available in
            Button("detail.download") { presentFlow(available, showDiscoveries: true) }
                .accessibilityIdentifier("showESIMDiscoveries")
            Button("detail.downloadLater", role: .cancel) { presentFlow(available, showDiscoveries: false) }
                .accessibilityIdentifier("skipESIMDiscoveries")
        } message: { _ in
            Text("detail.discoveriesAvailableMessage")
        }
        .sheet(isPresented: Binding(get: { sheet != nil }, set: { if !$0 { sheet = nil } }), onDismiss: { pinDismissed = state.needsPIN }) {
            NavigationStack {
                switch sheet {
                case .pin: DetailTextForm(state: state, kind: .pin)
                case .number: DetailTextForm(state: state, kind: .number)
                case .reminder: DetailReminderForm(state: state, profile: nil)
                case .sim: SIMApplicationView(state: sim)
                case nil: EmptyView()
                }
            }
            .presentationBackground(Color(uiColor: .systemGroupedBackground))
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
        .sheet(item: $flow, onDismiss: { refreshID += 1 }) { flow in
            ESIMFlowView(state: flow, showDiscoveries: showDiscoveries)
                .presentationDetents([.large])
                .presentationDragIndicator(.hidden)
                .presentationBackground(Color(uiColor: .systemGroupedBackground))
        }
    }
    var body: some View {
        presentedContent
        .onChange(of: sim.presented) { _, shown in
            if shown, sheet == nil, !addingESIM { sheet = .sim }
            if !shown, case .sim = sheet { sheet = nil }
        }
        .onChange(of: sim.failure) { _, failure in
            if sheet == nil, !addingESIM { presentedFailure = failure }
        }
        .onChange(of: sheet) { _, value in
            if value == nil, sim.presented, !addingESIM { sheet = .sim }
        }
        .onChange(of: state.simIdentity) { _, _ in
            cancelPreparation()
            if !state.loading, !state.busy { refreshID += 1 }
        }
        .onChange(of: state.needsPIN) { _, _ in updatePINPresentation() }
        .onChange(of: state.busy) { _, busy in
            if !busy { updatePINPresentation() }
        }
        .onChange(of: session.accessRevision) { _, _ in clear() }
        .onChange(of: session.canAccessDevices) { _, value in if !value { clear() } }
        .onChange(of: tab) { _, value in
            if value != .overview { cancelPreparation() }
        }
        .onDisappear { pending = nil; profileOperation = nil; cancelPreparation(); sim.stop() }
    }

    private var overview: some View {
        List {
            if let modem = state.modem {
                if state.needsPIN {
                    Section {
                        Label("detail.simLocked", systemImage: "lock")
                        Button("detail.unlock") { sheet = .pin }
                    }
                } else if !state.esim, !isPhysicalSIM {
                    slotSection(modem)
                }
                if state.esim {
                    profiles
                } else if isPhysicalSIM {
                    physicalActions(modem)
                        .disabled(state.loading || state.busy)
                } else {
                    ModemInfoSections(modem: modem, elements: [], capabilities: session.capabilities ?? [])
                }
            } else if state.loading {
                Section("modem.device") {
                    ForEach(["modem.id", "configuration.name", "modem.carrier"], id: \.self) { title in
                        DetailValue(title: title, value: "Example Mobile")
                    }
                }.detailSkeleton("detail.loadingInformation")
            } else {
                ContentUnavailableView("devices.unavailable", systemImage: "simcard", description: Text(session.canAccessDevices ? "devices.removed" : "devices.accessRequired"))
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.defaultMinListRowHeight, 44)
        .environment(\.horizontalSizeClass, horizontalSizeClass)
        .labelStyle(.titleAndIcon)
        // Let content show through around the glass capsule without a full-width edge backdrop.
        .scrollEdgeEffectHidden(showsNetworkBar, for: .top)
        .safeAreaBar(edge: .top, spacing: 0) {
            if showsNetworkBar, let modem = state.modem {
                networkBar(modem)
            }
        }
        .refreshable { await reload() }
        .detailErrorAlert($presentedFailure)
        .foregroundTask(id: refreshIdentity) {
            guard tab == .overview else { return }
            await reload()
            offerPIN()
        }
        .task(id: simObservationIdentity) {
            guard !sim.observationSuspended, tab == .overview, !addingESIM, state.modem != nil else { sim.stop(); return }
            await sim.observe()
        }
        .onDisappear { pending = nil; cancelPreparation(); sim.stop() }
    }

    @ToolbarContentBuilder private func toolbar(for tab: DetailTab) -> some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Button("devices.title", systemImage: "chevron.backward", action: close)
                .accessibilityIdentifier("backToDevices")
        }
        ToolbarItem(placement: .principal) {
            Menu {
                ForEach(session.modems) { modem in
                    Button(modem.displayName) { selectModem(modem.id) }
                }
            } label: {
                HStack(spacing: 4) {
                    VStack(spacing: 1) {
                        Text(state.modem?.displayName ?? detailText("devices.title")).font(.headline).lineLimit(1).truncationMode(.tail)
                            .accessibilityIdentifier("detailModemTitle")
                        Text(session.configuration.name).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                            .accessibilityIdentifier("detailBackendName")
                    }
                    Image(systemName: "chevron.down").font(.caption)
                }.frame(width: horizontalSizeClass == .regular ? 320 : 200, height: 44).clipped()
            }
            .disabled(state.busy || settings.busy || pending != nil)
            .accessibilityLabel(Text("detail.switchModem"))
            .accessibilityValue((state.modem?.displayName ?? "") + " · " + session.configuration.name)
        }
        if tab == .overview, state.esim || isPhysicalSIM {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink { ModemInfoView(state: state) } label: {
                    Label("common.details", systemImage: "info.circle").labelStyle(.iconOnly)
                }
                .accessibilityIdentifier("modemInfo")
            }

        }
        if tab == .messages {
            ToolbarItem(placement: .topBarTrailing) {
                Button("messages.new", systemImage: "square.and.pencil") { composingMessage = true }
                    .disabled(messages.modem?.sim.identifier.isEmpty != false)
                    .accessibilityIdentifier("new-message")
            }
        }
        if tab == .calls {
            ToolbarItemGroup(placement: .topBarTrailing) {
                Button("calls.recordings", systemImage: "waveform") { recordingsPresented = true }
                Button("calls.keypad", systemImage: "circle.grid.3x3.fill") { dialing = true }
                    .disabled(callHistory.line == nil).accessibilityIdentifier("calls-keypad")
            }
        }
    }
    private func slotSection(_ modem: Modem) -> some View {
        Section("modem.sim") {
            LabeledContent {
                HStack(spacing: 12) {
                    networkItems(modem)
                }
            } label: { Text(modem.carrier) }
        }
    }

    private func networkBar(_ modem: Modem) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) {
                Text(modem.carrier).fixedSize()
                Spacer(minLength: 0)
                networkControls(modem)
            }
            VStack(spacing: 8) {
                Text(modem.carrier)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                networkControls(modem)
            }
            .frame(maxWidth: .infinity)
        }
        .font(.subheadline)
        .padding(.horizontal, dynamicTypeSize.isAccessibilitySize ? 44 : 20)
        .padding(.vertical, 10)
        .frame(minHeight: 44)
        .glassEffect(.regular, in: Capsule())
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("esimNetworkBar")
        .padding(.horizontal, 16)
        .padding(.top, 8)
        // The grouped list already supplies the physical SIM's section spacing below the bar.
        .padding(.bottom, isPhysicalSIM ? 0 : 8)
    }

    private func networkControls(_ modem: Modem) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { networkItems(modem) }
            VStack(spacing: 8) { networkItems(modem) }
        }
    }

    @ViewBuilder private func networkItems(_ modem: Modem) -> some View {
        ModemStatusIcons(modem: modem, capabilities: session.capabilities ?? [])
        if let slots = modem.slots, slots.count > 1 {
            slotMenu(slots, selectedSlot: modem.sim.slot)
        }
    }

    private func slotMenu(_ slots: [SIMSlot], selectedSlot: Int) -> some View {
        Menu {
            ForEach(slots) { slot in
                Button { confirmation = slot } label: {
                    if slot.active {
                        Label("SIM \(slot.slot)", systemImage: "checkmark")
                    } else {
                        Text("SIM \(slot.slot)")
                    }
                }
                .disabled(slot.active)
                .accessibilityIdentifier("simSlot-\(slot.slot)")
            }
        } label: {
            HStack(spacing: 4) {
                if pending != nil { ProgressView() }
                Text("SIM \(selectedSlot)")
                Image(systemName: "chevron.down").font(.caption)
            }
            .fixedSize()
        }
        .disabled(state.busy || pending != nil || addingESIM)
        .accessibilityLabel(Text("detail.switchSlot"))
        .accessibilityValue("SIM \(selectedSlot)")
        .accessibilityIdentifier("simSlotPicker")
    }

    @ViewBuilder private var profiles: some View {
        if !state.elements.isEmpty {
            ForEach(state.elements) { element in
                profileSection(element)
            }
        } else if !state.elementsLoaded, state.failure == nil {
            Section("detail.profiles") { ProfileSkeletonRows() }
        } else if state.elementsFailure != nil || state.profilesFailure != nil {
            Section("detail.profiles") {
                ContentUnavailableView("detail.profilesUnavailable", systemImage: "simcard")
            }
        }
    }

    private func profileSection(_ element: SecureElement) -> some View {
        let profiles = state.groups.first { $0.id == element.id }?.profiles ?? []
        return Section {
            if !state.profilesLoaded, state.failure == nil {
                ProfileSkeletonRows()
            } else if profiles.isEmpty, state.profilesFailure != nil {
                ContentUnavailableView("detail.profilesUnavailable", systemImage: "simcard")
            } else {
                ForEach(profiles) { profile in
                    profileRow(profile)
                }
                if profiles.isEmpty, state.profilesLoaded {
                    Text("detail.noProfiles").foregroundStyle(.secondary)
                }
                AsyncActionButton(id: "addESIM-" + element.id, minimumLabelHeight: nil, action: {
                    await prepareFlow(seID: element.id)
                }) {
                    Text("detail.setupTitle").frame(maxWidth: .infinity, alignment: .leading)
                }
                .disabled(!state.canInstall || addingESIM)
                .accessibilityValue(preparingFlow?.seID == element.id ? detailText("detail.checkingESIM") : "")
            }
        } header: {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text("detail.profiles")
                    if state.elements.count > 1 {
                        Text("· \(secureElementTitle(element.id, elements: state.elements))")
                    }
                    Spacer()
                    if state.profilesLoaded, state.profilesFailure == nil || !profiles.isEmpty {
                        Text(profiles.count.formatted()).monospacedDigit()
                    }
                }
                .foregroundStyle(Color(uiColor: .label))
                SecureElementMetadata(element: element)
            }
        }
    }

    private func profileRow(_ profile: ESIMProfile) -> some View {
        NavigationLink {
            ESIMProfileActionsView(state: state, profileID: profile.id) { profileOperation = $0 }
        } label: {
            if dynamicTypeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .top, spacing: 12) {
                        ProfileAvatar(icon: profile.icon, region: profile.regionCode, plmn: profile.profileOwner.plmn)
                        Text(profile.name).font(.body).fixedSize(horizontal: false, vertical: true)
                    }
                    Text(profile.numberLabel)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    HStack { profileStatus(profile) }
                }
            } else {
                HStack(spacing: 12) {
                    ProfileAvatar(icon: profile.icon, region: profile.regionCode, plmn: profile.profileOwner.plmn)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(profile.name).font(.body).lineLimit(1)
                        Text(profile.numberLabel)
                            .font(.subheadline.monospaced())
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 4)
                    profileStatus(profile)
                }
            }
        }
        .disabled(profileOperation?.profile.id == profile.id || state.loading)
        .accessibilityIdentifier("profileActions-" + profile.id)
    }

    @ViewBuilder private func profileStatus(_ profile: ESIMProfile) -> some View {
        Group {
            if let operation = profileOperation, operation.profile.id == profile.id {
                Text(operation.kind == .enable ? "modem.value.enabling" : "detail.deletingProfile")
            } else {
                Text(profile.enabled ? "detail.lineOn" : "detail.lineOff")
            }
        }
        .foregroundStyle(.secondary)
        if profile.reminder != nil {
            Image(systemName: "bell").font(.caption).foregroundStyle(.secondary)
                .accessibilityLabel(Text("detail.reminder"))
        }
    }

    @ViewBuilder private func physicalActions(_ modem: Modem) -> some View {
        Section {
            DetailEditRow(
                title: "modem.number",
                value: PhoneNumberDisplay.format(modem.number, regionCode: modem.sim.regionCode)
            ) { sheet = .number }
        }
        .disabled(state.needsPIN || modem.sim.identifier.isEmpty)
        LineConnectionSection(state: state, profileID: nil)
            .disabled(modem.sim.identifier.isEmpty)
        if !modem.sim.identifier.isEmpty {
            Section {
                DetailEditRow(title: "detail.reminder") { sheet = .reminder }
            } footer: {
                if let reminder = modem.sim.reminder { ReminderSummary(reminder: reminder) }
            }
        }
        if sim.available {
            Section {
                DetailEditRow(title: "detail.simApplication") { sim.open(); sheet = .sim }
            }
            .disabled(state.needsPIN)
        }
    }
    private func offerPIN() {
        if state.needsPIN, !pinDismissed, sheet == nil, !addingESIM { sheet = .pin }
    }
    private func prepareFlow(seID: String) async {
        guard state.canInstall, !addingESIM else { return }
        let simIdentity = state.simIdentity
        let prepared = ESIMFlowState(detail: state, seID: seID)
        preparingFlow = prepared
        defer {
            if preparingFlow?.id == prepared.id { preparingFlow = nil }
        }
        await prepared.discover()
        guard preparingFlow?.id == prepared.id, !Task.isCancelled,
              tab == .overview,
              session.canAccessDevices, state.canInstall, prepared.validSE,
              state.simIdentity == simIdentity else { return }
        if let discoveries = prepared.discoveries, !discoveries.isEmpty {
            availableFlow = prepared
        } else {
            presentFlow(prepared, showDiscoveries: false)
        }
    }
    private func presentFlow(_ prepared: ESIMFlowState, showDiscoveries: Bool) {
        self.showDiscoveries = showDiscoveries
        flow = prepared
    }
    private func cancelPreparation() {
        preparingFlow?.interrupt()
        preparingFlow = nil
        availableFlow = nil
    }
    private func reload() async {
        await state.load()
        guard !Task.isCancelled else { return }
        presentedFailure = session.failure ?? state.failure ?? state.elementsFailure ?? state.profilesFailure
    }
    private func updatePINPresentation() {
        if state.needsPIN {
            offerPIN()
            return
        }
        pinDismissed = false
        // The form owns the unlock task; closing it early cancels the remaining refresh.
        if !state.busy, sheet == .pin { sheet = nil }
    }
    private func clear() {
        cancelPreparation()
        flow?.interrupt(); flow = nil; sheet = nil; pending = nil; profileOperation = nil; confirmation = nil; presentedFailure = nil
        state.clear(); settings.clear(); sim.stop()
        if session.authentication == .required || session.license == .activation {
            messages.clear()
            messageConversation = nil
            composingMessage = false
        }
    }
}

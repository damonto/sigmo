import SwiftUI

struct ModemNetworkModeView: View {
    let state: ModemSettingsState
    @State private var pending: ModeChange?
    @State private var presentedFailure: BackendFailure?

    private struct ModeChange: Identifiable {
        let id = UUID()
        let mode: ModemModeSelection
    }

    var body: some View {
        ModemSettingsForm(state: state) {
            Section {
                if let modes = state.modes {
                    ForEach(modes.supported) { mode in
                        Button {
                            pending = ModeChange(mode: mode.id)
                        } label: {
                            HStack {
                                Text(mode.label).foregroundStyle(.primary)
                                    .fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 12)
                                if pending?.mode == mode.id {
                                    ProgressView().accessibilityLabel(Text("modemSettings.saving"))
                                } else if mode.id == modes.current.id {
                                    Image(systemName: "checkmark").foregroundStyle(.tint)
                                }
                            }
                        }
                        .disabled(!state.canChangeMode || pending != nil || mode.id == modes.current.id)
                        .accessibilityIdentifier("modemMode-" + mode.id.identifier)
                        .accessibilityAddTraits(mode.id == modes.current.id ? [.isSelected] : [])
                    }
                    if modes.supported.isEmpty { Text("modemSettings.noModes").foregroundStyle(.secondary) }
                }
                ModemSettingsReadError(state: state, section: .mode)
                ModemSettingsReadError(state: state, section: .airplane)
            } footer: {
                if state.airplane?.enabled == true { Text("modemSettings.radioOff") }
            }
        }
        .navigationTitle("modemSettings.mode")
        .navigationBarTitleDisplayMode(.inline)
        .detailErrorAlert($presentedFailure)
        .task(id: pending?.id) {
            guard let change = pending else { return }
            _ = await state.save(.mode(change.mode))
            guard pending?.id == change.id, !Task.isCancelled else { return }
            pending = nil
            presentedFailure = state.failure
        }
        .onDisappear { pending = nil }
    }
}

struct ModemFrequencyBandsView: View {
    let state: ModemSettingsState
    @State private var selected: Set<ModemBandValue>?
    @State private var original: Set<ModemBandValue>?
    @State private var presentedFailure: BackendFailure?
    @Environment(\.dismiss) private var dismiss

    init(state: ModemSettingsState) {
        self.state = state
        let values = state.bands.map { Set($0.current) }
        _selected = State(initialValue: values)
        _original = State(initialValue: values)
    }

    var body: some View {
        ModemSettingsForm(state: state) {
            Section {
                if let bands = state.bands {
                    ForEach(bands.supported) { band in
                        Button {
                            guard var values = selected else { return }
                            if values.contains(band.value) { values.remove(band.value) }
                            else { values.insert(band.value) }
                            selected = values
                        } label: {
                            HStack {
                                Text(band.label).foregroundStyle(.primary)
                                Spacer(minLength: 12)
                                if selected?.contains(band.value) == true {
                                    Image(systemName: "checkmark").foregroundStyle(.tint)
                                }
                            }
                        }
                        .disabled(!state.canChangeBands || selected == nil)
                        .accessibilityIdentifier("modemBand-" + band.value.identifier)
                        .accessibilityAddTraits(selected?.contains(band.value) == true ? [.isSelected] : [])
                    }
                    if bands.supported.isEmpty { Text("modemSettings.noBands").foregroundStyle(.secondary) }
                }
                ModemSettingsReadError(state: state, section: .bands)
                ModemSettingsReadError(state: state, section: .airplane)
            } footer: {
                if state.airplane?.enabled == true { Text("modemSettings.radioOff") }
                else if selected?.isEmpty == true { Text("modemSettings.bandRequired") }
                else { Text("modemSettings.bandsHint") }
            }
        }
        .navigationTitle("modemSettings.bands")
        .navigationBarTitleDisplayMode(.inline)
        .detailErrorAlert($presentedFailure)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done", id: "saveModemBands") {
                    guard let selected, let bands = state.bands else { return }
                    // Keep the server's order while serializing each semantic band once.
                    let values = bands.supported.map(\.value).filter { selected.contains($0) }
                    if await state.save(.bands(values)) { dismiss() }
                    else if !Task.isCancelled { presentedFailure = state.failure }
                }
                .disabled(!state.canSaveBands(selected ?? []))
            }
        }
        .onChange(of: state.bands) { _, bands in
            guard let bands else { selected = nil; original = nil; return }
            let current = Set(bands.current)
            if selected == nil || selected == original { selected = current }
            else { selected?.formIntersection(Set(bands.supported.map(\.value))) }
            original = current
        }
    }
}

struct ModemDeviceSettingsView: View {
    let state: ModemSettingsState
    @State private var draft: ModemDeviceDraft?
    @State private var original: ModemDeviceSettings?
    @State private var presentedFailure: BackendFailure?
    @FocusState private var focused: Field?
    @Environment(\.dismiss) private var dismiss

    private enum Field { case alias, mss }

    init(state: ModemSettingsState) {
        self.state = state
        _draft = State(initialValue: state.device.map(ModemDeviceDraft.init))
        _original = State(initialValue: state.device)
    }

    var body: some View {
        ModemSettingsForm(state: state) {
            Section {
                LabeledContent {
                    TextField("modemSettings.alias", text: Binding(get: { draft?.alias ?? "" }, set: { draft?.alias = $0 }))
                        .multilineTextAlignment(.trailing)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focused, equals: .alias)
                        .submitLabel(.next)
                        .onSubmit { focused = .mss }
                        .disabled(!state.canChangeDevice || draft == nil)
                        .accessibilityIdentifier("modemAlias")
                } label: {
                    Text("modemSettings.alias")
                }
            } footer: {
                Text("modemSettings.aliasHint")
            }
            Section {
                LabeledContent {
                    TextField("modemSettings.mss", text: Binding(get: { draft?.mss ?? "" }, set: { draft?.mss = $0 }))
                        .multilineTextAlignment(.trailing)
                        .keyboardType(.numberPad)
                        .focused($focused, equals: .mss)
                        .disabled(!state.canChangeDevice || draft == nil)
                        .accessibilityIdentifier("modemMSS")
                } label: {
                    Text("modemSettings.mss")
                }
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("modemSettings.mssHint")
                    if draft != nil, draft?.submission == nil {
                        Text("modemSettings.mssInvalid").foregroundStyle(.red)
                    }
                }
            }
            if state.failures[.device] != nil {
                Section { ModemSettingsReadError(state: state, section: .device) }
            }
        }
        .navigationTitle("modemSettings.device")
        .navigationBarTitleDisplayMode(.inline)
        .scrollDismissesKeyboard(.interactively)
        .detailErrorAlert($presentedFailure)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done", id: "saveModemDevice") {
                    guard let settings = draft?.submission else { return }
                    focused = nil
                    if await state.save(.device(settings)) { dismiss() }
                    else if !Task.isCancelled { presentedFailure = state.failure }
                }
                .disabled(draft.map { !state.canSaveDevice($0) } ?? true)
            }
        }
        .onChange(of: state.device) { _, settings in
            guard let settings else { draft = nil; original = nil; return }
            if draft == nil || draft?.submission == original { draft = ModemDeviceDraft(settings) }
            original = settings
        }
    }
}

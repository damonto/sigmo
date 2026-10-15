import SwiftUI

struct DetailTextForm: View {
    enum Kind { case pin, number, rename(ESIMProfile) }
    let state: ModemDetailState
    let kind: Kind
    @State private var target: ModemSIMTarget
    @State private var value: String
    @State private var presentedFailure: BackendFailure?
    @FocusState private var focused: Bool
    @Environment(\.dismiss) private var dismiss

    init(state: ModemDetailState, kind: Kind) {
        self.state = state
        self.kind = kind
        _target = State(initialValue: state.simTarget)
        let initial: String
        switch kind {
        case .pin: initial = ""
        case .number: initial = PhoneNumberDisplay.format(state.modem?.number ?? "", regionCode: state.modem?.sim.regionCode)
        case .rename(let profile): initial = profile.name
        }
        _value = State(initialValue: initial)
    }
    private var title: String {
        switch kind {
        case .pin: "detail.unlock"
        case .number: "modem.number"
        case .rename: "detail.lineLabel"
        }
    }
    private var valid: Bool {
        switch kind {
        case .pin: !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        case .number: !PhoneNumberDisplay.submission(value, regionCode: state.modem?.sim.regionCode).isEmpty
        case .rename: ModemDetailState.validNickname(value)
        }
    }
    var body: some View {
        Form {
            Section {
                if case .pin = kind {
                    DetailValue(title: "modem.lock", value: state.modem?.unlockRequired)
                    SecureField("detail.pin", text: $value).keyboardType(.numberPad).accessibilityIdentifier("detailInput").focused($focused)
                } else {
                    TextField(detailText(title), text: $value)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        .keyboardType(isNumber ? .phonePad : .default)
                        .accessibilityIdentifier("detailInput").focused($focused)
                }
            } footer: {
                if case .rename = kind { Text("detail.nicknameLimit") }
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .detailErrorAlert($presentedFailure)
        .navigationTitle(detailText(title)).navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("common.cancel") { value = ""; dismiss() }.disabled(state.busy)
            }
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done", id: "saveDetail") {
                    let success: Bool
                    switch kind {
                    case .pin: success = await state.unlock(value)
                    case .number: success = await state.updateNumber(value, target: target)
                    case .rename(let profile): success = await state.rename(profile, name: value)
                    }
                    if success { value = ""; dismiss() }
                    else if !Task.isCancelled { presentedFailure = state.failure }
                }.disabled(!valid || state.busy)
            }
        }
        .task { focused = true }
        .interactiveDismissDisabled(state.busy)
        .onChange(of: state.simTarget) { _, current in if current != target { dismiss() } }
        .onDisappear { value = "" }
    }
    private var isNumber: Bool { if case .number = kind { true } else { false } }
}

struct DetailReminderForm: View {
    let state: ModemDetailState
    let profile: ESIMProfile?
    @State private var target: ModemSIMTarget
    @State private var scheduled: Date
    @State private var repeatDays: String
    @State private var content: String
    @State private var clearConfirmation = false
    @State private var clearing = false
    @State private var presentedFailure: BackendFailure?
    @Environment(\.dismiss) private var dismiss

    init(state: ModemDetailState, profile: ESIMProfile?) {
        self.state = state; self.profile = profile
        _target = State(initialValue: state.simTarget)
        let reminder = profile == nil ? state.modem?.sim.reminder : profile?.reminder
        _scheduled = State(initialValue: reminder?.nextAt ?? Date().addingTimeInterval(3600))
        _repeatDays = State(initialValue: reminder?.repeatDays.map(String.init) ?? "")
        _content = State(initialValue: reminder?.content ?? "")
    }
    private var hasReminder: Bool { profile == nil ? state.modem?.sim.reminder != nil : profile?.reminder != nil }
    private var valid: Bool {
        scheduled > Date() && !content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        (repeatDays.isEmpty || (repeatDays.first != "0" && repeatDays.allSatisfy { $0 >= "0" && $0 <= "9" } && (Int(repeatDays).map { (1...3650).contains($0) } ?? false)))
    }
    var body: some View {
        Form {
            Section {
                Text(profile?.name ?? state.modem?.sim.operatorName ?? "").font(.headline)
                DatePicker("detail.scheduledAt", selection: $scheduled)
                TextField("detail.repeatDays", text: $repeatDays).keyboardType(.numberPad)
                TextField("detail.reminderContent", text: $content, axis: .vertical).lineLimit(3...8)
                    .accessibilityIdentifier("reminderContent")
            } footer: { Text("detail.repeatHint") }
            if hasReminder {
                Section { Button("detail.clearReminder", role: .destructive) { clearConfirmation = true }.disabled(state.busy) }
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .detailErrorAlert($presentedFailure)
        .navigationTitle("detail.reminder").navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() }.disabled(state.busy) }
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done", id: "saveReminder") {
                    let value = ReminderSubmission(scheduledAt: scheduled, repeatDays: Int(repeatDays), content: content.trimmingCharacters(in: .whitespacesAndNewlines))
                    if await state.saveReminder(profile: profile, value: value, target: target) { dismiss() }
                    else if !Task.isCancelled { presentedFailure = state.failure }
                }.disabled(!valid || state.busy)
            }
        }
        .interactiveDismissDisabled(state.busy)
        .alert("detail.clearReminder", isPresented: $clearConfirmation) {
            Button("common.delete", role: .destructive) { clearing = true }
            Button("common.cancel", role: .cancel) {}
        } message: {
            Text("detail.clearReminderMessage")
        }
        .task(id: clearing) {
            if clearing {
                if await state.saveReminder(profile: profile, value: nil, target: target) { dismiss() }
                else if !Task.isCancelled { presentedFailure = state.failure }
                clearing = false
            }
        }
        .onDisappear { clearing = false }
        .onChange(of: state.simTarget) { _, current in if current != target { dismiss() } }
    }
}

struct SIMApplicationView: View {
    @Bindable var state: SIMApplicationState
    @State private var presentedFailure: BackendFailure?
    private var needsBack: Bool {
        switch state.page {
        case .menu(let menu): menu.kind != "root"
        case nil: false
        default: true
        }
    }
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        Form {
            switch state.page {
            case .menu(let menu):
                Section(menu.title ?? detailText("detail.simApplication")) {
                    ForEach(menu.items) { item in
                        AsyncActionButton(action: { await state.send(.init(type: "menu_selection", itemId: item.id, helpRequested: false)) }) {
                            HStack {
                                Text(item.label)
                                Spacer()
                                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                            }.frame(maxWidth: .infinity).contentShape(Rectangle())
                        }.foregroundStyle(.primary).accessibilityLabel(Text(item.label))
                        if menu.helpAvailable == true {
                            AsyncActionButton(title: "detail.help") { await state.send(.init(type: "menu_selection", itemId: item.id, helpRequested: true)) }
                        }
                    }
                }
            case .display(let value):
                Section { Text(value.text ?? "").fixedSize(horizontal: false, vertical: true) }
                AsyncActionButton(title: "detail.confirm") { await state.send(.init(type: "confirm_response", accepted: true)) }
            case .input(let value):
                Section {
                    Text(value.text ?? "")
                    if value.hideInput == true { SecureField("detail.response", text: $state.input) }
                    else { TextField("detail.response", text: $state.input).textInputAutocapitalization(.never).autocorrectionDisabled() }
                    AsyncActionButton(title: "detail.confirm") { await state.send(.init(type: "input_response", text: state.input)) }
                        .disabled(state.input.count < (value.minLength ?? 0) || ((value.maxLength ?? 255) > 0 && state.input.count > (value.maxLength ?? 255)))
                }
            case .inkey(let value):
                Section {
                    Text(value.text ?? "")
                    if value.yesNo == true {
                        AsyncActionButton(title: "common.yes") { await state.send(.init(type: "inkey_response", text: "Y")) }
                        AsyncActionButton(title: "common.no") { await state.send(.init(type: "inkey_response", text: "N")) }
                    } else {
                        TextField("detail.response", text: $state.input)
                        AsyncActionButton(title: "detail.confirm") { await state.send(.init(type: "inkey_response", text: state.input)) }.disabled(state.input.count != 1)
                    }
                }
            case .confirm(let value):
                Section {
                    Text(value.text ?? "")
                    AsyncActionButton(title: "common.yes") { await state.send(.init(type: "confirm_response", accepted: true)) }
                    AsyncActionButton(title: "common.no") { await state.send(.init(type: "confirm_response", accepted: false)) }
                }
            case nil: ContentUnavailableView("detail.simApplicationUnavailable", systemImage: "simcard")
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .disabled(state.busy)
        .detailErrorAlert($presentedFailure)
        .onChange(of: state.failure) { _, failure in presentedFailure = failure }
        .navigationTitle("detail.simApplication").navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done") { await state.dismiss(); dismiss() }
            }
            if needsBack {
                ToolbarItem(placement: .topBarLeading) {
                    AsyncActionButton(action: { await state.send(.init(type: "back")) }) {
                        Label("detail.back", systemImage: "chevron.left").labelStyle(.iconOnly)
                    }
                }
            }
        }
        .interactiveDismissDisabled()
    }
}

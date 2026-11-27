import SwiftUI

struct SIMPINView: View {
    @Bindable var state: SIMPINState
    @Environment(\.dismiss) private var dismiss
    @FocusState private var focus: Field?
    @State private var submission: UUID?
    private enum Field: Hashable { case current, replacement, confirmation }

    var body: some View {
        Form {
            protectionSection
            if let action = state.action {
                Section {
                    SecureField(action == .puk ? "simPin.puk" : "simPin.currentPin", text: $state.pin)
                        .focused($focus, equals: .current)
                        .accessibilityIdentifier("simPINCurrent")
                    if action.needsNewPIN {
                        SecureField("simPin.newPin", text: $state.newPIN)
                            .focused($focus, equals: .replacement)
                            .accessibilityIdentifier("simPINNew")
                        SecureField("simPin.confirmPin", text: $state.confirmation)
                            .focused($focus, equals: .confirmation)
                            .accessibilityIdentifier("simPINConfirmation")
                    }
                } header: {
                    Text(action.title)
                } footer: {
                    if let message = state.attemptMessage {
                        Text(message)
                    }
                }
                .keyboardType(.numberPad)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .disabled(state.busy || state.loading)
                Section {
                    LoadingButton(isWorking: submission != nil || state.busy) {
                        focus = nil
                        submission = UUID()
                    } label: {
                        Text(action.title)
                    }
                    .disabled(!state.canSubmit)
                    .accessibilityIdentifier("simPINSubmit")
                    if action == .enable || action == .disable || action == .change {
                        Button("common.cancel", role: .cancel) { state.choose(nil) }
                            .disabled(state.busy)
                    }
                }
            }
        }
        .detailErrorAlert(state.failure)
        .navigationTitle("simPin.title")
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(state.busy)
        .interactiveDismissDisabled(state.busy)
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("simPin.done") { focus = nil }
            }
        }
        .task { await state.load() }
        .onChange(of: state.identity) { _, _ in
            state.reset()
            dismiss()
        }
        .onChange(of: state.targetChanged) { _, changed in
            if changed { dismiss() }
        }
        .task(id: submission) {
            guard let current = submission else { return }
            await state.submit()
            guard submission == current, !Task.isCancelled else { return }
            if state.legacy, state.acknowledged { dismiss() }
            submission = nil
        }
        .onChange(of: state.action) { _, action in
            focus = action == nil ? nil : .current
        }
        .onDisappear {
            submission = nil
            focus = nil
            state.leave()
        }
    }

    @ViewBuilder private var protectionSection: some View {
        let readingStatus = state.loading && !state.busy
        let enabled = protectionEnabled
        if readingStatus || enabled != nil {
            Section {
                protectionToggle(enabled: enabled ?? false, readingStatus: readingStatus)
                if enabled == true, state.action == nil {
                    Button("simPin.actions.change") { state.choose(.change) }
                        .disabled(state.busy || state.loading)
                }
            }
        } else if state.status != nil {
            Section { Text(state.statusMessage) }
        }
    }

    private var protectionEnabled: Bool? {
        guard let status = state.status, status.supported, status.lock == .unlocked else { return nil }
        return status.enabled
    }

    private func protectionToggle(enabled: Bool, readingStatus: Bool) -> some View {
        let title = Text("simPin.title")
        return LabeledContent {
            Toggle(isOn: Binding(
                get: { enabled },
                set: { state.choose($0 ? .enable : .disable) }
            )) {
                title
            }
            .labelsHidden()
            .fixedSize()
            .opacity(readingStatus ? 0.5 : 1)
            .disabled(state.busy || state.loading || state.action != nil)
            .overlay {
                if readingStatus {
                    ProgressView()
                        .controlSize(.small)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            .accessibilityValue(Text("simPin.loading"), isEnabled: readingStatus)
            .accessibilityIdentifier("simPINProtection")
        } label: {
            title.accessibilityHidden(true)
        }
    }
}

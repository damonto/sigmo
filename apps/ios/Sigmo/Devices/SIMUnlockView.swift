import SwiftUI

// Unlocking after activation has its own state and presentation, separate from PIN settings.
struct SIMUnlockView: View {
    @State private var state: SIMPINState
    let unlocked: () -> Void
    @State private var presentedFailure: BackendFailure?
    @FocusState private var focus: Field?
    @Environment(\.dismiss) private var dismiss
    private enum Field: Hashable { case current, replacement, confirmation }

    init(session: BackendSession, modemID: String, unlocked: @escaping () -> Void) {
        _state = State(initialValue: SIMPINState(session: session, modemID: modemID))
        self.unlocked = unlocked
    }

    var body: some View {
        Form {
            if state.loading && !state.busy {
                Section { ProgressView("simPin.loading") }
            }
            if state.action == .pin || state.action == .puk {
                Section {
                    SecureField(state.action == .puk ? "simPin.puk" : "detail.pin", text: $state.pin)
                        .focused($focus, equals: .current)
                        .accessibilityIdentifier("simUnlockPIN")
                    if state.action == .puk {
                        SecureField("simPin.newPin", text: $state.newPIN)
                            .focused($focus, equals: .replacement)
                            .accessibilityIdentifier("simUnlockNewPIN")
                        SecureField("simPin.confirmPin", text: $state.confirmation)
                            .focused($focus, equals: .confirmation)
                            .accessibilityIdentifier("simUnlockConfirmation")
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        if state.action == .puk {
                            Text("simPin.locks.puk")
                        }
                        if let message = state.attemptMessage {
                            Text(message)
                        }
                    }
                    .accessibilityElement(children: .contain)
                }
                .keyboardType(.numberPad)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .disabled(state.busy || state.loading)
            } else if !state.loading, state.status != nil {
                Section { Text(state.statusMessage) }
            }
        }
        .navigationTitle(Text(LocalizedStringKey(state.action == .puk ? "simPin.actions.puk" : "detail.unlock")))
        .navigationBarTitleDisplayMode(.inline)
        .interactiveDismissDisabled(state.busy)
        .detailErrorAlert($presentedFailure)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("common.cancel") { dismiss() }
                    .disabled(state.busy)
            }
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "common.done", id: "simUnlockSubmit") {
                    focus = nil
                    await state.submit(refreshAfterSuccess: false)
                    guard !Task.isCancelled else { return }
                    if state.acknowledged, state.failure == nil {
                        unlocked()
                        dismiss()
                    } else {
                        presentedFailure = state.failure
                    }
                }
                .disabled(!state.canSubmit)
            }
        }
        .task {
            await state.load()
            guard !Task.isCancelled else { return }
            presentedFailure = state.failure
            focus = .current
            if state.status?.lock == .unlocked {
                unlocked()
                dismiss()
            }
        }
        .onChange(of: state.identity) { _, _ in
            state.reset()
            dismiss()
        }
        .onChange(of: state.targetChanged) { _, changed in
            if changed { dismiss() }
        }
        .onChange(of: presentedFailure) { _, failure in
            if failure == nil, state.acknowledged {
                unlocked()
                dismiss()
            }
        }
        .onDisappear {
            focus = nil
            state.leave()
        }
    }
}

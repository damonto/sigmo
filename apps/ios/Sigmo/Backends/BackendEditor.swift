import SwiftUI

struct BackendEditor: View {
    @Environment(BackendStore.self) private var store
    @Environment(\.dismiss) private var dismiss
    private let backendID: UUID
    private let isNew: Bool
    @State private var name: String
    @State private var address: String
    @FocusState private var nameFocused: Bool

    init(configuration: BackendConfiguration? = nil) {
        backendID = configuration?.id ?? UUID()
        isNew = configuration == nil
        _name = State(initialValue: configuration?.name ?? "")
        _address = State(initialValue: configuration?.displayAddress ?? "")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("backends.connection") {
                    TextField("configuration.name", text: $name)
                        .focused($nameFocused)
                        .accessibilityIdentifier("backendName")
                    TextField("backends.address", text: $address)
                        .keyboardType(.URL).textContentType(.URL)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        .accessibilityIdentifier("backendAddress")
                }
            }
            .detailErrorAlert(Binding(get: { store.failure }, set: { store.failure = $0 }))
            .navigationTitle(isNew ? "backends.add" : "backends.edit")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.save") {
                        if store.saveBackend(id: backendID, name: name, address: address) {
                            let session = store.sessions[backendID]
                            dismiss()
                            // The saved backend owns this refresh even after the editor closes.
                            Task { await session?.refresh(force: true) }
                        }
                    }
                    .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || address.isEmpty)
                    .accessibilityIdentifier("saveBackend")
                }
            }
        }
        .task {
            if isNew { nameFocused = true }
        }
    }
}

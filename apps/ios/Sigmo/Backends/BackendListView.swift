import SwiftUI

private enum BackendListSheet: String, Identifiable {
    case add, order

    var id: Self { self }
}

struct BackendListView: View {
    @Environment(BackendStore.self) private var store
    @State private var sheet: BackendListSheet?
    @State private var refreshFailure: BackendFailure?

    var body: some View {
        List {
            ForEach(store.settings.backends) { backend in
                if let session = store.sessions[backend.id] {
                    Section {
                        NavigationLink(value: BackendDestination.backend(backend.id)) {
                            VStack(alignment: .leading, spacing: 6) {
                                HStack(alignment: .firstTextBaseline, spacing: 6) {
                                    Text(backend.name).font(.headline)
                                    BackendStatusLabel(session: session)
                                        .labelStyle(.iconOnly)
                                }
                                Text(backend.displayAddress).font(.subheadline).foregroundStyle(.secondary)
                            }
                            .padding(.vertical, 4)
                        }
                        .accessibilityIdentifier("backend-\(backend.id)")
                    }
                }
            }
            #if DEBUG
            Section {
                NavigationLink { DiagnosticsView() } label: {
                    Label("diagnostics.title", systemImage: "stethoscope")
                }
                .accessibilityIdentifier("openDiagnostics")
            }
            #endif
        }
        .listStyle(.insetGrouped)
        .listSectionSpacing(12)
        .navigationTitle("backends.manage")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            refreshFailure = nil
            await store.refreshAll()
            guard !Task.isCancelled else { return }
            refreshFailure = store.orderedSessions().compactMap(\.failure).first
        }
        .detailErrorAlert(sheet == nil ? store.failure ?? refreshFailure : nil)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("backends.reorder", systemImage: "arrow.up.arrow.down") { sheet = .order }
                    .disabled(store.settings.backends.count < 2)
                    .accessibilityIdentifier("reorderBackends")
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button("backends.add", systemImage: "plus") { sheet = .add }
                    .disabled(!store.configurationLoaded)
                    .accessibilityIdentifier("addBackend")
            }
        }
        .sheet(item: $sheet) { value in
            switch value {
            case .add: BackendEditor()
            case .order: BackendOrderView()
            }
        }
    }
}

private struct BackendOrderView: View {
    @Environment(BackendStore.self) private var store
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                ForEach(store.settings.backends) { backend in
                    Text(backend.name).accessibilityIdentifier("orderBackend-\(backend.id)")
                }
                .onMove { store.moveBackends(from: $0, to: $1) }
            }
            .detailErrorAlert(Binding(get: { store.failure }, set: { store.failure = $0 }))
            .environment(\.editMode, .constant(.active))
            .navigationTitle("backends.reorder")
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("common.done") { dismiss() } } }
        }
    }
}

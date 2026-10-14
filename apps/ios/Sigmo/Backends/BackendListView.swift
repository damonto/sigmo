import SwiftUI

private enum BackendListSheet: Identifiable {
    case editor(BackendConfiguration?)
    case order

    var id: String {
        switch self {
        case .editor(let configuration): "edit-\(configuration?.id.uuidString ?? "new")"
        case .order: "order"
        }
    }
}

struct BackendListView: View {
    @Environment(BackendStore.self) private var store
    @Environment(CallsState.self) private var calls
    @Environment(\.demoSession) private var demoSession
    @State private var demo: DemoPresentation?
    @State private var sheet: BackendListSheet?
    @State private var refreshFailure: BackendFailure?
    var open: (BackendDestination) -> Void

    var body: some View {
        List {
            ForEach(store.settings.backends) { backend in
                if let session = store.sessions[backend.id] {
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
                    .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                        Button("common.edit", systemImage: "pencil") { sheet = .editor(backend) }
                            .tint(.orange)
                            .accessibilityIdentifier("editBackend-\(backend.id)")
                        Button("common.settings", systemImage: "gearshape") { open(.settings(backend.id)) }
                            .tint(.blue)
                            .accessibilityIdentifier("settingsBackend-\(backend.id)")
                    }
                }
            }
            if demoSession == nil {
                Section {
                    Button {
                        do { demo = try DemoPresentation() }
                        catch { store.failure = .wrapping(error) }
                    } label: {
                        Label("demo.try", systemImage: "play.rectangle")
                    }
                    .disabled(calls.hasCall)
                    .accessibilityIdentifier("tryDemo")
                } footer: { Text("demo.entryDescription") }
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
                Button("backends.add", systemImage: "plus") { sheet = .editor(nil) }
                    .disabled(!store.configurationLoaded)
                    .accessibilityIdentifier("addBackend")
            }
        }
        .fullScreenCover(item: demoBinding) { demo in
            DemoView(presentation: demo, liveCalls: calls)
        }
        .sheet(item: $sheet) { value in
            switch value {
            case .editor(let configuration): BackendEditor(configuration: configuration)
            case .order: BackendOrderView()
            }
        }
    }

    private var demoBinding: Binding<DemoPresentation?> {
        Binding(get: { demo }, set: { next in
            let previous = demo
            demo = next
            if previous?.id != next?.id {
                Task { await previous?.close() }
            }
        })
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

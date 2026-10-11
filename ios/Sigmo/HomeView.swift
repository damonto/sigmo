import SwiftUI

struct HomeView: View {
    @Environment(BackendStore.self) private var store
    @Environment(\.nativePush) private var push
    @Environment(\.scenePhase) private var scenePhase
    @State private var path: [BackendDestination] = []
    @State private var adding = false
    @State private var selectedDevice: BackendDestination?
    @State private var selectedBackendID: UUID?
    @State private var search = ""

    var body: some View {
        Group {
            if let selectedDevice {
                destinationView(selectedDevice)
            } else {
                homeNavigation
            }
        }
        .modifier(CallPresentation())
        .sheet(isPresented: $adding) { BackendEditor() }
        .onChange(of: store.settings.backends.map(\.id)) { _, ids in
            if let id = selectedDevice?.backendID, !ids.contains(id) { selectedDevice = nil }
            if let removed = path.firstIndex(where: { destination in
                destination.backendID.map { !ids.contains($0) } ?? false
            }) {
                path.removeSubrange(removed...)
            }
        }
        .onChange(of: store.onlineSessions.map(\.id)) { _, ids in
            if let selectedBackendID, !ids.contains(selectedBackendID) { self.selectedBackendID = nil }
        }
        .onChange(of: push?.pendingNavigation?.id, initial: true) { _, _ in navigateFromNotification() }
        .onChange(of: store.configurationLoaded) { _, _ in navigateFromNotification() }
        .onChange(of: push?.hasIdentity) { _, _ in navigateFromNotification() }
        .onChange(of: scenePhase) { _, _ in navigateFromNotification() }
        .alert("push.notification", isPresented: Binding(get: { push?.navigationFailure != nil }, set: { if !$0 { push?.navigationFailure = nil } })) {
            Button("common.done") { push?.navigationFailure = nil }
        } message: { Text(push?.navigationFailure?.message ?? "") }
    }

    // Device tabs own their navigation stacks; keeping Home outside them also
    // preserves its search and connection filter while switching workspaces.
    private var homeNavigation: some View {
        NavigationStack(path: $path) {
            HomeDevicesView(backendID: selectedBackendID, search: search, add: { adding = true }, open: { selectedDevice = $0 })
                .safeAreaInset(edge: .top, spacing: 0) {
                    if !store.settings.backends.isEmpty {
                        BackendFilterBar(selection: $selectedBackendID)
                            .padding(.horizontal, 16)
                            .padding(.bottom, 4)
                    }
                }
                .navigationTitle("Sigmo")
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $search, placement: .navigationBarDrawer(displayMode: .always), prompt: "home.search")
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
                .refreshable { await store.refreshAll() }
                .navigationDestination(for: BackendDestination.self) { destination in
                    destinationView(destination)
                }
                .toolbar {
                    // Keep the toolbar aligned while opening a 10-point search gap.
                    ToolbarItem(placement: .principal) {
                        Text("Sigmo")
                            .font(.headline)
                            .accessibilityAddTraits(.isHeader)
                            .offset(y: -5)
                    }
                    ToolbarItem(placement: .topBarLeading) {
                        toolbarButton("backends.manage", symbol: "server.rack") {
                            path.append(.management)
                        }
                        .accessibilityIdentifier("manageBackends")
                        .offset(y: -5)
                    }
                    .sharedBackgroundVisibility(.hidden)
                    ToolbarItem(placement: .topBarTrailing) {
                        toolbarButton("backends.add", symbol: "plus") { adding = true }
                            .disabled(!store.configurationLoaded)
                            .accessibilityIdentifier("addBackend")
                            .offset(y: -5)
                    }
                    .sharedBackgroundVisibility(.hidden)
                }
        }
    }

    private func toolbarButton(_ title: LocalizedStringKey, symbol: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 20, weight: .semibold))
                .frame(width: 24, height: 24)
        }
        .buttonStyle(.glass)
        .buttonBorderShape(.circle)
        .controlSize(.large)
        .frame(minWidth: 44, minHeight: 44)
        .accessibilityLabel(Text(title))
    }

    @ViewBuilder
    private func destinationView(_ destination: BackendDestination) -> some View {
        if destination == .management {
            BackendListView { path.append($0) }
        } else if let id = destination.backendID, let session = store.sessions[id] {
            Group {
                switch destination {
                case .management: EmptyView()
                case .backend: BackendDetailView(session: session)
                case .settings: BackendSettingsView(session: session)
                case .modem(_, let modemID):
                    ModemDetailView(session: session, modemID: modemID) { selectedDevice = nil }
                case .messages(_, let modemID, let conversationID):
                    if session.canAccessDevices {
                        ModemDetailView(session: session, modemID: modemID, initialConversationID: conversationID) {
                            selectedDevice = nil
                        }
                    } else {
                        NavigationStack {
                            SettingsAccessView(session: session) { EmptyView() }
                                .navigationTitle(session.configuration.name)
                                .toolbar {
                                    ToolbarItem(placement: .topBarLeading) {
                                        Button("devices.title", systemImage: "chevron.backward") { selectedDevice = nil }
                                            .accessibilityIdentifier("backToDevices")
                                    }
                                }
                        }
                    }
                case .reminder(_, let modemID, let profileID, let profileType, let seID):
                    ReminderDestinationView(session: session, modemID: modemID, profileID: profileID, profileType: profileType, seID: seID)
                }
            }
            // Endpoint changes must also replace view-owned state.
            .id(destination)
            .id(ObjectIdentifier(session))
        }
    }

    private func navigateFromNotification() {
        guard let destination = push?.takeNavigation(in: store, active: scenePhase == .active) else { return }
        adding = false
        switch destination {
        case .modem, .messages:
            path = []
            selectedDevice = destination
        default:
            selectedDevice = nil
            path = [destination]
        }
        AppDiagnostics.record("notification navigation applied to active scene")
    }
}


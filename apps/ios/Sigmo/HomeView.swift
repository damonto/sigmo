import SwiftUI

private enum HomeTab: Hashable { case devices, messages, calls, contacts }

struct HomeView: View {
    private struct CommunicationIdentity: Hashable {
        let session: ObjectIdentifier
        let backendID: String
        let accessRevision: Int
    }

    @State private var homeTab = HomeTab.devices
    @AppStorage("communication.backend") private var communicationBackend = ""
    @Environment(BackendStore.self) private var store
    @Environment(CallsState.self) private var calls
    @Environment(\.nativePush) private var push
    @Environment(\.scenePhase) private var scenePhase
    @State private var path: [BackendDestination] = []
    @State private var adding = false
    @State private var selectedDevice: BackendDestination?
    @State private var selectedBackendID: UUID?
    @State private var search = ""
    @State private var refreshFailure: BackendFailure?
    @State private var communicationDial: CommunicationDialRequest?
    @State private var modemDial: ModemDialRequest?

    init() {
        #if DEBUG
        if ProcessInfo.processInfo.environment["SIGMO_UI_SCENARIO"] == "detail-reminder" {
            _path = State(initialValue: [.reminder(PreviewBackends.homeID, "device-1", "8986000000000000002", "esim", "se-a")])
        }
        #endif
    }

    var body: some View {
        Group {
            if let selectedDevice {
                destinationView(selectedDevice)
            } else {
                homeTabs
            }
        }
        .modifier(CallPresentation())
        .sheet(isPresented: $adding) { BackendEditor() }
        .onChange(of: store.settings.backends.map(\.id), initial: true) { _, ids in
            if !ids.contains(where: { $0.uuidString == communicationBackend }) { communicationBackend = "" }
            if let id = selectedDevice?.backendID, !ids.contains(id) { selectedDevice = nil }
            if let removed = path.firstIndex(where: { destination in
                destination.backendID.map { !ids.contains($0) } ?? false
            }) {
                path.removeSubrange(removed...)
            }
        }
        .onChange(of: store.onlineSessions.map(\.id)) { _, ids in
            if ids.count < 2 || selectedBackendID.map({ !ids.contains($0) }) == true {
                selectedBackendID = nil
            }
        }
        .onChange(of: push?.pendingNavigation?.id, initial: true) { _, _ in navigateFromNotification() }
        .onChange(of: store.configurationLoaded) { _, _ in navigateFromNotification() }
        .onChange(of: push?.hasIdentity) { _, _ in navigateFromNotification() }
        .onChange(of: scenePhase) { _, _ in navigateFromNotification() }
        .task(id: calls.systemCallback?.id) { await applySystemCallback() }
        .alert("push.notification", isPresented: Binding(get: { push?.navigationFailure != nil }, set: { if !$0 { push?.navigationFailure = nil } })) {
            Button("common.done") { push?.navigationFailure = nil }
        } message: { Text(push?.navigationFailure?.message ?? "") }
    }

    private var homeTabs: some View {
        TabView(selection: $homeTab) {
            Tab("communication.devices", systemImage: "server.rack", value: HomeTab.devices) { homeNavigation }
            Tab("communication.messages", systemImage: "message", value: HomeTab.messages) { communicationTab(.messages) }
            Tab("communication.calls", systemImage: "phone", value: HomeTab.calls) { communicationTab(.calls) }
            Tab("communication.contacts", systemImage: "person.crop.circle", value: HomeTab.contacts) { communicationTab(.contacts) }
        }
    }

    private func communicationTab(_ section: CommunicationSection) -> some View {
        NavigationStack {
            let sessions = store.orderedSessions(backendID: UUID(uuidString: communicationBackend))
            CommunicationWorkspace(sessions: sessions, section: section,
                                   callbackDial: section == .calls ? $communicationDial : .constant(nil))
                .id(sessions.map {
                    CommunicationIdentity(session: ObjectIdentifier($0), backendID: $0.communicationBackendID,
                                          accessRevision: $0.accessRevision)
                })
                .toolbar {
                    if store.settings.backends.count > 1 {
                        ToolbarItem(placement: .topBarLeading) {
                            CommunicationBackendFilter(selection: Binding(
                                get: { UUID(uuidString: communicationBackend) },
                                set: { communicationBackend = $0?.uuidString ?? "" }
                            ))
                        }
                    }
                }
        }.id(communicationBackend)
    }

    // Device tabs own their navigation stacks; keeping Home outside them also
    // preserves its search and connection filter while switching workspaces.
    private var homeNavigation: some View {
        NavigationStack(path: $path) {
            HomeDevicesView(backendID: selectedBackendID, search: $search, add: { adding = true }, open: { selectedDevice = $0 })
                .safeAreaInset(edge: .top, spacing: 0) {
                    if store.onlineSessions.count > 1 {
                        BackendFilterBar(selection: $selectedBackendID)
                            .padding(.horizontal, 16)
                            .padding(.bottom, 4)
                    }
                }
                .navigationTitle("Sigmo")
                .navigationBarTitleDisplayMode(.large)
                .refreshable {
                    await store.refreshAll()
                    guard !Task.isCancelled else { return }
                    refreshFailure = store.orderedSessions(backendID: selectedBackendID).compactMap(\.failure).first
                }
                .detailErrorAlert($refreshFailure)
                .navigationDestination(for: BackendDestination.self) { destination in
                    destinationView(destination)
                }
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("backends.manage", systemImage: "server.rack") {
                            path.append(.management)
                        }
                        .accessibilityIdentifier("manageBackends")
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("backends.add", systemImage: "plus") { adding = true }
                            .disabled(!store.configurationLoaded)
                            .accessibilityIdentifier("addBackend")
                    }
                }
        }
        .toolbarVisibility(path.isEmpty ? .visible : .hidden, for: .tabBar)
    }

    @ViewBuilder
    private func destinationView(_ destination: BackendDestination) -> some View {
        if destination == .management {
            BackendListView()
        } else if let id = destination.backendID, let session = store.sessions[id] {
            Group {
                switch destination {
                case .management: EmptyView()
                case .backend: BackendDetailView(session: session)
                case .settings: BackendSettingsView(session: session)
                case .modem(_, let modemID):
                    let request = modemDialRequest(backendID: id, modemID: modemID)
                    ModemDetailView(
                        session: session,
                        modemID: modemID,
                        callRequest: request,
                        callRequestHandled: {
                            guard let request, modemDial?.id == request.id else { return }
                            modemDial = nil
                        }
                    ) { selectedDevice = nil }
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
        AppDiagnostics.record("notification.navigation.applied", category: "push")
    }

    private func modemDialRequest(backendID: UUID, modemID: String) -> ModemDialRequest? {
        guard let modemDial, modemDial.backendID == backendID, modemDial.modemID == modemID else { return nil }
        return modemDial
    }

    private func applySystemCallback() async {
        guard let request = calls.systemCallback else { return }
        let destination = await calls.resolveSystemCallback(request)
        guard !Task.isCancelled, calls.systemCallback?.id == request.id else { return }
        prepareForCallbackNavigation()
        let route: String
        switch destination {
        case .home(let number, let backendID):
            route = "home"
            homeTab = .calls
            let existingBackend = backendID.flatMap { store.sessions[$0] == nil ? nil : $0 }
            if let existingBackend { communicationBackend = existingBackend.uuidString }
            communicationDial = CommunicationDialRequest(number: number, backendID: existingBackend, modemID: nil)
            modemDial = nil
            calls.consumeSystemCallback(request)
        case .modem(let number, let backendID, let modemID):
            route = "modem"
            homeTab = .devices
            selectedDevice = .modem(backendID, modemID)
            modemDial = ModemDialRequest(backendID: backendID, modemID: modemID, number: number)
            communicationDial = nil
            calls.consumeSystemCallback(request)
        case .dial(let number, let line):
            route = "dial"
            homeTab = .calls
            communicationBackend = line.backendID.uuidString
            await calls.dial(number: number, line: line)
            guard calls.systemCallback?.id == request.id else { return }
            calls.consumeSystemCallback(request)
        }
        AppDiagnostics.record("callback.navigation.applied", category: "calls", fields: ["route": route])
    }

    private func prepareForCallbackNavigation() {
        adding = false
        selectedDevice = nil
        path = []
    }
}


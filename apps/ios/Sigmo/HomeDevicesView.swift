import SwiftUI

struct HomeDevice: Identifiable {
    let session: BackendSession
    let modem: Modem
    var id: BackendDestination { .modem(session.id, modem.id) }
}

extension BackendStore {
    func homeDevices(backendID: UUID?, search: String = "") -> [HomeDevice] {
        let terms = search.split(whereSeparator: \.isWhitespace).map(String.init)
        return onlineSessions.filter {
            backendID == nil || backendID == $0.id
        }.flatMap { session in
            session.modems.compactMap { modem -> HomeDevice? in
                if terms.isEmpty { return HomeDevice(session: session, modem: modem) }
                let fields = [modem.displayName, modem.carrier, modem.number,
                              PhoneNumberDisplay.format(modem.number, regionCode: modem.sim.regionCode),
                              modem.number.filter(\.isNumber), session.configuration.name, modemValue(modem.simKind)]
                guard terms.allSatisfy({ term in fields.contains { $0.localizedStandardContains(term) } }) else { return nil }
                return HomeDevice(session: session, modem: modem)
            }
        }
    }
}

struct HomeDevicesView: View {
    @Environment(BackendStore.self) private var store
    @Environment(CallsState.self) private var calls
    @Environment(\.demoSession) private var demoSession
    @State private var demo: DemoPresentation?
    @State private var demoFailure: BackendFailure?
    let backendID: UUID?
    @Binding var search: String
    var add: () -> Void
    var open: (BackendDestination) -> Void

    var body: some View {
        Group {
            if store.settings.backends.isEmpty && store.configurationLoaded && store.failure == nil {
                emptyState
            } else {
                list
            }
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .detailErrorAlert(store.failure)
        .fullScreenCover(item: demoBinding) { demo in
            DemoView(presentation: demo, liveCalls: calls)
        }
    }

    private var emptyState: some View {
        ScrollView {
            VStack(spacing: 32) {
                VStack(spacing: 16) {
                    Image(systemName: "server.rack")
                        .font(.largeTitle)
                        .imageScale(.large)
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    Text("home.welcome.title")
                        .font(.title.bold())
                        .accessibilityAddTraits(.isHeader)
                    Text("home.welcome.description")
                        .foregroundStyle(.secondary)
                }

                VStack(spacing: 16) {
                    Button("backends.connect", systemImage: "server.rack", action: add)
                        .buttonStyle(.glassProminent)
                        .controlSize(.large)
                        .buttonSizing(.flexible)
                        .accessibilityIdentifier("connectSigmo")

                    if demoSession == nil {
                        Button {
                            do { demo = try DemoPresentation() }
                            catch { demoFailure = .wrapping(error) }
                        } label: {
                            Text("demo.try")
                                .font(.body.weight(.semibold))
                                .frame(maxWidth: .infinity, minHeight: 44)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.borderless)
                        .tint(.accentColor)
                        .disabled(calls.hasCall)
                        .accessibilityHint(Text("demo.entryDescription"))
                        .accessibilityIdentifier("tryDemo")
                    }
                }
            }
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: 480)
            .padding(24)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.center, for: .alignment)
        .scrollBounceBehavior(.basedOnSize)
        .detailErrorAlert($demoFailure)
        .accessibilityIdentifier("homeWelcome")
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

    private var list: some View {
        List {
            if !store.settings.backends.isEmpty {
                let sessions = store.orderedSessions(backendID: backendID)
                let devices = store.homeDevices(backendID: backendID, search: search)
                let groups = Dictionary(grouping: devices, by: { $0.session.id })
                if devices.isEmpty && !search.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    ContentUnavailableView.search(text: search)
                        .listRowSeparator(.hidden)
                        .accessibilityIdentifier("homeSearchEmpty")
                } else if devices.isEmpty && !sessions.isEmpty && sessions.allSatisfy({ !$0.isRefreshing }) {
                    ContentUnavailableView("devices.empty", systemImage: "simcard")
                        .listRowSeparator(.hidden)
                        .accessibilityIdentifier("homeDevicesEmpty")
                }
                ForEach(sessions) { session in
                    if let devices = groups[session.id] {
                        Section {
                            ForEach(devices) { device in
                                deviceRow(device)
                            }
                        } header: {
                            Text(session.configuration.name)
                                .listRowInsets(.top, 0)
                                .accessibilityIdentifier("backendHeading-\(session.id)")
                        }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .contentMargins(.top, 0, for: .scrollContent)
        .scrollDismissesKeyboard(.interactively)
        .searchable(text: $search, placement: .navigationBarDrawer(displayMode: .always), prompt: "home.search")
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
    }

    private func deviceRow(_ device: HomeDevice) -> some View {
        Button { open(device.id) } label: {
            HStack(spacing: 8) {
                ModemRow(modem: device.modem, capabilities: device.session.capabilities ?? [],
                         backendID: device.session.id,
                         unreadCount: device.session.unreadCounts[device.modem.id] ?? 0)
                Image(systemName: "chevron.forward")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))
        .alignmentGuide(.listRowSeparatorLeading) { _ in 52 }
        .accessibilityIdentifier("modem-\(device.session.id)-\(device.modem.id)")
    }
}

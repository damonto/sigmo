import SwiftUI

struct InternetView: View {
    private enum Action {
        case connect(Bool)
        case options(InternetConnectionOptions, keyPath: WritableKeyPath<InternetConnectionOptions, Bool>)
    }
    private struct PublicLookup: Equatable {
        let connectionID: String
        let retry: Int
    }
    @State private var state: InternetState
    @State private var traffic = InternetTrafficState()
    @State private var action: ConnectionAction<Action>?
    @State private var publicRetry = 0
    @Environment(\.scenePhase) private var scenePhase

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: InternetState(detail: detail, profileID: profileID))
    }

    private var trafficTarget: ModemSIMTarget? {
        guard scenePhase == .active, state.scope.radioAvailable, state.connected else { return nil }
        return state.scope.detail.simTarget
    }

    var body: some View {
        let target = trafficTarget
        let lookup = state.connection.flatMap(\.connectionID).map { PublicLookup(connectionID: $0, retry: publicRetry) }
        let action = action
        return Form {
            ConnectionRadioNotice(scope: state.scope)
            Section {
                InternetTrafficCard(traffic: traffic, target: target, accent: .accentColor, status: state.status)
                    .listRowInsets(EdgeInsets())
            }
            if state.connection != nil {
                connectionOptions
                configuration
            }
            if let connection = state.connection, connection.connected {
                connectionDetails(connection)
            }
        }
        .navigationTitle("connection.internet")
        .modifier(ConnectionLifecycle(scope: state.scope, titleDisplayMode: .large,
                                      reload: { await state.reload() }, cancelAction: { self.action = nil }))
        .task(id: target) {
            if let target { await traffic.observe(state.scope.detail, target: target) }
        }
        .task(id: lookup) {
            if let lookup, lookup.connectionID == state.connection?.connectionID { await state.loadPublic() }
        }
        .task(id: action?.id) {
            if let action { await perform(action) }
        }
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }

    @ViewBuilder private var connectionOptions: some View {
        if state.connection?.knownStatus == true {
            Section {
                Toggle(isOn: Binding(
                    get: { state.connected }, set: { action = .init(value: .connect($0)) }
                )) {
                    InlineProgressLabel(isWorking: changingConnection) { Text("connection.internetConnection") }
                }
                .disabled(!state.canChange || action != nil)
                .accessibilityIdentifier("internetConnected")
            } footer: {
                if state.canEditAPN { Text("connection.internetDraftHint") }
            }
        }
        optionSection("connection.defaultRoute", hint: "connection.defaultRouteHint",
                      keyPath: \.defaultRoute, identifier: "internetDefaultRoute")
        optionSection("connection.proxy", hint: "connection.proxyHint",
                      keyPath: \.proxyEnabled, identifier: "internetProxyEnabled")
        optionSection("connection.alwaysOn", hint: "connection.alwaysOnHint",
                      keyPath: \.alwaysOn, identifier: "internetAlwaysOn")
    }

    private var configuration: some View {
        let apn = state.draft.apn.isEmpty ? detailText("connection.automatic") : state.draft.apn
        return Section("connection.networkConfiguration") {
            NavigationLink {
                InternetAPNView(state: state)
            } label: {
                LabeledContent("connection.apn", value: apn)
            }
            .disabled(state.busy || action != nil)
            .accessibilityLabel(Text("connection.apnAccessibility"))
            .accessibilityValue(apn)
            .accessibilityIdentifier("internetAPN")
        }
    }

    private func optionSection(_ title: LocalizedStringKey, hint: LocalizedStringKey,
                               keyPath: WritableKeyPath<InternetConnectionOptions, Bool>, identifier: String) -> some View {
        Section {
            Toggle(isOn: Binding(
                get: { state.options[keyPath: keyPath] },
                set: { value in
                    var options = state.options
                    options[keyPath: keyPath] = value
                    action = .init(value: .options(options, keyPath: keyPath))
                }
            )) {
                InlineProgressLabel(isWorking: changingOption(keyPath)) { Text(title) }
            }
            .disabled(!state.canEditOptions || action != nil)
            .accessibilityIdentifier(identifier)
        } footer: {
            Text(hint)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var changingConnection: Bool {
        if case .connect = action?.value { return true }
        return false
    }

    private func changingOption(_ keyPath: WritableKeyPath<InternetConnectionOptions, Bool>) -> Bool {
        if case .options(_, let pendingKeyPath) = action?.value { return pendingKeyPath == keyPath }
        return false
    }

    @ViewBuilder private func connectionDetails(_ connection: DetailInternet) -> some View {
        Section("connection.details") {
            valueRow(title: "connection.interface", value: connection.interfaceName, copyable: true)
                .accessibilityIdentifier("internetInterface")
            ConnectionDuration(seconds: connection.durationSeconds, observedAt: state.observedAt) { value in
                DetailValue(title: "connection.duration", value: value)
            }
            valueRow(title: "connection.sent", value: ConnectionFormat.bytes(connection.txBytes))
            valueRow(title: "connection.received", value: ConnectionFormat.bytes(connection.rxBytes))
        }
        Section("connection.ipAddresses") {
            valueRow(title: "IPv4", value: connection.ipv4Addresses.joined(separator: "\n"), copyable: true)
            valueRow(title: "IPv6", value: connection.ipv6Addresses.joined(separator: "\n"), copyable: true)
            valueRow(title: "DNS", value: connection.dns.joined(separator: "\n"), copyable: true)
            valueRow(title: "connection.routeMetric", value: connection.routeMetric.map(String.init))
        }
        Section("connection.publicNetwork") {
            if state.publicLoading {
                ConnectionProgress(title: "connection.loading")
            } else if state.publicFailure != nil {
                Text("apiError.internet_public_failed").foregroundStyle(.secondary)
                Button("common.retry") { publicRetry += 1 }
            } else {
                valueRow(title: "connection.publicIP", value: state.publicInfo?.ip, copyable: true)
                    .accessibilityIdentifier("internetPublicIP")
                valueRow(title: "connection.country", value: state.publicInfo?.country)
                valueRow(title: "connection.organization", value: state.publicInfo?.organization)
            }
        }
        if connection.preferences.proxyEnabled {
            Section("connection.proxyDetails") {
                valueRow(title: "connection.status", value: ConnectionFormat.enabled(connection.proxy?.enabled))
                valueRow(title: "connection.username", value: connection.proxy?.username, copyable: true)
                ConnectionSecretValue(title: "connection.password", value: connection.proxy?.password)
                proxyRow(connection.proxy, socks: false)
                proxyRow(connection.proxy, socks: true)
            }
        }
    }

    private func valueRow(title: String, value: String?, copyable: Bool = false) -> some View {
        DetailValue(title: title, value: value, copyable: copyable)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(Text(detailText(title)))
            .accessibilityValue(modemValue(value))
    }

    private func proxyRow(_ proxy: InternetProxy?, socks: Bool) -> some View {
        let address = socks ? proxy?.socks5Address : proxy?.httpAddress
        let url = proxy?.url(socks: socks)

        return LabeledContent {
            HStack {
                Text(modemValue(address))
                    .fixedSize(horizontal: false, vertical: true)
                    .multilineTextAlignment(.trailing)
                    .textSelection(.enabled)
                    .contextMenu {
                        if let address, !address.isEmpty {
                            Button("detail.copy", systemImage: "document.on.document") { UIPasteboard.general.string = address }
                        }
                    }
                Button(socks ? "connection.copySOCKSURL" : "connection.copyHTTPURL", systemImage: "document.on.document") {
                    if let url { UIPasteboard.general.string = url }
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
                .disabled(url == nil)
                .accessibilityIdentifier(socks ? "copySOCKSProxy" : "copyHTTPProxy")
            }
        } label: { Text(socks ? "connection.socksProxy" : "connection.httpProxy") }
    }

    private func perform(_ action: ConnectionAction<Action>) async {
        guard self.action?.id == action.id, !Task.isCancelled else { return }
        switch action.value {
        case .connect(let enabled): await state.setConnected(enabled)
        case .options(let options, _): await state.updateOptions(options)
        }
        if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
    }
}

private struct InternetAPNView: View {
    @Bindable var state: InternetState

    var body: some View {
        Form {
            Section {
                LabeledContent {
                    TextField("connection.automatic", text: $state.draft.apn)
                        .multilineTextAlignment(.trailing)
                        .accessibilityIdentifier("internetAPNField")
                } label: { Text("connection.apn") }
                LabeledContent {
                    TextField("connection.optional", text: $state.draft.apnUsername)
                        .multilineTextAlignment(.trailing)
                        .accessibilityIdentifier("internetUsername")
                } label: { Text("connection.username") }
                LabeledContent {
                    SecureField("connection.optional", text: $state.draft.apnPassword)
                        .multilineTextAlignment(.trailing)
                        .accessibilityIdentifier("internetPassword")
                } label: { Text("connection.password") }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            Section {
                Picker("connection.ipType", selection: Binding(
                    get: { state.draft.ipType.isEmpty ? "ipv4v6" : state.draft.ipType },
                    set: { state.draft.ipType = $0 }
                )) {
                    Text("IPv4v6").tag("ipv4v6")
                    Text("IPv4").tag("ipv4")
                    Text("IPv6").tag("ipv6")
                    if !["", "ipv4v6", "ipv4", "ipv6"].contains(state.draft.ipType) {
                        Text(state.draft.ipType).tag(state.draft.ipType).disabled(true)
                    }
                }
                .accessibilityIdentifier("internetIPType")
                Picker("connection.authentication", selection: Binding(
                    get: { state.draft.apnAuth == "default" ? "" : state.draft.apnAuth },
                    set: { state.draft.apnAuth = $0 }
                )) {
                    Text("connection.automatic").tag("")
                    Text("connection.noAuthentication").tag("none")
                    Text("PAP").tag("pap")
                    Text("CHAP").tag("chap")
                    Text("PAP / CHAP").tag("pap|chap")
                    Text("MS-CHAP v2").tag("mschapv2")
                    if !["", "default", "none", "pap", "chap", "pap|chap", "mschapv2"].contains(state.draft.apnAuth) {
                        Text(state.draft.apnAuth).tag(state.draft.apnAuth).disabled(true)
                    }
                }
                .accessibilityIdentifier("internetAuthentication")
            } footer: {
                if state.connected { Text("connection.disconnectToEditAPN") }
                else if !state.draft.valid { Text("connection.invalidAPN") }
            }
        }
        .pickerStyle(.menu)
        .disabled(!state.canEditAPN)
        .navigationTitle("connection.apn")
        .scrollDismissesKeyboard(.interactively)
        .modifier(ConnectionLifecycle(scope: state.scope, reload: { await state.reload() }, cancelAction: {}))
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }
}

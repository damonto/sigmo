import SwiftUI

struct InternetView: View {
    @State private var state: InternetState

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: InternetState(detail: detail, profileID: profileID))
    }

    var body: some View { InternetPage(state: state, page: .connection) }
}

private enum InternetPageKind {
    case connection, apn

    var title: String {
        switch self {
        case .connection: "connection.internet"
        case .apn: "connection.apn"
        }
    }
}

private struct InternetPage: View {
    private enum Action { case connect(Bool), options(InternetConnectionOptions) }
    @Bindable var state: InternetState
    let page: InternetPageKind
    @State private var action: ConnectionAction<Action>?

    var body: some View {
        Form {
            if page == .connection { ConnectionRadioNotice(scope: state.scope) }
            switch page {
            case .connection: connectionSections
            case .apn: apnSections
            }
        }
        .navigationTitle(Text(detailText(page.title)))
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil }))
        .task(id: action?.id) { await perform() }
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }

    private var connectionSections: some View {
        Group {
            Section {
                if state.connection == nil {
                    ConnectionInitialValue(loading: state.loadFailure == nil)
                } else if state.connection?.knownStatus == true {
                    Toggle("connection.internet", isOn: Binding(
                        get: { state.connected }, set: { action = .init(value: .connect($0)) }
                    ))
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("internetConnected")
                    valueRow(title: "connection.status", value: detailText(state.statusKey))
                } else { ConnectionInitialValue(loading: false) }
                if state.updating { ConnectionProgress() }
            }
            Section {
                NavigationLink {
                    InternetPage(state: state, page: .apn)
                } label: {
                    LabeledContent("connection.apn", value: state.draft.apn.isEmpty ? detailText("connection.automatic") : state.draft.apn)
                }
                .accessibilityIdentifier("internetAPN")
                .accessibilityLabel(Text("connection.apn"))
                .accessibilityValue(state.draft.apn.isEmpty ? detailText("connection.automatic") : state.draft.apn)
            }
            .disabled(state.connection == nil || state.busy || action != nil)
            if state.connection != nil {
                optionsSections
                detailSections
            }
        }
    }

    private var apnSections: some View {
        Group {
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
            .disabled(!state.canEditAPN)
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
            }
            .disabled(!state.canEditAPN)
            if !state.draft.valid {
                Section { Text("connection.invalidAPN").foregroundStyle(.secondary) }
            }
        }
    }

    private var optionsSections: some View {
        Group {
            Section {
                option("connection.defaultRoute", keyPath: \.defaultRoute, identifier: "internetDefaultRoute")
            } footer: { Text("connection.defaultRouteHint").fixedSize(horizontal: false, vertical: true) }
            Section {
                option("connection.proxy", keyPath: \.proxyEnabled, identifier: "internetProxyEnabled")
            } footer: { Text("connection.proxyHint").fixedSize(horizontal: false, vertical: true) }
            Section {
                option("connection.alwaysOn", keyPath: \.alwaysOn, identifier: "internetAlwaysOn")
            } footer: { Text("connection.alwaysOnHint").fixedSize(horizontal: false, vertical: true) }
        }
    }

    private func option(_ title: String, keyPath: WritableKeyPath<InternetConnectionOptions, Bool>, identifier: String) -> some View {
        Toggle(isOn: Binding(
            get: { state.options[keyPath: keyPath] },
            set: { value in
                var options = state.options
                options[keyPath: keyPath] = value
                action = .init(value: .options(options))
            }
        )) { Text(detailText(title)).fixedSize(horizontal: false, vertical: true) }
        .disabled(!state.canEditOptions || action != nil)
        .accessibilityIdentifier(identifier)
    }

    @ViewBuilder private var detailSections: some View {
        if let connection = state.connection, connection.connected {
            Section("connection.details") {
                valueRow(title: "connection.interface", value: connection.interfaceName, copyable: true)
                    .accessibilityIdentifier("internetInterface")
                ConnectionDurationRow(seconds: connection.durationSeconds, observedAt: state.observedAt, running: connection.connected)
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
                if state.publicLoading { ConnectionProgress(title: "connection.loading") }
                valueRow(title: "connection.publicIP", value: state.publicInfo?.ip, copyable: true)
                valueRow(title: "connection.country", value: state.publicInfo?.country)
                valueRow(title: "connection.organization", value: state.publicInfo?.organization)
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

    private func perform() async {
        guard let action else { return }
        switch action.value {
        case .connect(let enabled): await state.setConnected(enabled)
        case .options(let options): await state.updateOptions(options)
        }
        if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
    }
}

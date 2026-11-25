import SwiftUI

struct WiFiProxyView: View {
    let connection: WiFiCallingState
    @State private var proxy: WiFiProxyState
    @State private var editor: WiFiProxyEditor?
    @State private var action: ConnectionAction<String?>?
    @State private var catalogReload = 0
    @State private var failure: BackendFailure?

    init(connection: WiFiCallingState) {
        self.connection = connection
        _proxy = State(initialValue: WiFiProxyState(scope: connection.scope))
    }

    var body: some View {
        let check = proxy.check
        let action = action
        Form {
            switch proxy.catalog {
            case .idle, .loading:
                Section { ConnectionProgress(title: "connection.loading") }
            case .unsupported:
                Section { Text("connection.wifiProxy.unsupported").foregroundStyle(.secondary) }
            case .failed(let failure):
                Section {
                    Button("common.retry") { catalogReload += 1 }
                } header: {
                    Text("connection.wifiProxy.listFailed")
                } footer: {
                    Text(failure.message)
                }
            case .loaded:
                if connection.settings != nil || !proxy.proxies.isEmpty { proxySelection }
                if !proxy.proxies.isEmpty { proxyActions }
                if let result = proxy.result { testResults(result) }
            }
        }
        .navigationTitle("connection.wifiProxy.title")
        .toolbar {
            if proxy.catalog == .loaded {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("connection.wifiProxy.add", systemImage: "plus") { editor = .init(proxy: nil) }
                        .labelStyle(.iconOnly)
                }
            }
        }
        .modifier(ConnectionLifecycle(scope: connection.scope, reload: {}, cancelAction: {
            self.action = nil
            editor = nil
            proxy.cancelPending()
        }))
        .foregroundTask(id: catalogReload) { await proxy.loadCatalog() }
        .task(id: check?.id) {
            guard let check, proxy.check?.id == check.id, !Task.isCancelled else { return }
            failure = nil
            do { try await proxy.runCheck(check) }
            catch { failure = .wrapping(error) }
        }
        .task(id: action?.id) {
            guard let action, self.action?.id == action.id, !Task.isCancelled else { return }
            await connection.selectProxy(action.value)
            if !Task.isCancelled {
                failure = connection.failure
                connection.failure = nil
            }
            if self.action?.id == action.id { self.action = nil }
        }
        .onChange(of: connection.settings?.underlay.proxyId, initial: true) { previous, current in
            if proxy.selectedID == (previous ?? "") || proxy.selectedID.isEmpty {
                proxy.select(current ?? "")
            }
        }
        .sheet(item: $editor) { entry in WiFiProxyForm(entry: entry, save: proxy.save) }
        .detailErrorAlert($failure)
    }

    private var proxySelection: some View {
        Section {
            let selected = proxy.selectedProxy
            if connection.settings != nil {
                Toggle("connection.wifiProxy.useProxy", isOn: Binding(
                    get: { connection.usesProxy },
                    set: { enabled in
                        if !enabled || selected != nil {
                            action = .init(value: enabled ? selected?.id : nil)
                        }
                    }
                ))
                .disabled(!connection.canChange || action != nil || (!connection.usesProxy && selected == nil))
                .accessibilityIdentifier("wifiProxyEnabled")
            }
            if !proxy.proxies.isEmpty {
                Picker("connection.wifiProxy.selection", selection: Binding(get: { proxy.selectedID }, set: proxy.select)) {
                    Text("connection.wifiProxy.selection").tag("")
                    ForEach(proxy.proxies) { entry in Text(entry.name).tag(entry.id) }
                    if !proxy.selectedID.isEmpty, selected == nil { Text("common.unknown").tag(proxy.selectedID) }
                }
                .pickerStyle(.menu)
                if let selected, connection.usesProxy, connection.settings?.underlay.proxyId != selected.id {
                    Button("connection.wifiProxy.switchProxy") { action = .init(value: selected.id) }
                        .disabled(!connection.canChange || action != nil)
                }
            }
        }
    }

    private var proxyActions: some View {
        Section {
            LoadingButton(isWorking: proxy.check != nil, progressValue: Text("connection.wifiProxy.checking"),
                          action: proxy.requestCheck) {
                Text("connection.wifiProxy.check")
            }
            .disabled(proxy.selectedProxy == nil)
            if let selected = proxy.selectedProxy {
                Button("connection.wifiProxy.edit") { editor = .init(proxy: selected) }
            }
        }
    }

    private func testResults(_ result: WiFiProxyCheck) -> some View {
        Section {
            resultRow("connection.wifiProxy.server", result.tcp.serverAddress ?? result.udp.serverAddress ?? "—")
            resultRow("connection.wifiProxy.tcp", result.tcp.ip ?? result.tcp.message)
            resultRow("connection.wifiProxy.relay", result.udp.relayAddress ?? "—")
            resultRow("connection.wifiProxy.udp", result.udp.ipAndPort)
            resultRow("connection.wifiProxy.dns", result.dns.addresses?.joined(separator: ", ") ?? result.dns.message)
        } header: {
            Text("connection.wifiProxy.results")
        } footer: {
            Text("connection.wifiProxy.scope")
        }
    }

    private func resultRow(_ title: LocalizedStringKey, _ value: String) -> some View {
        LabeledContent(title) {
            Text(value).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
    }
}

private struct WiFiProxyEditor: Identifiable {
    let id = UUID()
    let proxy: WiFiProxy?
}

private struct WiFiProxyForm: View {
    @Environment(\.dismiss) private var dismiss
    let entry: WiFiProxyEditor
    let save: @MainActor (String?, WiFiProxySettings) async throws -> Void
    @State private var name: String
    @State private var host: String
    @State private var port: String
    @State private var auth: Bool
    @State private var username: String
    @State private var password = ""
    @State private var invalidPaste = false
    @State private var submission: ConnectionAction<WiFiProxySettings>?
    @State private var failure: BackendFailure?

    private var saving: Bool { submission != nil }

    private var settings: WiFiProxySettings? {
        guard !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              !host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              let port = port.isEmpty ? WiFiProxyPaste.defaultPort : Int(port), (1...65535).contains(port),
              !auth || (!username.isEmpty && (!password.isEmpty || entry.proxy?.hasPassword == true)) else { return nil }
        return WiFiProxySettings(name: name, host: host, port: port,
                                 username: auth ? username : "", password: auth ? password : "")
    }

    private var hostInput: Binding<String> {
        Binding(get: { host }, set: { value in
            host = value
            invalidPaste = false
        })
    }

    init(entry: WiFiProxyEditor, save: @escaping @MainActor (String?, WiFiProxySettings) async throws -> Void) {
        self.entry = entry
        self.save = save
        _name = State(initialValue: entry.proxy?.name ?? "")
        _host = State(initialValue: entry.proxy?.host ?? "")
        _port = State(initialValue: entry.proxy.map { String($0.port) } ?? "")
        _auth = State(initialValue: entry.proxy?.username.isEmpty == false)
        _username = State(initialValue: entry.proxy?.username ?? "")
    }

    var body: some View {
        NavigationStack {
            Form {
                LabeledContent("connection.wifiProxy.name") {
                    TextField("connection.wifiProxy.name", text: $name, prompt: Text("connection.wifiProxy.namePlaceholder"))
                        .accessibilityIdentifier("wifiProxyName")
                }
                LabeledContent("connection.wifiProxy.host") {
                    if entry.proxy == nil {
                        WiFiProxyHostField(text: hostInput, onPaste: applyPaste)
                    } else {
                        TextField("connection.wifiProxy.host", text: $host, prompt: Text("connection.wifiProxy.hostPlaceholder"))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .accessibilityIdentifier("wifiProxyHost")
                    }
                }
                if invalidPaste {
                    Text("connection.wifiProxy.invalidPaste")
                        .font(.footnote)
                        .foregroundStyle(.red)
                        .multilineTextAlignment(.leading)
                }
                LabeledContent("connection.wifiProxy.port") {
                    TextField("connection.wifiProxy.port", text: $port, prompt: Text("connection.wifiProxy.portPlaceholder"))
                        .keyboardType(.numberPad)
                        .accessibilityIdentifier("wifiProxyPort")
                }
                Toggle("connection.wifiProxy.auth", isOn: $auth)
                if auth {
                    LabeledContent("connection.wifiProxy.username") {
                        TextField("connection.wifiProxy.username", text: $username,
                                  prompt: Text("connection.wifiProxy.usernamePlaceholder"))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .accessibilityIdentifier("wifiProxyUsername")
                    }
                    LabeledContent("connection.wifiProxy.password") {
                        SecureField("connection.wifiProxy.password", text: $password,
                                    prompt: Text(entry.proxy?.hasPassword == true ? "connection.wifiProxy.passwordSaved"
                                                 : "connection.wifiProxy.passwordPlaceholder"))
                    }
                }
            }
            .multilineTextAlignment(.trailing)
            .disabled(saving)
            .navigationTitle(entry.proxy == nil ? "connection.wifiProxy.add" : "connection.wifiProxy.edit")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("connection.wifiProxy.cancel") { dismiss() }.disabled(saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.save") {
                        if !saving, let settings { submission = .init(value: settings) }
                    }
                    .disabled(saving || settings == nil)
                }
            }
            .detailErrorAlert($failure)
        }
        .interactiveDismissDisabled(saving)
        .task(id: submission?.id) { [submission] in
            if let submission { await submit(submission) }
        }
    }

    private func applyPaste(_ paste: WiFiProxyPaste) {
        invalidPaste = paste == .invalid
        guard case .address(let address) = paste else { return }

        if name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            name = String(address.host.prefix(128))
        }
        port = String(address.port)
        auth = !address.username.isEmpty
        username = address.username
        password = address.password
    }

    private func submit(_ submission: ConnectionAction<WiFiProxySettings>) async {
        guard self.submission?.id == submission.id, !Task.isCancelled else { return }
        defer {
            if self.submission?.id == submission.id { self.submission = nil }
        }
        do {
            try await save(entry.proxy?.id, submission.value)
            if !Task.isCancelled { dismiss() }
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
        }
    }
}

import SwiftUI

struct WiFiUplinkView: View {
    private enum Action: Equatable {
        case directUplink(WiFiUnderlay)
        case proxy(String?)
    }

    @Bindable var state: WiFiCallingState
    @State private var action: ConnectionAction<Action>?
    @State private var proxies: [WiFiProxy] = []
    @State private var loadingProxies = true
    @State private var selected = ""
    @State private var editor: WiFiProxyEditor?
    @State private var checking = false
    @State private var supported = true
    @State private var checkTask: Task<Void, Never>?
    @State private var result: WiFiProxyCheck?

    private var selectedProxy: WiFiProxy? { proxies.first { $0.id == selected } }

    var body: some View {
        Form {
            Section {
                ForEach(state.uplinks) { uplink in
                    Button { action = .init(value: .directUplink(uplink.underlay)) } label: {
                        HStack(spacing: 12) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(uplink.name).foregroundStyle(.primary)
                                if !uplink.available { Text("connection.uplinkUnavailable").font(.caption).foregroundStyle(.secondary) }
                            }
                            Spacer(minLength: 8)
                            if action?.value == .directUplink(uplink.underlay) {
                                ProgressView().controlSize(.small).accessibilityLabel(Text("common.working"))
                            } else if state.isDirectUplink(uplink.underlay) {
                                Image(systemName: "checkmark").foregroundStyle(.tint)
                            }
                        }
                    }
                    .disabled(!state.canChange || action != nil || state.usesProxy || !uplink.available)
                    .accessibilityIdentifier("wifiUplink-" + uplink.underlay.mode + (uplink.underlay.modemId.map { "-" + $0 } ?? ""))
                    .accessibilityAddTraits(state.isDirectUplink(uplink.underlay) ? .isSelected : [])
                }
            } footer: { Text("connection.uplinkHint") }
            if supported { proxySection }
        }
        .navigationTitle("connection.uplink")
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil; clearResult() },
                                      observe: { await state.observePending() }))
        .task(id: action?.id) {
            guard let action else { return }
            switch action.value {
            case .directUplink(let underlay): await state.selectUplink(underlay)
            case .proxy(let id): await state.selectProxy(id)
            }
            if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
        }
        .task {
            loadingProxies = true
            defer { loadingProxies = false }
            do {
                proxies = try await state.scope.read { api, token in try await api.wifiCallingProxies(token: token) }
                if selected.isEmpty { selected = state.settings?.underlay.proxyId ?? "" }
            } catch let error as BackendFailure where error.status == 404 {
                supported = false
            } catch {
                if !Task.isCancelled { state.failure = .wrapping(error) }
            }
        }
        .onChange(of: selected) { _, _ in clearResult() }
        .onChange(of: state.settings?.underlay.proxyId) { previous, next in
            if selected == (previous ?? "") { selected = next ?? "" }
        }
        .sheet(item: $editor) { entry in WiFiProxyForm(entry: entry, save: saveProxy) }
        .detailErrorAlert($state.failure)
    }

    private var proxySection: some View {
        Section {
            Toggle("connection.wifiProxy.useProxy", isOn: Binding(
                get: { state.usesProxy },
                set: { enabled in
                    if enabled {
                        if let selectedProxy { action = .init(value: .proxy(selectedProxy.id)) }
                    } else {
                        action = .init(value: .proxy(nil))
                    }
                }
            ))
            .disabled(!state.canChange || action != nil || (!state.usesProxy && selectedProxy == nil))
            .accessibilityIdentifier("wifiProxyEnabled")
            if !proxies.isEmpty {
                Picker("connection.wifiProxy.selection", selection: $selected) {
                    Text("connection.wifiProxy.selection").tag("")
                    ForEach(proxies) { proxy in Text(proxy.name).tag(proxy.id) }
                }
                Button("connection.wifiProxy.check") { testConnection() }
                    .disabled(selectedProxy == nil || checking)
                if checking {
                    ProgressView("connection.wifiProxy.checking")
                }
                if let selectedProxy {
                    Button("connection.wifiProxy.edit") { editor = WiFiProxyEditor(proxy: selectedProxy) }
                    if state.usesProxy, state.settings?.underlay.proxyId != selectedProxy.id {
                        Button("connection.wifiProxy.switchProxy") {
                            action = .init(value: .proxy(selectedProxy.id))
                        }
                        .disabled(!state.canChange || action != nil)
                    }
                }
            }
            Button("connection.wifiProxy.add") { editor = WiFiProxyEditor() }
                .disabled(!state.scope.available || loadingProxies)
            if let result {
                resultRow("connection.wifiProxy.server", result.tcp.serverAddress ?? result.udp.serverAddress ?? "—")
                resultRow("connection.wifiProxy.tcp", result.tcp.ip ?? result.tcp.message)
                resultRow("connection.wifiProxy.relay", result.udp.relayAddress ?? "—")
                resultRow("connection.wifiProxy.udp", result.udp.ipAndPort)
                resultRow("connection.wifiProxy.dns", result.dns.addresses?.joined(separator: ", ") ?? result.dns.message)
            }
        } header: {
            Text("connection.wifiProxy.title")
        } footer: {
            if result != nil { Text("connection.wifiProxy.scope") }
        }
    }

    private func resultRow(_ title: LocalizedStringKey, _ value: String) -> some View {
        LabeledContent(title) {
            Text(value).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
    }

    private func saveProxy(_ id: String?, _ settings: WiFiProxySettings) async throws {
        let saved = try await state.scope.read { api, token in
            try await api.saveWiFiProxy(id, settings: settings, token: token)
        }
        proxies.removeAll { $0.id == saved.id }
        proxies.append(saved)
        clearResult()
        selected = saved.id
    }

    private func testConnection() {
        clearResult()
        state.failure = nil
        let id = selected
        let modem = state.scope.modemID
        checking = true
        checkTask = Task {
            defer { if !Task.isCancelled { checking = false; checkTask = nil } }
            do {
                let value = try await state.scope.read { api, token in
                    try await api.checkWiFiProxy(modem, proxyId: id, token: token)
                }
                guard selected == id, !Task.isCancelled else { return }
                result = value
            } catch {
                guard selected == id, !Task.isCancelled else { return }
                state.failure = .wrapping(error)
            }
        }
    }

    private func clearResult() {
        checkTask?.cancel()
        checkTask = nil
        checking = false
        result = nil
    }
}

private struct WiFiProxyEditor: Identifiable {
    let id = UUID()
    let proxy: WiFiProxy?

    init(proxy: WiFiProxy? = nil) { self.proxy = proxy }
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
    @State private var saving = false
    @State private var failure: BackendFailure?

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
                    TextField("connection.wifiProxy.host", text: $host, prompt: Text("connection.wifiProxy.hostPlaceholder"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityIdentifier("wifiProxyHost")
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
                    Button("connection.wifiProxy.save") { Task { await submit() } }
                        .disabled(saving || name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                                  || host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                                  || !port.isEmpty && !(1...65535).contains(Int(port) ?? 0)
                                  || (auth && (username.isEmpty || (password.isEmpty && entry.proxy?.hasPassword != true))))
                }
            }
            .detailErrorAlert($failure)
        }
        .interactiveDismissDisabled(saving)
    }

    private func submit() async {
        guard !saving, let port = port.isEmpty ? 1080 : Int(port), (1...65535).contains(port) else { return }
        saving = true
        defer { saving = false }
        do {
            try await save(entry.proxy?.id, WiFiProxySettings(name: name, host: host, port: port,
                            username: auth ? username : "", password: auth ? password : ""))
            dismiss()
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
        }
    }
}

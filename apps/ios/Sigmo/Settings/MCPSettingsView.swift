import SwiftUI
import UniformTypeIdentifiers

struct SkillDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.zip] }
    var data: Data
    init(data: Data) { self.data = data }
    init(configuration: ReadConfiguration) throws {
        guard let data = configuration.file.regularFileContents else { throw BackendFailure.local("api.invalidResponse") }
        self.data = data
    }
    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper { FileWrapper(regularFileWithContents: data) }
}

struct MCPSettingsView: View {
    @Bindable var state: MCPState
    @State private var creating = false
    @State private var pendingEnabled: Bool?

    var body: some View {
        SettingsAccessView(session: state.session) {
            Form {
                if let settings = state.settings {
                    Section {
                        Toggle("settings.mcp.enabled", isOn: Binding(get: { settings.enabled }, set: { value in
                            pendingEnabled = value
                        }))
                        .disabled(state.busy || pendingEnabled != nil).accessibilityIdentifier("mcp-enabled")
                        Text("settings.mcp.securityDescription")
                            .font(.footnote).foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Section("settings.mcp.endpoint") {
                        Text(verbatim: state.endpoint).font(.footnote.monospaced()).textSelection(.enabled)
                            .accessibilityLabel(Text("settings.mcp.endpoint")).accessibilityValue(state.endpoint)
                        SettingsCopyButton(title: "settings.native.copyEndpoint", value: state.endpoint)
                    }
                    Section("settings.mcp.clientConfig") {
                        Text(verbatim: state.clientConfiguration).font(.footnote.monospaced()).textSelection(.enabled)
                            .accessibilityLabel(Text("settings.mcp.clientConfig")).accessibilityValue(state.clientConfiguration)
                        SettingsCopyButton(title: "settings.mcp.copyConfig", value: state.clientConfiguration)
                        AsyncActionButton(title: "settings.mcp.downloadSkill", id: "download-skill") { await state.download() }.disabled(state.busy)

                    }
                    Section("settings.mcp.keysTitle") {
                        Button("settings.mcp.createKey", systemImage: "plus") { creating = true }
                            .disabled(state.busy).accessibilityIdentifier("create-mcp-key")
                        if state.keys.isEmpty { Text("settings.mcp.noKeys").foregroundStyle(.secondary) }
                        ForEach(state.keys) { key in
                            NavigationLink {
                                MCPKeyDetailView(state: state, keyID: key.id)
                            } label: {
                                VStack(alignment: .leading, spacing: 5) {
                                    Text(key.name)
                                    Text("\(key.tokenHint) · \(settingsText("settings.mcp.status.\(key.status)"))").font(.caption).foregroundStyle(.secondary)
                                }
                            }.accessibilityIdentifier("key-\(key.id)")
                        }
                    }
                    Section {
                        Text(String(format: settingsText("settings.native.auditRetention"), settings.auditRetentionDays)).foregroundStyle(.secondary)
                        if state.audit.isEmpty { Text("settings.mcp.noAudit").foregroundStyle(.secondary) }
                        ForEach(state.audit) { event in
                            VStack(alignment: .leading, spacing: 5) {
                                Text(verbatim: event.tool).font(.body.monospaced())
                                Text(verbatim: event.keyName)
                                Text(settingsText("settings.mcp.outcome.\(event.outcome)"))
                                Text(event.createdAt.formatted(date: .abbreviated, time: .shortened)).font(.caption).foregroundStyle(.secondary)
                                if let error = event.errorCode { Text(verbatim: error).font(.caption) }
                            }
                        }
                    } header: { Text("settings.mcp.auditTitle") }
                }
                if state.busy { ProgressView("settings.native.working") }
            }
            .detailErrorAlert(creating ? nil : state.failure)
            .refreshable { await state.load() }
            .foregroundTask { if state.settings == nil { await state.load() } }
        }
        .navigationTitle("settings.mcp.title").navigationBarTitleDisplayMode(.inline)
        .background {
            Color.clear
            .fileExporter(isPresented: $state.exporting, document: SkillDocument(data: state.skillData ?? Data()), contentTypes: [.zip], defaultFilename: "sigmo-control.zip") { result in
                state.finishExport(result)
            } onCancellation: {
                state.discardExport()
            }
        }
        .sheet(isPresented: $creating, onDismiss: { state.secret = nil }) { MCPCreateKeyView(state: state) }
        .onChange(of: state.session.accessRevision) { _, _ in
            creating = false
            state.clear()
        }
        .task(id: pendingEnabled) {
            guard let pendingEnabled, !Task.isCancelled else { return }
            await state.setEnabled(pendingEnabled)
            if !Task.isCancelled { self.pendingEnabled = nil }
        }
        .onDisappear { pendingEnabled = nil }
    }

}

private struct MCPKeyDetailView: View {
    @Bindable var state: MCPState
    let keyID: String
    @State private var revoking = false
    @State private var revocationRequested = false

    var body: some View {
        SettingsAccessView(session: state.session) {
            Form {
                if let key = state.keys.first(where: { $0.id == keyID }) {
                    keyDetails(key)
                    if key.status == "active" {
                        Button("settings.mcp.revoke", role: .destructive) { revoking = true }
                            .disabled(state.busy).accessibilityIdentifier("revoke-\(key.id)")
                    }
                }
            }
            .detailErrorAlert(state.failure)
        }
        .navigationTitle(state.keys.first { $0.id == keyID }?.name ?? settingsText("settings.mcp.keysTitle"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("settings.mcp.revokeTitle", isPresented: $revoking, titleVisibility: .visible) {
            Button("settings.mcp.revoke", role: .destructive) {
                revocationRequested = true
            }
            .accessibilityIdentifier("confirm-revoke-key")
        } message: { Text("settings.native.revokeDescription") }
        .task(id: revocationRequested) {
            guard revocationRequested, !Task.isCancelled else { return }
            await state.revoke(keyID)
            if !Task.isCancelled { revocationRequested = false }
        }
        .onDisappear { revocationRequested = false }
    }

    @ViewBuilder private func keyDetails(_ key: MCPAPIKey) -> some View {
        Section {
            LabeledContent("settings.mcp.keyName", value: key.name)
            LabeledContent("settings.native.status", value: settingsText("settings.mcp.status.\(key.status)"))
                .accessibilityIdentifier("mcp-key-status")
            Text(verbatim: key.tokenHint).font(.footnote.monospaced())
            LabeledContent("settings.native.createdAt", value: key.createdAt.formatted(date: .abbreviated, time: .shortened))
            LabeledContent("settings.native.expiresAt", value: key.expiresAt.formatted(date: .abbreviated, time: .shortened))
        }
        Section("settings.mcp.allModems") {
            if key.allModems { Text("settings.mcp.allModems") }
            else { ForEach(key.modemIds ?? [], id: \.self) { id in Text(state.modems.first { $0.id == id }?.name ?? id) } }
        }
        Section("settings.native.permissions") {
            ForEach(key.permissions ?? [], id: \.self) { Text(permissionName($0)) }
        }
    }
}

struct SettingsCopyButton: View {
    let title: String
    let value: String
    @State private var copied = false
    var body: some View {
        Button {
            UIPasteboard.general.string = value
            copied = true
        } label: {
            Label(copied ? "settings.native.copied" : LocalizedStringKey(title), systemImage: copied ? "checkmark" : "document.on.document")
        }
        .accessibilityIdentifier(title)
        .onChange(of: value) { _, _ in copied = false }
    }
}

private func permissionName(_ permission: String) -> String {
    let key = "settings.mcp.permissionLabels.\(permission.replacingOccurrences(of: ".", with: "_"))"
    let label = settingsText(key)
    return label == key ? permission : label
}

private struct MCPCreateKeyView: View {
    @Bindable var state: MCPState
    @Environment(\.dismiss) private var dismiss
    @State private var request = MCPKeyRequest()
    @State private var days = "30"

    private var permissions: [MCPPermission] { state.settings?.permissions ?? [] }
    private var modules: [String] { Array(Set(permissions.map(\.module))).sorted() }
    private var payload: MCPKeyRequest {
        var payload = request
        payload.validityDays = Int(days) ?? 0
        return payload
    }

    var body: some View {
        NavigationStack {
            Form {
                if let created = state.secret {
                    Section {
                        Text("settings.native.secretOnce").foregroundStyle(.secondary)
                        Text(verbatim: created.token).font(.footnote.monospaced()).textSelection(.enabled).privacySensitive()
                            .accessibilityIdentifier("mcp-secret")
                        SettingsCopyButton(title: "settings.native.copySecret", value: created.token)
                    }
                    Section("settings.mcp.clientConfig") {
                        Text(verbatim: created.clientConfiguration).font(.footnote.monospaced()).textSelection(.enabled).privacySensitive()
                        SettingsCopyButton(title: "settings.mcp.copyConfig", value: created.clientConfiguration)
                    }
                } else {
                    Section {
                        TextField("settings.mcp.keyName", text: $request.name).accessibilityIdentifier("mcp-key-name")
                        TextField("settings.mcp.validityDays", text: $days).keyboardType(.numberPad).accessibilityIdentifier("mcp-key-days")
                        Toggle("settings.mcp.allModems", isOn: $request.allModems).accessibilityIdentifier("mcp-all-modems")
                        if !request.allModems {
                            ForEach(state.modems) { modem in
                                Toggle(modem.name, isOn: selection(modem.id, in: $request.modemIds))
                            }
                            if state.modems.isEmpty { Text("devices.empty").foregroundStyle(.secondary) }
                        }
                    }
                    Section("settings.native.permissions") {
                        Button(request.permissions.count == permissions.count ? "settings.native.deselectAll" : "settings.native.selectAll") {
                            request.permissions = request.permissions.count == permissions.count ? [] : permissions.map(\.name)
                        }.accessibilityIdentifier("mcp-all-permissions")
                    }
                    ForEach(modules, id: \.self) { module in
                        Section(settingsText("settings.mcp.modules.\(module)")) {
                            ForEach(permissions.filter { $0.module == module }) { permission in
                                Toggle(permissionName(permission.name), isOn: selection(permission.name, in: $request.permissions))
                                    .accessibilityIdentifier("permission-\(permission.name)")
                            }
                        }
                    }
                    Section {
                        AsyncActionButton(title: "settings.mcp.createKey", id: "submit-mcp-key") {
                            await state.create(payload)
                        }.disabled(!payload.isValid || state.busy)
                    }
                }
            }
            .disabled(state.busy)
            .detailErrorAlert(state.failure)
            .textInputAutocapitalization(.never).autocorrectionDisabled()
            .navigationTitle(state.secret == nil ? "settings.mcp.createKey" : "settings.mcp.secretTitle")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.done", action: close).disabled(state.busy)
                }
            }
        }
        .interactiveDismissDisabled(state.busy)
        .onDisappear { state.secret = nil }
        .onChange(of: state.session.canAccessDevices) { _, accessible in
            if !accessible { close() }
        }
    }

    private func close() {
        state.secret = nil
        dismiss()
    }

    private func selection(_ value: String, in values: Binding<[String]>) -> Binding<Bool> {
        Binding(get: { values.wrappedValue.contains(value) }, set: { enabled in
            values.wrappedValue.removeAll { $0 == value }
            if enabled { values.wrappedValue.append(value) }
        })
    }
}

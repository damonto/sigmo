import SwiftUI

struct SettingsFeedback: View {
    var message: String?
    var body: some View {
        if let message { Section { Label(LocalizedStringKey(message), systemImage: "checkmark.circle").foregroundStyle(.secondary) } }
    }
}

struct SettingsAccessView<Content: View>: View {
    let session: BackendSession
    @ViewBuilder var content: () -> Content
    @State private var login = false
    @State private var activation = false

    var body: some View {
        Group {
            if session.canAccessDevices { content() }
            else {
                Form {
                    Section {
                        Text(session.configuration.name).font(.headline)
                        if session.isRefreshing { ProgressView() }
                        else if session.license == .activation {
                            Button("activation.start") { activation = true }
                        } else if session.authentication == .required {
                            if session.supportsBrowserLogin {
                                BrowserLoginButton(session: session)
                            } else {
                                Button("auth.login") { login = true }
                                    .disabled(session.isLoggingOut)
                            }
                        }
                    }
                }
                .detailErrorAlert(login || activation ? nil : session.authFailure ?? session.failure)
            }
        }
        .sheet(isPresented: $login) { AuthenticationView(session: session) }
        .sheet(isPresented: $activation) { ActivationView(session: session) }
        .foregroundTask {
            if !session.canAccessDevices { await session.refresh(force: true) }
        }
    }
}

struct SettingsFieldRow: View {
    let field: SettingsField
    @Binding var value: SettingValue
    var channels: [NotificationSchema] = []
    var stacked = false

    private var text: Binding<String> {
        Binding(get: { value.text }, set: { value = .text($0) })
    }
    private var toggle: Binding<Bool> {
        Binding(get: { value.bool }, set: { value = .toggle($0) })
    }

    private var selection: Binding<String> {
        Binding(get: { value.text.isEmpty ? field.options?.first?.value ?? "" : value.text }, set: { value = .text($0) })
    }

    private var selectionLabel: String {
        field.options?.first(where: { $0.value == selection.wrappedValue })?.label ?? selection.wrappedValue
    }

    private var picker: some View {
        Picker(settingsText(field.label), selection: selection) {
            ForEach(field.options ?? []) { Text(settingsText($0.label)).tag($0.value) }
        }
    }

    private var inputLabel: some View {
        Text(settingsText(field.label))
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityIdentifier("label-\(field.key)")
    }

    @ViewBuilder private var input: some View {
        Group {
            if field.secret == true || field.control == .password {
                SecureField("", text: text)
            } else {
                TextField("", text: text, prompt: field.placeholder.map { Text(settingsText($0)) })
                    .keyboardType(field.control == .number ? .numbersAndPunctuation : .default)
            }
        }
        .multilineTextAlignment(stacked ? .leading : .trailing)
        .textInputAutocapitalization(.never).autocorrectionDisabled()
        .accessibilityLabel(settingsText(field.label))
        .accessibilityIdentifier(field.key)
    }

    var body: some View {
        Section {
            switch field.control {
            case .toggle:
                Toggle(settingsText(field.label), isOn: toggle).accessibilityIdentifier(field.key)
            case .select:
                Menu { picker } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        Text(settingsText(field.label))
                            .foregroundStyle(.primary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            Text(settingsText(selectionLabel))
                                .multilineTextAlignment(.trailing)
                            Image(systemName: "chevron.up.chevron.down")
                                .font(.caption)
                                .accessibilityHidden(true)
                        }
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .trailing)
                    }
                    .fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityLabel(settingsText(field.label))
                .accessibilityValue(settingsText(selectionLabel))
                .accessibilityIdentifier(field.key)
            case .channelList:
                Text(settingsText(field.label)).font(.headline)
                if channels.isEmpty { Text("settings.noConfiguredChannels").foregroundStyle(.secondary) }
                ForEach(channels) { channel in
                    Toggle(settingsText(channel.label), isOn: Binding(get: { value.strings.contains(channel.key) }, set: { enabled in
                        var names = value.strings.filter { $0 != channel.key }
                        if enabled { names.append(channel.key) }
                        value = .list(names.sorted())
                    })).accessibilityIdentifier("provider-\(channel.key)")
                }
            case .list:
                SettingsRecipientsField(field: field, value: $value)
            case .keyValue:
                SettingsHeadersField(title: settingsText(field.label), value: $value)
            case .text, .password, .number:
                if stacked {
                    VStack(alignment: .leading, spacing: 8) {
                        inputLabel
                        input
                    }
                } else {
                    LabeledContent { input } label: { inputLabel }
                }
            default:
                LabeledContent(settingsText(field.label), value: value.text)
                Text("settings.native.unsupportedField").foregroundStyle(.secondary)
            }
        } footer: {
            if let description = field.description { Text(settingsText(description)) }
        }
    }
}

private struct RecipientDraft: Identifiable, Equatable {
    let id = UUID()
    var text: String
}

private struct SettingsRecipientsField: View {
    let field: SettingsField
    @Binding var value: SettingValue
    @State private var recipients: [RecipientDraft]

    init(field: SettingsField, value: Binding<SettingValue>) {
        self.field = field
        _value = value
        _recipients = State(initialValue: value.wrappedValue.strings.map { RecipientDraft(text: $0) })
    }

    var body: some View {
        Text(settingsText(field.label)).font(.headline)
        ForEach($recipients) { $recipient in
            HStack {
                TextField(settingsText(field.label), text: $recipient.text)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("\(field.key)-\(recipient.id)")
                Button(role: .destructive) {
                    let id = recipient.id
                    recipients.removeAll { $0.id == id }
                } label: {
                    Image(systemName: "minus.circle")
                }
                .buttonStyle(.borderless)
                .accessibilityLabel(Text("common.delete"))
                .accessibilityIdentifier("delete-\(field.key)-\(recipient.id)")
            }
        }
        .onChange(of: recipients) { _, rows in value = .list(rows.map(\.text)) }
        .onChange(of: value) { _, value in
            // A successful save may normalize recipients. Ordinary keystrokes
            // retain row identity, including duplicate values and empty drafts.
            if recipients.map(\.text) != value.strings {
                recipients = value.strings.map { RecipientDraft(text: $0) }
            }
        }
        Button("common.add", systemImage: "plus") { recipients.append(RecipientDraft(text: "")) }
            .accessibilityIdentifier("add-\(field.key)")
    }
}

private struct HeaderDraft: Identifiable {
    let id = UUID()
    var original: String?
    var key = ""
    var value = ""
}

private struct SettingsHeadersField: View {
    let title: String
    @Binding var value: SettingValue
    @State private var draft: HeaderDraft?

    var body: some View {
        Text(title).font(.headline)
        ForEach(value.dictionary.keys.sorted(), id: \.self) { key in
            Button {
                draft = HeaderDraft(original: key, key: key, value: value.dictionary[key] ?? "")
            } label: { LabeledContent(key, value: value.dictionary[key] ?? "") }
            .foregroundStyle(.primary)
        }
        Button("common.add", systemImage: "plus") { draft = HeaderDraft() }
            .accessibilityIdentifier("add-header")
            .sheet(item: $draft) { item in
                HeaderEditor(draft: item, existing: Set(value.dictionary.keys)) { saved in
                    var headers = value.dictionary
                    if let original = item.original { headers[original] = nil }
                    if let saved { headers[saved.key] = saved.value }
                    value = .pairs(headers)
                }
            }
    }
}

private struct HeaderEditor: View {
    @Environment(\.dismiss) private var dismiss
    @State var draft: HeaderDraft
    let existing: Set<String>
    let save: (HeaderDraft?) -> Void
    private var trimmedKey: String { draft.key.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var valid: Bool { !trimmedKey.isEmpty && (trimmedKey == draft.original || !existing.contains(trimmedKey)) }

    var body: some View {
        NavigationStack {
            Form {
                TextField("settings.native.headerName", text: $draft.key).accessibilityIdentifier("header-name")
                TextField("settings.native.headerValue", text: $draft.value).accessibilityIdentifier("header-value")
                if !valid { Text("settings.native.invalidHeader").foregroundStyle(.secondary) }
                if draft.original != nil {
                    Button("common.delete", role: .destructive) { save(nil); dismiss() }
                }
            }
            .textInputAutocapitalization(.never).autocorrectionDisabled()
            .navigationTitle("settings.native.header")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.save") { draft.key = trimmedKey; save(draft); dismiss() }
                        .disabled(!valid).accessibilityIdentifier("save-header")
                }
            }
        }
    }
}

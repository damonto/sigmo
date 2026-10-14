import Contacts
import ContactsUI
import PhotosUI
import SwiftUI

struct ContactAvatarView: View {
    let session: BackendSession
    let contact: SigmoContact?
    var fallback = ""
    var size: CGFloat = 48
    @State private var image: UIImage?
    var body: some View {
        Group {
            if let image { Image(uiImage: image).resizable().scaledToFill() }
            else {
                ZStack {
                    LinearGradient(colors: [Color(red: 0.45, green: 0.44, blue: 0.53), Color(red: 0.24, green: 0.21, blue: 0.36)], startPoint: .top, endPoint: .bottom)
                    if let name = contact?.name, !name.isEmpty {
                        Text(name.split(whereSeparator: \.isWhitespace).prefix(2).compactMap(\.first).map(String.init).joined().uppercased())
                            .font(.system(size: size * 0.38, weight: .medium)).foregroundStyle(.white)
                    } else {
                        Image(systemName: "person.fill").resizable().scaledToFit().foregroundStyle(.white)
                            .padding(size * 0.17)
                    }
                }
            }
        }
        .frame(width: size, height: size).clipShape(Circle()).accessibilityHidden(true)
        .task(id: "\(contact?.id ?? ""):\(contact?.updatedAt ?? ""):\(session.accessRevision)") {
            image = nil
            guard let contact, contact.hasAvatar else { return }
            do {
                let data = try await session.protectedRequest { api, token in try await api.contactAvatar(contact.id, token: token) }
                guard !Task.isCancelled else { return }
                image = UIImage(data: data)
            } catch { /* A missing avatar keeps the contact's initials visible. */ }
        }
    }
}

extension Color {
    static func contactTint(_ hex: String) -> Color {
        guard hex.count == 7, hex.hasPrefix("#"), let value = UInt32(hex.dropFirst(), radix: 16) else { return Color(uiColor: .systemGroupedBackground) }
        return Color(red: Double((value >> 16) & 255) / 255, green: Double((value >> 8) & 255) / 255, blue: Double(value & 255) / 255)
    }
}

struct ContactDraft: Identifiable {
    let id = UUID()
    var contact: SigmoContact
    var avatar: Data?
}

struct ContactQuickActions: View {
    let numbers: [ContactPhone]
    var action: (String, Bool) -> Void
    @State private var choosingNumber = false
    @State private var calling = false

    var body: some View {
        ContactActionButtons(canMessage: !numbers.isEmpty, canCall: !numbers.isEmpty,
                             message: { choose(calling: false) }, call: { choose(calling: true) })
        .confirmationDialog(calling ? LocalizedStringKey("communication.dial") : LocalizedStringKey("communication.message"),
                            isPresented: $choosingNumber, titleVisibility: .visible) {
            ForEach(Array(numbers.enumerated()), id: \.offset) { _, phone in
                Button(phone.label.isEmpty ? phone.number : "\(phone.number) · \(phone.label)") {
                    action(phone.number, calling)
                }
                .tint(.accentColor)
            }
            Button("common.cancel", role: .cancel) { }.tint(.accentColor)
        }
    }

    private func choose(calling: Bool) {
        if numbers.count > 1 {
            self.calling = calling
            choosingNumber = true
        } else if let phone = numbers.first {
            action(phone.number, calling)
        }
    }
}

struct ContactActionButtons: View {
    var canMessage = true
    var canCall = true
    var messageIdentifier = "contact-message"
    var callIdentifier = "contact-call"
    let message: () -> Void
    let call: () -> Void
    @ScaledMetric(relativeTo: .title3) private var symbolSize: CGFloat = 20

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 16) {
                shortcut("communication.message", symbol: "message.fill", identifier: messageIdentifier, action: message)
                    .disabled(!canMessage)
                shortcut("communication.dial", symbol: "phone.fill", identifier: callIdentifier, action: call)
                    .disabled(!canCall)
            }
            .buttonStyle(.glass).buttonBorderShape(.circle)
        }
    }

    private func shortcut(_ title: LocalizedStringKey, symbol: String, identifier: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: min(symbolSize, 28))).frame(width: 44, height: 44)
        }
        .accessibilityLabel(title)
        .accessibilityIdentifier(identifier)
    }
}

struct ContactBackdrop: View {
    let color: String
    let hasPhoto: Bool
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        if hasPhoto, color.count == 7 {
            // Keep the photo's hue across the whole page while preserving white text contrast.
            let original = UIColor(Color.contactTint(color))
            let tint = backgroundColor(original)
            LinearGradient(colors: [tint, tint.opacity(0.82)], startPoint: .top, endPoint: .bottom)
                .background(.black).ignoresSafeArea()
        } else {
            Color(uiColor: .systemGroupedBackground).ignoresSafeArea()
        }
    }

    private func backgroundColor(_ color: UIColor) -> Color {
        var hue: CGFloat = 0, saturation: CGFloat = 0, brightness: CGFloat = 0, alpha: CGFloat = 0
        color.getHue(&hue, saturation: &saturation, brightness: &brightness, alpha: &alpha)
        return Color(UIColor(hue: hue, saturation: saturation, brightness: min(brightness, scheme == .dark ? 0.38 : 0.48), alpha: 1))
    }
}

struct SigmoContactDetail: View {
    let session: BackendSession
    let contactID: String
    var modemID: String?
    @Environment(\.dismiss) private var dismiss
    @State private var editor: ContactDraft?
    @State private var action: CommunicationAction?
    private var contact: SigmoContact? { session.savedContacts.first { $0.id == contactID } }

    var body: some View {
        Group {
            if let contact {
                let photoTheme = contact.hasAvatar && !contact.avatarColor.isEmpty
                List {
                    Section {
                        VStack(spacing: 16) {
                            ContactAvatarView(session: session, contact: contact, size: 208)
                            Text(contact.displayName).font(.title2.weight(.bold))
                                .multilineTextAlignment(.center).textSelection(.enabled)
                            ContactQuickActions(numbers: contact.numbers) { number, calling in
                                action = CommunicationAction(number: number, calling: calling, modemID: modemID)
                            }
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.top, 24).padding(.bottom, 8)
                    }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 0, leading: 0, bottom: 0, trailing: 0))
                    .listRowSeparator(.hidden)

                    if !contact.numbers.isEmpty {
                        Section {
                            ForEach(Array(contact.numbers.enumerated()), id: \.offset) { _, phone in
                                HStack {
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(phone.label.isEmpty ? detailText("communication.number") : phone.label)
                                            .font(.caption).foregroundStyle(photoTheme ? .white.opacity(0.8) : .secondary)
                                        Text(phone.number).textSelection(.enabled)
                                    }
                                    Spacer(minLength: 12)
                                    Button { action = CommunicationAction(number: phone.number, calling: true, modemID: modemID) } label: {
                                        Image(systemName: "phone.fill").font(.system(size: 20)).frame(minWidth: 44, minHeight: 44)
                                    }
                                    .buttonStyle(.borderless).accessibilityLabel("communication.dial")
                                    .accessibilityValue(phone.number)
                                }
                                .padding(.vertical, 2)
                            }
                        }
                        .listRowBackground(photoTheme ? Color.white.opacity(0.1) : Color(uiColor: .secondarySystemGroupedBackground))
                    }
                    if !contact.notes.isEmpty {
                        Section {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("communication.notes").font(.caption).foregroundStyle(photoTheme ? .white.opacity(0.8) : .secondary)
                                Text(contact.notes).textSelection(.enabled)
                            }.padding(.vertical, 6)
                        }
                        .listRowBackground(photoTheme ? Color.white.opacity(0.1) : Color(uiColor: .secondarySystemGroupedBackground))
                    }
                }
                .listStyle(.insetGrouped).listSectionSpacing(12)
                .contentMargins(.top, 0, for: .scrollContent)
                .contentMargins(.horizontal, 12, for: .scrollContent)
                .scrollContentBackground(.hidden)
                .background(ContactBackdrop(color: contact.avatarColor, hasPhoto: photoTheme))
                .foregroundStyle(photoTheme ? Color.white : Color.primary)
                .tint(photoTheme ? .white : .accentColor)
                .toolbarColorScheme(photoTheme ? .dark : nil, for: .navigationBar)
                .toolbar {
                    Button("common.edit") { editor = ContactDraft(contact: contact) }
                        .accessibilityLabel("communication.edit")
                }
            } else {
                ContentUnavailableView("communication.emptyContacts", systemImage: "person.crop.circle")
            }
        }
        .navigationTitle("").navigationBarTitleDisplayMode(.inline)
        .sheet(item: $editor) { draft in ContactEditView(session: session, draft: draft) }
        .communicationActions($action, session: session)
        .onChange(of: contact?.id) { _, id in if id == nil { dismiss() } }
    }
}

struct ContactEditView: View {
    let session: BackendSession
    @Environment(BackendStore.self) private var store
    @Environment(\.dismiss) private var dismiss
    @State private var contact: SigmoContact
    @State private var avatar: UIImage?
    @State private var photo: PhotosPickerItem?
    @State private var zoom = 1.0
    @State private var horizontal = 0.5
    @State private var vertical = 0.5
    @State private var cropping = false
    @State private var removingAvatar = false
    @State private var saving = false
    @State private var deleting = false
    @State private var failure: BackendFailure?
    init(session: BackendSession, draft: ContactDraft) {
        self.session = session
        _contact = State(initialValue: draft.contact)
        _avatar = State(initialValue: draft.avatar.flatMap(UIImage.init(data:)))
    }
    var body: some View {
        let hasAvatar = avatar != nil || contact.hasAvatar && !removingAvatar
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 14) {
                        if let preview = croppedAvatar() {
                            Image(uiImage: preview).resizable().scaledToFit()
                                .frame(width: 168, height: 168).clipShape(Circle())
                        } else {
                            ContactAvatarView(session: session, contact: removingAvatar ? nil : contact, size: 168)
                        }
                        HStack(spacing: 12) {
                            PhotosPicker(selection: $photo, matching: .images) {
                                Text(hasAvatar ? LocalizedStringKey("communication.changePhoto") : LocalizedStringKey("communication.addPhoto"))
                                    .font(.subheadline.weight(.medium)).padding(.horizontal, 8)
                            }
                            .buttonStyle(.glass).buttonBorderShape(.capsule)
                            if hasAvatar {
                                Menu {
                                    if avatar != nil { Button("communication.cropPhoto", systemImage: "crop") { cropping = true } }
                                    Button("communication.removeAvatar", systemImage: "trash", role: .destructive) { avatar = nil; photo = nil; removingAvatar = true }
                                } label: { Label("communication.photoOptions", systemImage: "ellipsis") }
                                .labelStyle(.iconOnly).buttonStyle(.glass).buttonBorderShape(.circle)
                            }
                        }
                    }
                    .frame(maxWidth: .infinity).padding(.bottom, 12)
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 0, trailing: 0))

                if store.settings.backends.count > 1 {
                    Section {
                        LabeledContent("communication.backend", value: session.configuration.name)
                    }
                }
                Section {
                    TextField("communication.name", text: $contact.name).textContentType(.name)
                }
                Section {
                    ForEach(contact.numbers.indices, id: \.self) { index in
                        VStack(alignment: .leading, spacing: 8) {
                            HStack(spacing: 12) {
                                TextField("communication.label", text: $contact.numbers[index].label)
                                    .foregroundStyle(.tint).frame(maxWidth: 92)
                                Divider()
                                TextField("communication.number", text: $contact.numbers[index].number)
                                    .keyboardType(.phonePad).textContentType(.telephoneNumber)
                            }
                        }
                        .padding(.vertical, 4)
                    }.onDelete { contact.numbers.remove(atOffsets: $0) }
                    Button { contact.numbers.append(ContactPhone()) } label: {
                        Label {
                            Text("communication.addNumber").foregroundStyle(.primary)
                        } icon: {
                            Image(systemName: "plus.circle.fill").foregroundStyle(.green)
                        }
                    }
                }
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("communication.notes").font(.subheadline)
                        TextEditor(text: $contact.notes).frame(minHeight: 72)
                    }
                }
                if !contact.id.isEmpty {
                    Section {
                        Button("communication.delete", role: .destructive) { deleting = true }
                            .frame(maxWidth: .infinity)
                    }
                }
            }
            .listSectionSpacing(12)
            .contentMargins(.top, 8, for: .scrollContent)
            .contentMargins(.horizontal, 16, for: .scrollContent)
            .navigationTitle(contact.id.isEmpty ? LocalizedStringKey("communication.newContact") : LocalizedStringKey("communication.edit"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.cancel", systemImage: "xmark") { dismiss() }.labelStyle(.iconOnly).disabled(saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("communication.save", systemImage: "checkmark") { Task { await save() } }
                        .labelStyle(.iconOnly).disabled(saving || !session.canAccessDevices || session.capabilities?.contains("contacts") != true || (contact.name.isEmpty && contact.numbers.isEmpty))
                }
            }
            .alert("communication.delete", isPresented: $deleting) {
                Button("communication.delete", role: .destructive) { Task { await remove() } }
                Button("common.cancel", role: .cancel) { }
            } message: { Text("communication.deleteContactDescription") }
            .disabled(saving)
            .interactiveDismissDisabled(saving)
            .detailErrorAlert($failure)
            .sheet(isPresented: $cropping) { cropEditor }
            .task(id: photo) {
                guard let photo else { return }
                do {
                    guard let data = try await photo.loadTransferable(type: Data.self), data.count <= 20 * 1024 * 1024, let image = UIImage(data: data) else { throw BackendFailure.local("communication.invalidPhoto") }
                    guard !Task.isCancelled else { return }
                    avatar = image; zoom = 1; horizontal = 0.5; vertical = 0.5; removingAvatar = false
                    cropping = true
                } catch { if !Task.isCancelled { failure = .wrapping(error) } }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }
    private func remove() async {
        guard !saving else { return }
        saving = true
        defer { saving = false }
        let id = contact.id
        do {
            try await session.protectedRequest { api, token in try await api.deleteContact(id, token: token) }
            try await session.refreshContacts(force: true)
            dismiss()
        } catch { failure = .wrapping(error) }
    }

    private var cropEditor: some View {
        NavigationStack {
            Form {
                Section {
                    if let preview = croppedAvatar() {
                        Image(uiImage: preview).resizable().scaledToFit()
                            .frame(maxWidth: .infinity).accessibilityLabel("communication.avatar")
                    }
                }
                .listRowInsets(EdgeInsets())
                Section {
                    VStack(alignment: .leading) {
                        Text("communication.zoom").font(.subheadline).foregroundStyle(.secondary)
                        Slider(value: $zoom, in: 1...4) { Text("communication.zoom") }
                    }
                    VStack(alignment: .leading) {
                        Text("communication.horizontal").font(.subheadline).foregroundStyle(.secondary)
                        Slider(value: $horizontal, in: 0...1) { Text("communication.horizontal") }
                    }
                    VStack(alignment: .leading) {
                        Text("communication.vertical").font(.subheadline).foregroundStyle(.secondary)
                        Slider(value: $vertical, in: 0...1) { Text("communication.vertical") }
                    }
                }
            }
            .navigationTitle("communication.cropPhoto")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.done") { cropping = false }
                }
            }
        }
    }
    private func croppedAvatar() -> UIImage? {
        guard let avatar, avatar.size.width > 0, avatar.size.height > 0 else { return nil }
        let size: CGFloat = 512
        let scale = size / min(avatar.size.width, avatar.size.height) * zoom
        let width = avatar.size.width * scale, height = avatar.size.height * scale
        let format = UIGraphicsImageRendererFormat(); format.scale = 1
        return UIGraphicsImageRenderer(size: CGSize(width: size, height: size), format: format).image { _ in
            avatar.draw(in: CGRect(x: -(width-size)*horizontal, y: -(height-size)*vertical, width: width, height: height))
        }
    }
    private func save() async {
        guard !saving else { return }
        saving = true
        defer { saving = false }
        do {
            let draft = contact
            contact = try await session.protectedRequest { api, token in try await api.saveContact(draft, token: token) }
            let id = contact.id
            if let data = croppedAvatar()?.pngData() {
                contact = try await session.protectedRequest { api, token in try await api.saveContactAvatar(id, data: data, token: token) }
            } else if removingAvatar {
                contact = try await session.protectedRequest { api, token in try await api.saveContactAvatar(id, data: nil, token: token) }
            }
            try await session.refreshContacts(force: true)
            dismiss()
        } catch { failure = .wrapping(error) }
    }
}

struct SystemContactImport: UIViewControllerRepresentable {
    var selected: (ContactDraft) -> Void
    @Environment(\.dismiss) private var dismiss
    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }
    func makeUIViewController(context: Context) -> CNContactPickerViewController {
        let controller = CNContactPickerViewController(); controller.delegate = context.coordinator; return controller
    }
    func updateUIViewController(_ controller: CNContactPickerViewController, context: Context) { context.coordinator.parent = self }
    static func dismantleUIViewController(_ controller: CNContactPickerViewController, coordinator: Coordinator) { controller.delegate = nil }
    @MainActor final class Coordinator: NSObject, @preconcurrency CNContactPickerDelegate {
        var parent: SystemContactImport
        init(parent: SystemContactImport) { self.parent = parent }
        func contactPicker(_ picker: CNContactPickerViewController, didSelect contact: CNContact) {
            let name = CNContactFormatter.string(from: contact, style: .fullName) ?? ""
            let numbers = contact.phoneNumbers.map { ContactPhone(label: $0.label.map(CNLabeledValue<NSString>.localizedString(forLabel:)) ?? "", number: $0.value.stringValue) }
            let image = contact.isKeyAvailable(CNContactImageDataKey) ? contact.imageData : nil
            parent.selected(ContactDraft(contact: SigmoContact(name: name, numbers: numbers), avatar: image))
            parent.dismiss()
        }
        func contactPickerDidCancel(_ picker: CNContactPickerViewController) { parent.dismiss() }
    }
}

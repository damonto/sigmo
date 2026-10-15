import SwiftUI

private struct BackendContactDraft: Identifiable {
    let session: BackendSession
    let draft: ContactDraft
    var id: UUID { draft.id }
}

private enum CommunicationCreation {
    case contact, importContact, message, call
}

struct CommunicationWorkspace: View {
    private struct SourceIdentity: Equatable {
        let accessRevision: Int
        let supported: Bool
        let accessible: Bool
    }

    private struct RefreshIdentity: Equatable {
        let search: String
        let missedOnly: Bool
        let sources: [SourceIdentity]
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let section: CommunicationSection
    let modemID: String?
    @State private var state: CommunicationOverviewState
    @State private var editor: BackendContactDraft?
    @State private var importSession: BackendSession?
    @State private var imported: BackendContactDraft?
    @State private var action: BackendCommunicationAction?
    @State private var creation: CommunicationCreation?
    @State private var deleting: CommunicationRecordID?
    @State private var selectedCall: CommunicationRecord<CallRecord>?
    @State private var selectedContact: CommunicationRecord<SigmoContact>?

    init(session: BackendSession, section: CommunicationSection, modemID: String? = nil) {
        self.init(sessions: [session], section: section, modemID: modemID)
    }

    init(sessions: [BackendSession], section: CommunicationSection, modemID: String? = nil) {
        self.section = section
        self.modemID = modemID
        _state = State(initialValue: CommunicationOverviewState(sessions: sessions, section: section))
    }

    var body: some View {
        Group {
            if state.sources.isEmpty {
                ContentUnavailableView("backends.empty.title", systemImage: "server.rack",
                                       description: Text("backends.empty.description"))
            } else if state.sources.count == 1, let source = state.sources.first, !source.session.canAccessDevices {
                SettingsAccessView(session: source.session) { EmptyView() }
            } else if state.sources.count == 1, let source = state.sources.first, !source.supported {
                ContentUnavailableView("communication.callsUnavailable", systemImage: section.icon)
            } else {
                list
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if section != .contacts {
                filters.padding(.horizontal, 16).padding(.bottom, 8)
            }
        }
        .navigationTitle(section.title)
        .navigationBarTitleDisplayMode(modemID == nil ? .large : .inline)
        .searchable(text: $state.search, placement: .navigationBarDrawer(displayMode: .always), prompt: dynamicTypeSize.isAccessibilitySize ? LocalizedStringKey("home.search") : section.searchPrompt)
        .searchToolbarBehavior(.minimize)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                composeButton.disabled(state.creationSessions.isEmpty)
            }
        }
        .navigationDestination(item: $selectedContact) { item in
            SigmoContactDetail(session: item.session, contactID: item.value.id, modemID: modemID)
        }
        .navigationDestination(item: $selectedCall) { item in
            CommunicationCallDetail(session: item.session, record: item.value,
                                    sourceLine: lines(for: item.session).first { $0.id == item.value.profileId })
        }
        .sheet(item: $editor) { destination in
            ContactEditView(session: destination.session, draft: destination.draft)
        }
        .sheet(item: $importSession, onDismiss: { editor = imported; imported = nil }) { session in
            SystemContactImport { draft in
                imported = BackendContactDraft(session: session, draft: draft)
            }
        }
        .communicationActions($action, sent: { Task { await state.load() } })
        .confirmationDialog("communication.chooseBackend", isPresented: Binding(
            get: { creation != nil }, set: { if !$0 { creation = nil } }
        ), titleVisibility: .visible, presenting: creation) { operation in
            ForEach(state.creationSessions) { session in
                Button(session.configuration.name) {
                    create(operation, session: session)
                    creation = nil
                }
                .accessibilityIdentifier("communication-create-\(session.configuration.name)")
            }
            Button("common.cancel", role: .cancel) { creation = nil }
        }
        .detailErrorAlert($state.failure)
        .foregroundTask(id: refreshIdentity) {
            do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            await state.load(automatic: true)
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(15)) } catch { return }
                if !state.loading { await state.load(automatic: true) }
            }
        }
        .alert("communication.delete", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { id in
            Button("communication.delete", role: .destructive) {
                Task { await state.delete(id) }
                deleting = nil
            }
            Button("common.cancel", role: .cancel) { }
        } message: { _ in
            Text(section == .messages ? LocalizedStringKey("communication.deleteThreadDescription") : LocalizedStringKey("communication.deleteCallDescription"))
        }
    }

    private var refreshIdentity: RefreshIdentity {
        RefreshIdentity(search: state.search, missedOnly: state.missedOnly, sources: state.sources.map {
            SourceIdentity(accessRevision: $0.session.accessRevision, supported: $0.supported,
                           accessible: $0.session.canAccessDevices)
        })
    }

    private var list: some View {
        List {
            backendStatus
            if section != .contacts {
                ForEach(state.availableSources, id: \.session.id) { source in
                    HistoryAssignmentSection(session: source.session, showsBackend: state.sources.count > 1)
                }
            }
            switch section {
            case .contacts:
                ForEach(ContactIndexSection.sections(state.contacts)) { group in
                    Section {
                        ForEach(group.contacts) { item in
                            Button { selectedContact = item } label: {
                                HStack(spacing: 12) {
                                    ContactAvatarView(session: item.session, contact: item.value, size: 36)
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(item.value.displayName).font(.body.weight(.medium)).foregroundStyle(.primary)
                                        Text(item.session.configuration.name).font(.caption).foregroundStyle(.secondary)
                                    }
                                    Spacer(minLength: 0)
                                }
                                .padding(.vertical, 4)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                        }
                    } header: {
                        if !group.contacts.isEmpty { Text(group.title).font(.footnote).textCase(nil) }
                    }
                    .sectionIndexLabel(group.title)
                }
            case .messages:
                let threads = state.threads
                ForEach(threads) { item in
                    NavigationLink {
                        CommunicationThreadView(session: item.session, thread: item.value, lines: lines(for: item.session))
                    } label: {
                        messageRow(item)
                    }
                    .listRowSeparator(item.id == threads.first?.id ? .hidden : .automatic, edges: .top)
                    .accessibilityIdentifier("conversation-\(item.value.id)")
                    .swipeActions(allowsFullSwipe: false) {
                        Button { deleting = item.id } label: { Image(systemName: "trash") }
                            .tint(.red)
                            .accessibilityLabel(Text("communication.delete"))
                    }
                }
            case .calls:
                let calls = state.calls
                ForEach(calls) { item in
                    callRow(item)
                        .listRowSeparator(item.id == calls.first?.id ? .hidden : .automatic, edges: .top)
                        .swipeActions(allowsFullSwipe: false) {
                            if item.value.terminal {
                                Button { deleting = item.id } label: { Image(systemName: "trash") }
                                    .tint(.red)
                                    .accessibilityLabel(Text("communication.delete"))
                            }
                        }
                }
            }
            if state.isEmpty && !state.availableSources.isEmpty {
                Group {
                    if state.loading {
                        ProgressView("communication.loading").frame(maxWidth: .infinity)
                    } else if !state.search.isEmpty {
                        ContentUnavailableView.search(text: state.search)
                    } else {
                        ContentUnavailableView(section.emptyTitle, systemImage: section.icon)
                    }
                }
                .listRowSeparator(.hidden)
            }
            if state.hasMore {
                Button("communication.more") { Task { await state.load(more: true) } }
                    .frame(maxWidth: .infinity)
                    .disabled(state.loading)
            }
        }
        .listStyle(.plain)
        .listSectionIndexVisibility(section == .contacts && !state.isEmpty ? .visible : .hidden)
        .contentMargins(.top, 0, for: .scrollContent)
        .scrollDismissesKeyboard(.interactively)
        .refreshable { await state.load(force: true) }
    }

    @ViewBuilder private var backendStatus: some View {
        ForEach(state.sources, id: \.session.id) { source in
            if !source.session.canAccessDevices {
                NavigationLink {
                    SettingsAccessView(session: source.session) { EmptyView() }
                } label: {
                    LabeledContent(source.session.configuration.name) { Text(LocalizedStringKey(source.session.statusKey)) }
                }
            } else if !source.supported {
                VStack(alignment: .leading, spacing: 4) {
                    Text(source.session.configuration.name)
                    Text("communication.callsUnavailable").font(.footnote).foregroundStyle(.secondary)
                }
            } else if let failure = source.failure {
                VStack(alignment: .leading, spacing: 4) {
                    Text(source.session.configuration.name)
                    Text(failure.message).font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func lines(for session: BackendSession) -> [CommunicationLine] {
        state.source(for: session.id)?.lines ?? []
    }

    private func messageRow(_ item: CommunicationRecord<CommunicationThread>) -> some View {
        let thread = item.value
        return MessageConversationRow(session: item.session, participant: thread.participant,
                                      preview: thread.latestMessage.text, timestamp: CallRecord.date(thread.latestMessage.timestamp),
                                      unread: thread.unreadCount > 0 || thread.markedUnread,
                                      line: "\(item.session.configuration.name) · \(thread.latestMessage.lineTitle)", showsDisclosure: false)
    }

    private func callRow(_ item: CommunicationRecord<CallRecord>) -> some View {
        let call = item.value, session = item.session
        return CallHistoryRow(record: call, session: session,
                              region: lines(for: session).first { $0.id == call.profileId }?.region,
                              source: "\(session.configuration.name) · \(call.lineLabel ?? lines(for: session).first { $0.id == call.profileId }?.displayLabel ?? session.modems.first { $0.id == call.modemId }?.communicationLabel ?? detailText("common.unknown"))",
                              select: { selectedCall = item },
                              callback: { action = BackendCommunicationAction(session: session, action: CommunicationAction(number: call.number, calling: true, preferredModemID: call.modemId)) })
    }

    @ViewBuilder private var filters: some View {
        if section == .messages {
            Picker("messages.filter", selection: $state.unreadOnly) {
                Text("messages.all").tag(false)
                Text("messages.unread").tag(true)
            }
            .pickerStyle(.segmented)
            .accessibilityIdentifier("messages-unread-filter")
        } else if section == .calls {
            Picker("calls.filter", selection: $state.missedOnly) {
                Text("calls.all").tag(false)
                Text("calls.missed").tag(true)
            }
            .pickerStyle(.segmented)
            .accessibilityIdentifier("calls-filter")
        }
    }

    @ViewBuilder private var composeButton: some View {
        if section == .contacts {
            Menu {
                Button("communication.newContact", systemImage: "person.crop.circle.badge.plus") { begin(.contact) }
                Button("communication.import", systemImage: "person.crop.circle") { begin(.importContact) }
            } label: {
                Label("communication.contactOptions", systemImage: "plus")
            }
            .accessibilityIdentifier("modem-contact-actions")
        } else {
            Button { begin(section == .calls ? .call : .message) } label: {
                Label(section == .calls ? LocalizedStringKey("communication.dial") : LocalizedStringKey("communication.newMessage"), systemImage: section == .calls ? "circle.grid.3x3.fill" : "square.and.pencil")
            }
        }
    }

    private func begin(_ operation: CommunicationCreation) {
        let sessions = state.creationSessions
        guard let session = sessions.first else { return }
        if sessions.count == 1 {
            create(operation, session: session)
        } else {
            creation = operation
        }
    }

    private func create(_ operation: CommunicationCreation, session: BackendSession) {
        switch operation {
        case .contact:
            editor = BackendContactDraft(session: session, draft: ContactDraft(contact: SigmoContact()))
        case .importContact:
            importSession = session
        case .message, .call:
            action = BackendCommunicationAction(session: session, action: CommunicationAction(number: "", calling: operation == .call, modemID: modemID))
        }
    }
}

struct CommunicationCallDetail: View {
    let session: BackendSession
    let record: CallRecord
    var sourceLine: CommunicationLine?
    @State private var action: CommunicationAction?

    var body: some View {
        CallHistoryDetail(record: record, session: session, modemID: record.modemId ?? "",
                          lineTitle: "\(session.configuration.name) · \(record.lineLabel ?? sourceLine?.displayLabel ?? session.modems.first { $0.id == record.modemId }?.communicationLabel ?? detailText("common.unknown"))", region: sourceLine?.region,
                          canMessage: session.canAccessDevices && !record.number.isEmpty,
                          message: { action = CommunicationAction(number: record.number, preferredModemID: record.modemId) },
                          callback: { action = CommunicationAction(number: record.number, calling: true, preferredModemID: record.modemId) })
            .communicationActions($action, session: session)
    }
}

struct SaveContactNumberView: View {
    @Environment(CallsState.self) private var calls
    let session: BackendSession
    let number: String
    @State private var editor: ContactDraft?
    var body: some View {
        List {
            Button("communication.newContact", systemImage: "plus") { editor = ContactDraft(contact: SigmoContact(numbers: [ContactPhone(number: number)])) }
            Section("communication.addExisting") {
                ForEach(session.savedContacts) { contact in
                    Button(contact.displayName) {
                        var draft = contact
                        if !draft.numbers.contains(where: { $0.key == number || $0.number == number }) { draft.numbers.append(ContactPhone(number: number)) }
                        editor = ContactDraft(contact: draft)
                    }
                }
            }
        }.navigationTitle("communication.addContact")
            .sheet(item: $editor) { draft in ContactEditView(session: session, draft: draft) }
            .task { do { try await session.refreshContacts() } catch { /* The new-contact action remains available. */ } }
    }
}

struct CommunicationThreadView: View {
    @Environment(CallsState.self) private var calls
    let session: BackendSession
    let thread: CommunicationThread
    var lines: [CommunicationLine] = []
    @State private var state: CommunicationThreadState
    @State private var action: CommunicationAction?
    @State private var composer: CommunicationComposerState
    @State private var pendingSend: UUID?
    @State private var visibleIDs: Set<Int64> = []
    @Environment(\.nativePush) private var push
    @Environment(\.scenePhase) private var scenePhase
    init(session: BackendSession, thread: CommunicationThread, lines: [CommunicationLine] = []) {
        self.session = session; self.thread = thread; self.lines = lines
        _state = State(initialValue: CommunicationThreadState(session: session, threadID: thread.id))
        _composer = State(initialValue: CommunicationComposerState(session: session, action: CommunicationAction(number: thread.participant, preferredModemID: thread.latestMessage.modemId)))
    }

    var body: some View {
        MessageTimeline(messages: timeline, loading: state.loading,
                        older: state.cursor.isEmpty ? nil : { Task { await state.load(more: true) } },
                        visible: { id, visible in
                            if visible { visibleIDs.insert(id) } else { visibleIDs.remove(id) }
                            if visible && scenePhase == .active, let message = state.messages.first(where: { $0.id == id }) {
                                Task { await acknowledge(message) }
                            }
                        }, refresh: { await state.load() })
        .navigationTitle(calls.name(number: thread.participant, session: session))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .safeAreaBar(edge: .bottom, spacing: 0) {
            MessageComposer(text: $composer.text, sending: composer.busy, enabled: composer.selected != nil) {
                pendingSend = UUID()
            }
        }
        .toolbar {
            ToolbarItem(placement: .principal) {
                VStack(spacing: 2) {
                    Text(calls.name(number: thread.participant, session: session)).font(.headline).lineLimit(1)
                    CommunicationModemPicker(state: composer, compact: true)
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
              Menu {
                Button("communication.dial", systemImage: "phone") { action = CommunicationAction(number: thread.participant, calling: true, preferredModemID: thread.latestMessage.modemId) }
                NavigationLink("communication.addContact") { SaveContactNumberView(session: session, number: thread.participant) }
              } label: { Image(systemName: "ellipsis.circle") }
            }
        }
        .communicationActions($action, session: session, sent: { Task { await state.load() } })
        .detailErrorAlert($state.failure)
        .foregroundTask {
            await state.load()
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(10)) } catch { return }
                await state.poll()
            }
        }
        .task { await composer.load(); if !Task.isCancelled { state.failure = composer.failure } }
        .task(id: pendingSend) {
            guard let pendingSend, !Task.isCancelled else { return }
            if await composer.send() { await state.load(preserve: true) }
            else if !Task.isCancelled { state.failure = composer.failure }
            if self.pendingSend == pendingSend { self.pendingSend = nil }
        }
        .onDisappear { pendingSend = nil }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { for message in state.messages where visibleIDs.contains(message.id) { Task { await acknowledge(message) } } }
        }
    }
    private var timeline: [MessageTimelineItem] {
        state.messages.map { message in
            MessageTimelineItem(message, source: "\(session.configuration.name) · \(message.lineTitle)")
        }
    }

    private func acknowledge(_ message: CommunicationMessage) async {
        guard let badge = await state.acknowledge(message) else { return }
        push?.record(badge, sessionID: session.id)
        await push?.removeNotifications(backendID: badge.backendId, conversationID: message.conversationId)
        await session.refreshUnreadCounts(minimumRevision: badge.revision)
    }
}

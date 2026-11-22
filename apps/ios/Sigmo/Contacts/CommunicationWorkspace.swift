import SwiftUI

struct CommunicationWorkspace: View {
    private struct RefreshIdentity: Equatable {
        let search: String
        let missedOnly: Bool
        let accessRevision: Int
        let accessible: Bool
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.defaultMinListRowHeight) private var minimumRowHeight
    let modemID: String?
    @Binding private var callbackDial: CommunicationDialRequest?
    @State private var state: CommunicationState
    @State private var editor: ContactDraft?
    @State private var importing = false
    @State private var imported: ContactDraft?
    @State private var action: CommunicationAction?
    @State private var deleting: String?
    @State private var selectedCall: CallRecord?
    @State private var selectedContactID: String?

    private var session: BackendSession { state.session }
    private var section: CommunicationSection { state.section }
    private var needsLogin: Bool { session.authentication == .required && session.failure == nil }

    init(session: BackendSession, section: CommunicationSection, modemID: String? = nil,
         callbackDial: Binding<CommunicationDialRequest?> = .constant(nil)) {
        self.modemID = modemID
        _callbackDial = callbackDial
        _state = State(initialValue: CommunicationState(session: session, section: section))
    }

    var body: some View {
        Group {
            if session.canAccessDevices {
                list
            } else if needsLogin {
                SettingsAccessView(session: session) { EmptyView() }
            } else {
                unavailableList
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
                composeButton.disabled(!session.canAccessDevices)
            }
        }
        .navigationDestination(item: $selectedContactID) { contactID in
            SigmoContactDetail(session: session, contactID: contactID, modemID: modemID)
        }
        .navigationDestination(item: $selectedCall) { record in
            CommunicationCallDetail(session: session, record: record,
                                    sourceLine: state.lines.first { $0.id == record.profileId })
        }
        .sheet(item: $editor) { draft in
            ContactEditView(session: session, draft: draft)
        }
        .sheet(isPresented: $importing, onDismiss: { editor = imported; imported = nil }) {
            SystemContactImport { imported = $0 }
        }
        .communicationActions($action, session: session, sent: { Task { await state.load() } })
        .detailErrorAlert($state.deletionFailure)
        .task(id: callbackDial?.id) { [request = callbackDial] in
            if let request { applyCallbackDial(request) }
        }
        .refreshable {
            if session.canAccessDevices {
                await state.load(force: true)
            } else {
                await session.refresh(force: true)
            }
        }
        .foregroundTask {
            if !session.canAccessDevices && !needsLogin { await session.refresh(force: true) }
        }
        .foregroundTask(id: refreshIdentity) {
            do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            await state.load(automatic: true)
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(15)) } catch { return }
                await state.load(automatic: true)
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
        RefreshIdentity(search: state.search, missedOnly: state.missedOnly,
                        accessRevision: session.accessRevision, accessible: session.canAccessDevices)
    }

    private var unavailableList: some View {
        List {
            Group {
                if session.isRefreshing && session.failure == nil {
                    ProgressView("communication.loading").frame(maxWidth: .infinity)
                } else {
                    emptyState
                }
            }
            .listRowSeparator(.hidden)
        }
        .listStyle(.plain)
        .contentMargins(.top, 0, for: .scrollContent)
    }

    private var emptyState: some View {
        ContentUnavailableView(section.emptyTitle, systemImage: section.icon)
    }

    private var list: some View {
        List {
            if section != .contacts {
                filters
                    .padding(.bottom, 8)
                    .frame(minHeight: minimumRowHeight, alignment: .top)
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
                    .listRowSeparator(.hidden)
            }
            if section != .contacts {
                HistoryAssignmentSection(session: session, lines: state.lines) {
                    await state.load(force: true)
                }
            }
            switch section {
            case .contacts:
                ForEach(ContactIndexSection.sections(state.contacts)) { group in
                    Section {
                        ForEach(group.contacts) { contact in
                            Button { selectedContactID = contact.id } label: {
                                HStack(spacing: 12) {
                                    ContactAvatarView(session: session, contact: contact, size: 36)
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(contact.displayName).font(.body.weight(.medium)).foregroundStyle(.primary)
                                        Text(session.configuration.name).font(.caption).foregroundStyle(.secondary)
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
                let threads = state.filteredThreads
                ForEach(threads) { thread in
                    NavigationLink {
                        CommunicationThreadView(session: session, thread: thread, lines: state.lines)
                    } label: {
                        messageRow(thread)
                    }
                    .listRowSeparator(thread.id == threads.first?.id ? .hidden : .automatic, edges: .top)
                    .accessibilityIdentifier("conversation-\(thread.id)")
                    .swipeActions(allowsFullSwipe: false) {
                        Button { deleting = thread.id } label: { Image(systemName: "trash") }
                            .tint(.red)
                            .accessibilityLabel(Text("communication.delete"))
                    }
                }
            case .calls:
                let calls = state.filteredCalls
                ForEach(calls) { call in
                    callRow(call)
                        .listRowSeparator(call.id == calls.first?.id ? .hidden : .automatic, edges: .top)
                        .swipeActions(allowsFullSwipe: false) {
                            if call.terminal {
                                Button { deleting = call.id } label: { Image(systemName: "trash") }
                                    .tint(.red)
                                    .accessibilityLabel(Text("communication.delete"))
                            }
                        }
                }
            }
            if state.isEmpty {
                Group {
                    if state.loading {
                        ProgressView("communication.loading").frame(maxWidth: .infinity)
                    } else if !state.search.isEmpty && state.failure == nil {
                        ContentUnavailableView.search(text: state.search)
                    } else {
                        emptyState
                    }
                }
                .listRowSeparator(.hidden)
            }
            if !state.cursor.isEmpty {
                Button("communication.more") { Task { await state.load(more: true) } }
                    .frame(maxWidth: .infinity)
                    .disabled(state.loading)
            }
        }
        .listStyle(.plain)
        .listSectionIndexVisibility(section == .contacts && !state.isEmpty ? .visible : .hidden)
        .contentMargins(.top, 0, for: .scrollContent)
        .scrollDismissesKeyboard(.interactively)
    }

    private func messageRow(_ thread: CommunicationThread) -> some View {
        MessageConversationRow(session: session, participant: thread.participant,
                               preview: thread.latestMessage.text, timestamp: CallRecord.date(thread.latestMessage.timestamp),
                               unread: thread.unreadCount > 0 || thread.markedUnread,
                               line: "\(session.configuration.name) · \(thread.latestMessage.lineTitle)", showsDisclosure: false)
    }

    private func callRow(_ call: CallRecord) -> some View {
        let line = state.lines.first { $0.id == call.profileId }
        return CallHistoryRow(record: call, session: session,
                              region: line?.region,
                              source: "\(session.configuration.name) · \(call.lineLabel ?? line?.displayLabel ?? session.modems.first { $0.id == call.modemId }?.communicationLabel ?? detailText("common.unknown"))",
                              select: { selectedCall = call },
                              callback: { action = CommunicationAction(number: call.number, calling: true, preferredModemID: call.modemId) })
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
                Button("communication.newContact", systemImage: "person.crop.circle.badge.plus") {
                    editor = ContactDraft(contact: SigmoContact())
                }
                Button("communication.import", systemImage: "person.crop.circle") { importing = true }
            } label: {
                Label("communication.contactOptions", systemImage: "plus")
            }
            .accessibilityIdentifier("modem-contact-actions")
        } else {
            Button {
                action = CommunicationAction(number: "", calling: section == .calls, modemID: modemID)
            } label: {
                Label(section == .calls ? LocalizedStringKey("communication.dial") : LocalizedStringKey("communication.newMessage"), systemImage: section == .calls ? "circle.grid.3x3.fill" : "square.and.pencil")
            }
        }
    }

    private func applyCallbackDial(_ request: CommunicationDialRequest) {
        guard !Task.isCancelled, section == .calls, request.id == callbackDial?.id,
              request.sessionID == session.incarnationID else { return }
        callbackDial = nil
        guard session.canAccessDevices else { return }
        action = CommunicationAction(number: request.number, calling: true, modemID: modemID)
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
        .task(id: push?.eventRevision) { if scenePhase == .active { await state.poll() } }
        .foregroundTask(id: visibleIDs) {
            do { try await Task.sleep(for: .milliseconds(150)) } catch { return }
            await acknowledgeVisibleMessages()
        }
        .task(id: pendingSend) {
            guard let pendingSend, !Task.isCancelled else { return }
            if await composer.send() { await state.load(preserve: true) }
            else if !Task.isCancelled { state.failure = composer.failure }
            if self.pendingSend == pendingSend { self.pendingSend = nil }
        }
        .onAppear { updateVisibility() }
        .onChange(of: visibility) { _, _ in updateVisibility() }
        .onDisappear {
            pendingSend = nil
            if push?.visibleThread == visibility { push?.visibleThread = nil }
        }
        .onChange(of: scenePhase) { _, phase in
            updateVisibility()
        }
    }
    private var timeline: [MessageTimelineItem] {
        state.messages.map { message in
            MessageTimelineItem(message, source: "\(session.configuration.name) · \(message.lineTitle)")
        }
    }

    private var visibility: PushThread {
        PushThread(backendID: push?.backendID(sessionID: session.id) ?? "", participant: thread.participant,
                   conversationIDs: Set(state.messages.map(\.conversationId)).union([thread.latestMessage.conversationId]))
    }

    private func updateVisibility() {
        if scenePhase == .active { push?.visibleThread = visibility }
        else if push?.visibleThread == visibility { push?.visibleThread = nil }
    }

    private func acknowledgeVisibleMessages() async {
        let conversations = Set(state.messages.filter { visibleIDs.contains($0.id) && $0.incoming && !$0.read }.map(\.conversationId))
        guard let badge = await state.acknowledge(visibleIDs) else { return }
        push?.record(badge, sessionID: session.id)
        for id in conversations { await push?.removeNotifications(backendID: badge.backendId, conversationID: id) }
    }
}

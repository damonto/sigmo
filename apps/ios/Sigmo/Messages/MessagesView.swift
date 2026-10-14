import SwiftUI

private struct ConversationAction: Identifiable {
    enum Kind { case read, unread, delete }
    let id = UUID()
    let conversation: SMSConversation
    let kind: Kind
}

struct MessagesView: View {
    @Bindable var state: MessagesState
    var initialConversationID: String?
    @Binding var pushed: SMSConversation?
    @Binding var composing: Bool
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.nativePush) private var push
    @State private var selected: SMSConversation?
    @State private var deleteCandidate: SMSConversation?
    @State private var action: ConversationAction?
    @State private var openedInitial = false
    @State private var missingInitial = false

    private struct RefreshIdentity: Equatable {
        let revision: Int
        let search: String
        let historical: Bool
    }

    var body: some View {
        HStack(spacing: 0) {
            list
                .frame(maxWidth: sizeClass == .regular ? 360 : .infinity)
            if sizeClass == .regular {
                Divider()
                if let selected {
                    MessageThreadView(state: state.thread(selected), inline: true)
                        .id(selected.id)
                } else {
                    ContentUnavailableView("messages.chooseConversation", systemImage: "message")
                        .frame(maxWidth: .infinity)
                }
            }
        }
        .background(Color(uiColor: .systemBackground))
        .detailErrorAlert(composing ? nil : state.failure)
        .sheet(isPresented: $composing) {
            NavigationStack {
                NewMessageView(state: state) { conversation in
                    composing = false
                    if let conversation { open(conversation) }
                }
            }
        }
        .alert("messages.deleteTitle", isPresented: Binding(get: { deleteCandidate != nil }, set: { if !$0 { deleteCandidate = nil } })) {
            Button("common.delete", role: .destructive) {
                if let conversation = deleteCandidate { action = ConversationAction(conversation: conversation, kind: .delete) }
                deleteCandidate = nil
            }
            Button("common.cancel", role: .cancel) { deleteCandidate = nil }
        } message: { Text("messages.deleteExplanation") }
        .foregroundTask(id: RefreshIdentity(revision: state.session.accessRevision, search: state.searchQuery, historical: state.historical)) {
            do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            await state.load()
        }
        .task(id: push?.eventRevision) {
            guard scenePhase == .active else { return }
            await state.load()
        }
        .task(id: action?.id) {
            guard let action else { return }
            switch action.kind {
            case .read: await state.mark(action.conversation, unread: false)
            case .unread: await state.mark(action.conversation, unread: true)
            case .delete:
                if await state.delete(action.conversation), selected?.id == action.conversation.id { selected = nil }
            }
            if self.action?.id == action.id { self.action = nil }
        }
        .task(id: state.loading) {
            guard state.loaded, !state.loading, !openedInitial, let initialConversationID else { return }
            if let conversation = await state.find(initialConversationID), !Task.isCancelled {
                openedInitial = true
                open(conversation)
            } else if !Task.isCancelled, state.failure == nil {
                openedInitial = true
                missingInitial = true
            }
        }
        .alert("messages.deleted", isPresented: $missingInitial) { Button("common.done", role: .cancel) {} }
        .onChange(of: state.badge) { _, value in if let value { push?.record(value, sessionID: state.session.id) } }
        .onChange(of: state.session.accessRevision) { _, _ in state.clear(); selected = nil; pushed = nil; composing = false }
        .onChange(of: state.session.canAccessDevices) { _, value in
            if !value && (state.session.authentication == .required || state.session.license == .activation) {
                state.clear(); selected = nil; pushed = nil
            }
        }
    }

    private var list: some View {
        List {
            if state.legacy { Text("messages.upgrade").font(.footnote).foregroundStyle(.secondary) }
            if !state.loaded && state.loading {
                ForEach(0..<5) { _ in
                    HStack(spacing: 12) {
                        Circle().fill(.quaternary).frame(width: 48, height: 48)
                        VStack(alignment: .leading) { Text("10086"); Text("messages.loading").foregroundStyle(.secondary) }
                    }.redacted(reason: .placeholder).accessibilityHidden(true)
                }
            } else {
                if state.loaded && !state.loading && state.visibleItems.isEmpty && state.failure == nil {
                    Group {
                        if state.searchQuery.isEmpty {
                            ContentUnavailableView("messages.empty", systemImage: "message", description: Text("messages.emptyDescription"))
                        } else {
                            ContentUnavailableView.search(text: state.searchQuery)
                        }
                    }
                    // Keep empty results below the filters and scrollable at large text sizes.
                    .listRowSeparator(.hidden)
                }
                let conversations = state.visibleItems
                ForEach(conversations) { conversation in
                    Button { open(conversation) } label: {
                        MessageConversationRow(session: state.session, participant: conversation.participant,
                            preview: conversation.latestMessage.text, timestamp: conversation.latestMessage.timestamp,
                            unread: conversation.unreadCount > 0 || conversation.markedUnread,
                            region: state.modem?.sim.identifier == conversation.profileId ? state.modem?.sim.regionCode : nil,
                            line: conversation.lineDescription(regionCode: state.modem?.sim.identifier == conversation.profileId ? state.modem?.sim.regionCode : nil))
                    }
                        .buttonStyle(.plain)
                        .listRowSeparator(conversation.id == conversations.first?.id ? .hidden : .automatic, edges: .top)
                        .listRowBackground(selected?.id == conversation.id && sizeClass == .regular ? Color.accentColor.opacity(0.1) : Color(uiColor: .systemBackground))
                        .accessibilityIdentifier("conversation-\(conversation.id)")
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            Button { deleteCandidate = conversation } label: { Image(systemName: "trash") }
                                .tint(.red)
                                .accessibilityLabel(Text("common.delete"))
                        }
                        .swipeActions(edge: .leading, allowsFullSwipe: false) {
                            if !state.legacy {
                                Button(conversation.unreadCount > 0 ? "messages.markRead" : "messages.markUnread", systemImage: conversation.unreadCount > 0 ? "envelope.open" : "envelope.badge") {
                                    action = ConversationAction(conversation: conversation, kind: conversation.unreadCount > 0 ? .read : .unread)
                                }.tint(.blue)
                            }
                        }
                }
            }
        }
        .listStyle(.plain)
        .contentMargins(.top, 0, for: .scrollContent)
        .accessibilityIdentifier("messages-list")
        .safeAreaInset(edge: .top, spacing: 0) {
            filters.padding(.horizontal, 16).padding(.bottom, 8)
        }
        .scrollDismissesKeyboard(.interactively)
        .refreshable { await state.load() }
        .disabled(action != nil)
    }

    private var filters: some View {
        HStack {
            Picker("messages.filter", selection: $state.unreadOnly) {
                Text("messages.all").tag(false)
                Text("messages.unread").tag(true)
            }
            .pickerStyle(.segmented)
            .disabled(state.legacy)
            .accessibilityIdentifier("messages-unread-filter")
            Menu {
                Toggle("messages.historical", isOn: $state.historical)
            } label: { Image(systemName: "line.3.horizontal.decrease") }
                .accessibilityLabel("messages.lines")
                .accessibilityIdentifier("messages-history-filter")
        }
    }

    private func open(_ conversation: SMSConversation) {
        if sizeClass == .regular { selected = conversation }
        else { pushed = conversation }
    }
}

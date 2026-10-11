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
        let background: Bool
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
        .task(id: RefreshIdentity(background: scenePhase == .background, revision: state.session.accessRevision, search: state.searchQuery, historical: state.historical)) {
            guard scenePhase != .background else { return }
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
        .onChange(of: scenePhase) { _, value in if value == .background { action = nil } }
    }

    private var list: some View {
        List {
            Section {
                HStack {
                    Picker("messages.filter", selection: $state.unreadOnly) {
                        Text("messages.all").tag(false)
                        Text("messages.unread").tag(true)
                    }.pickerStyle(.segmented).disabled(state.legacy)
                        .accessibilityIdentifier("messages-unread-filter")
                    Menu {
                        Toggle("messages.historical", isOn: $state.historical)
                    } label: { Image(systemName: "line.3.horizontal.decrease") }
                        .accessibilityLabel("messages.lines")
                        .accessibilityIdentifier("messages-history-filter")
                }
                .listRowSeparator(.hidden)
                if state.legacy { Text("messages.upgrade").font(.footnote).foregroundStyle(.secondary) }
                if let failure = state.failure { FailureView(failure: failure) }
            }
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
                ForEach(state.visibleItems) { conversation in
                    Button { open(conversation) } label: { ConversationRow(conversation: conversation) }
                        .buttonStyle(.plain)
                        .listRowBackground(selected?.id == conversation.id && sizeClass == .regular ? Color.accentColor.opacity(0.1) : Color(uiColor: .systemBackground))
                        .accessibilityIdentifier("conversation-\(conversation.id)")
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            Button("common.delete", systemImage: "trash", role: .destructive) { deleteCandidate = conversation }
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
        .scrollDismissesKeyboard(.interactively)
        .refreshable { await state.load() }
        .disabled(action != nil)
        .accessibilityIdentifier("messages-list")
    }

    private func open(_ conversation: SMSConversation) {
        if sizeClass == .regular { selected = conversation }
        else { pushed = conversation }
    }
}

private struct ConversationRow: View {
    let conversation: SMSConversation
    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            Image(systemName: "person.crop.circle.fill").font(.system(size: 46)).foregroundStyle(.gray.opacity(0.55))
                // Keep the unread marker in the list gutter without indenting the row.
                .overlay(alignment: .leading) {
                    if conversation.unreadCount > 0 {
                        Circle().fill(.blue).frame(width: 8, height: 8).offset(x: -14)
                    }
                }
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline) {
                    Text(conversation.participant.isEmpty ? String(localized: "messages.unknownSender") : conversation.participant)
                        .font(.headline).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(conversation.latestMessage.timestamp, format: .dateTime.month(.abbreviated).day().hour().minute())
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                Text(conversation.latestMessage.text).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                Text(conversation.lineDescription).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
            }
            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(conversation.unreadCount > 0 ? String(localized: "messages.unread") : "")
    }
}

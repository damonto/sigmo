import Foundation
import Observation

@MainActor @Observable
final class MessagesState: Identifiable {
    let session: BackendSession
    let modemID: String
    private let expectedProfileID: String?
    var search = ""
    var unreadOnly = false
    var historical = false
    var newRecipient = ""
    var newDraft = ""
    private(set) var items: [SMSConversation] = []
    private(set) var loading = false
    private(set) var loaded = false
    private(set) var sending = false
    private(set) var legacy = false
    private var loadFailure: BackendFailure?
    private(set) var sendFailure: BackendFailure?
    var failure: BackendFailure? { sendFailure ?? loadFailure }
    private(set) var badge: MessageBadge?
    @ObservationIgnored private var revision = 0
    // Polls may overlap an action; only clearing the page invalidates its result.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var threads: [String: MessageThreadState] = [:]

    init(session: BackendSession, modemID: String, expectedProfileID: String? = nil) {
        self.session = session
        self.modemID = modemID
        self.expectedProfileID = expectedProfileID
    }

    var modem: Modem? { session.modems.first { $0.id == modemID } }
    var searchQuery: String { search.trimmingCharacters(in: .whitespacesAndNewlines) }
    var visibleItems: [SMSConversation] { unreadOnly ? items.filter { $0.unreadCount > 0 } : items }
    var unreadCount: Int { items.reduce(0) { $0 + $1.unreadCount } }

    func load() async {
        guard !sending, session.canAccessDevices, !Task.isCancelled else { return }
        revision += 1
        let current = revision
        let modemID = modemID, query = searchQuery, historical = historical
        // Input can change during the debounce before the next load starts.
        func isCurrent() -> Bool {
            current == revision && query == searchQuery && historical == self.historical && !Task.isCancelled
        }
        loading = true
        defer { if current == revision { loading = false } }
        do {
            let snapshot = try await session.protectedRequest { api, token in
                try await api.conversations(modemID: historical ? nil : modemID, search: query, unassigned: historical, token: token)
            }
            guard isCurrent() else { return }
            items = snapshot.items
            badge = MessageBadge(backendId: snapshot.backendId, revision: snapshot.revision, unreadCount: snapshot.unreadCount)
            legacy = false
            loaded = true
            loadFailure = nil
        } catch let error as BackendFailure where error.status == 404 {
            guard let modem, isCurrent() else { return }
            do {
                let messages = try await session.protectedRequest { api, token in
                    try await api.legacyMessages(modemID: modemID, search: query, token: token)
                }
                guard isCurrent() else { return }
                items = historical ? [] : messages.map { SMSConversation.legacy($0, modem: modem) }
                loaded = true
                legacy = true
                loadFailure = nil
            } catch { if isCurrent() { loadFailure = .wrapping(error) } }
        } catch { if isCurrent() { loadFailure = .wrapping(error) } }
    }

    func thread(_ conversation: SMSConversation) -> MessageThreadState {
        if let thread = threads[conversation.id] { return thread }
        let thread = MessageThreadState(session: session, conversation: conversation, legacy: legacy)
        threads[conversation.id] = thread
        return thread
    }

    func find(_ id: String) async -> SMSConversation? {
        if let item = items.first(where: { $0.id == id }) { return item }
        let current = generation
        do {
            let snapshot = try await session.protectedRequest { api, token in try await api.conversations(modemID: nil, token: token) }
            guard current == generation, !Task.isCancelled else { return nil }
            return snapshot.items.first { $0.id == id }
        } catch {
            if current == generation, !Task.isCancelled { loadFailure = .wrapping(error) }
            return nil
        }
    }

    func mark(_ conversation: SMSConversation, unread: Bool) async {
        guard !legacy, !Task.isCancelled else { return }
        let current = generation
        let read = unread ? ConversationRead(markedUnread: true) : ConversationRead(readThroughMessageId: conversation.lastMessageId ?? conversation.latestMessage.id, markedUnread: false)
        do {
            let badge = try await session.protectedRequest { api, token in try await api.updateConversation(conversation.id, read: read, token: token) }
            guard current == generation, !Task.isCancelled else { return }
            self.badge = badge
            await session.refreshUnreadCounts(minimumRevision: badge.revision)
            guard current == generation, !Task.isCancelled else { return }
            await load()
        } catch { if current == generation, !Task.isCancelled { loadFailure = .wrapping(error) } }
    }

    func delete(_ conversation: SMSConversation) async -> Bool {
        guard !Task.isCancelled else { return false }
        let current = generation
        let legacy = legacy
        do {
            try await session.protectedRequest { api, token in
                if legacy { try await api.deleteLegacyMessages(modemID: conversation.modemId, participant: conversation.participant, token: token) }
                else { try await api.deleteConversation(conversation.id, token: token) }
            }
            guard current == generation, !Task.isCancelled else { return false }
            threads[conversation.id]?.clear()
            threads[conversation.id] = nil
            await session.refreshUnreadCounts()
            guard current == generation, !Task.isCancelled else { return false }
            await load()
            return current == generation && !Task.isCancelled
        } catch {
            if current == generation, !Task.isCancelled { loadFailure = .wrapping(error) }
            return false
        }
    }

    func sendNew() async -> SMSConversation? {
        guard !sending, !Task.isCancelled, let modem, !modem.sim.identifier.isEmpty else { return nil }
        if let expectedProfileID, expectedProfileID != modem.sim.identifier {
            sendFailure = .local("messages.inactiveLine")
            return nil
        }
        let submission = SMSSubmission(to: PhoneNumberDisplay.submission(newRecipient, regionCode: modem.sim.regionCode), text: newDraft, expectedProfileId: modem.sim.identifier)
        guard !submission.to.isEmpty, !submission.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        let modemID = modemID
        revision += 1
        let current = revision
        sending = true
        sendFailure = nil
        defer { if current == revision { sending = false } }
        do {
            let result = try await session.protectedRequest { api, token in try await api.sendMessage(modemID: modemID, submission: submission, token: token) }
            guard current == revision, !Task.isCancelled else { return nil }
            newRecipient = ""
            newDraft = ""
            sending = false
            await load()
            return items.first { $0.id == result.conversationId || ($0.profileId == submission.expectedProfileId && $0.participant == result.to) }
        } catch {
            guard current == revision else { return nil }
            let error = BackendFailure.wrapping(error)
            sendFailure = error.unconfirmedMessageSend || Task.isCancelled ? .local("messages.resultUnknown") : error
            sending = false
            if !Task.isCancelled { await load() }
            return nil
        }
    }

    func clear() {
        revision += 1
        generation += 1
        for thread in threads.values { thread.clear() }
        threads = [:]
        items = []
        search = ""
        newDraft = ""
        newRecipient = ""
        badge = nil
        loading = false
        loaded = false
        loadFailure = nil
        sendFailure = nil
        sending = false
    }
}

import Foundation
import Observation

@MainActor @Observable
final class MessageThreadState {
    let session: BackendSession
    private(set) var conversation: SMSConversation
    var draft = ""
    private var replacementProfileID: String?
    private(set) var messages: [SMSMessage] = []
    private(set) var cursor = ""
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var sending = false
    private var loadFailure: BackendFailure?
    private(set) var sendFailure: BackendFailure?
    var failure: BackendFailure? { sendFailure ?? loadFailure }
    private(set) var badge: MessageBadge?
    private(set) var snapshotBadge: MessageBadge?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var acknowledged: Int64
    @ObservationIgnored private var manuallyUnread: Bool
    @ObservationIgnored private var headCursor: String?

    init(session: BackendSession, conversation: SMSConversation) {
        self.session = session
        self.conversation = conversation
        acknowledged = conversation.readThroughMessageId
        manuallyUnread = conversation.markedUnread
    }

    var modem: Modem? { session.modems.first { $0.id == conversation.modemId } }
    var sendingProfileID: String { replacementProfileID ?? conversation.profileId }
    var canUseActive: Bool { modem.map { !$0.sim.identifier.isEmpty && $0.state != "locked" && $0.sim.identifier != sendingProfileID } ?? false }
    func useActiveProfile() { replacementProfileID = modem?.sim.identifier }
    var canSend: Bool {
        session.canAccessDevices && !conversation.modemId.isEmpty &&
        modem?.sim.identifier == sendingProfileID && modem?.state != "locked"
    }

    func load(more: Bool = false) async {
        guard !sending, !Task.isCancelled, session.canAccessDevices, !more || (!loading && !cursor.isEmpty) else { return }
        revision += 1
        let current = revision
        loading = true
        defer { if current == revision { loading = false } }
        do {
            try await loadPage(more: more, current: current)
        } catch { if current == revision, !Task.isCancelled { loadFailure = .wrapping(error) } }
    }

    private func loadPage(more: Bool, current: Int) async throws {
        let id = conversation.id, next = more ? cursor : ""
        var page = try await session.protectedRequest { api, token in
            try await api.conversationMessagePage(id, cursor: next, token: token)
        }
        var received = page.items
        var receivedIDs = Set(received.map(\.id))
        let refreshedHead = page.nextCursor
        let known = Set(messages.map(\.id))
        var overlapping = !known.isDisjoint(with: page.items.map(\.id))
        var visited: Set<String> = [next]
        // Backdated arrivals require the new snapshot even when its head overlaps.
        // Skip already displayed pages on demand instead of refetching them on every poll.
        while !page.nextCursor.isEmpty {
            let catchingUp = !more && loaded && !known.isEmpty && !overlapping && received.count < 1000
            let skippingKnown = more && !page.items.isEmpty && page.items.allSatisfy { known.contains($0.id) }
            guard catchingUp || skippingKnown else { break }
            let next = page.nextCursor
            guard visited.insert(next).inserted else { throw BackendFailure.local("api.invalidResponse") }
            page = try await session.protectedRequest { api, token in
                try await api.conversationMessagePage(id, cursor: next, token: token)
            }
            let ids = Set(page.items.map(\.id))
            guard ids.isEmpty ? page.nextCursor.isEmpty : !ids.isSubset(of: receivedIDs) else {
                throw BackendFailure.local("api.invalidResponse")
            }
            receivedIDs.formUnion(ids)
            received.append(contentsOf: page.items)
            overlapping = !known.isDisjoint(with: page.items.map(\.id))
        }
        guard current == revision, !Task.isCancelled, page.conversation.id == id else { return }
        var byID = Dictionary(uniqueKeysWithValues: (more || overlapping ? messages : []).map { ($0.id, $0) })
        for message in received { byID[message.id] = message }
        messages = byID.values.sorted { ($0.timestamp, $0.id) < ($1.timestamp, $1.id) }
        if more || !loaded || !overlapping || refreshedHead != headCursor { cursor = page.nextCursor }
        if !more { headCursor = refreshedHead }
        if page.revision >= max(badge?.revision ?? -1, snapshotBadge?.revision ?? -1) {
            conversation.readThroughMessageId = page.conversation.readThroughMessageId
            conversation.markedUnread = page.conversation.markedUnread
            conversation.unreadCount = page.conversation.unreadCount
            acknowledged = max(acknowledged, conversation.readThroughMessageId)
            manuallyUnread = conversation.markedUnread
            snapshotBadge = MessageBadge(backendId: page.backendId, revision: page.revision, unreadCount: page.unreadCount)
        }
        loaded = true
        loadFailure = nil
    }

    func markRead(through id: Int64) async {
        guard session.canAccessDevices, id > acknowledged || manuallyUnread else { return }
        let conversationID = conversation.id
        let current = revision
        do {
            let result = try await session.protectedRequest { api, token in
                try await api.updateConversation(conversationID, read: ConversationRead(readThroughMessageId: id, markedUnread: false), token: token)
            }
            guard !Task.isCancelled, current == revision else { return }
            acknowledged = max(acknowledged, id)
            manuallyUnread = false
            badge = result
            await session.refreshUnreadCounts(minimumRevision: result.revision)
        } catch { /* A later visible-message acknowledgment retries a canceled/offline read. */ }
    }

    func send() async {
        guard canSend, !sending, !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        revision += 1
        let current = revision
        let submission = SMSSubmission(to: conversation.participant, text: draft, expectedProfileId: sendingProfileID)
        let modemID = conversation.modemId
        sending = true
        sendFailure = nil
        do {
            let sent = try await session.protectedRequest { api, token in try await api.sendMessage(modemID: modemID, submission: submission, token: token) }
            guard current == revision else { return }
            draft = ""
            if let id = sent.conversationId, id != conversation.id,
               let snapshot = try? await session.protectedRequest({ api, token in try await api.conversations(modemID: modemID, token: token) }) {
                guard current == revision else { return }
                if let replacement = snapshot.items.first(where: { $0.id == id }) {
                    conversation = replacement
                    messages = []; cursor = ""; headCursor = nil; loaded = false; badge = nil; snapshotBadge = nil
                    replacementProfileID = nil
                    acknowledged = replacement.readThroughMessageId
                    manuallyUnread = replacement.markedUnread
                }
            }
            sending = false
            await load()
        } catch {
            if current == revision {
                let error = BackendFailure.wrapping(error)
                sendFailure = error.unconfirmedMessageSend || Task.isCancelled ? .local("messages.resultUnknown") : error
                sending = false
                if !Task.isCancelled { await load() }
            }
        }
        if current == revision || !sending { sending = false }
    }

    func clear() {
        revision += 1
        messages = []
        cursor = ""
        headCursor = nil
        draft = ""
        replacementProfileID = nil
        sending = false
        loading = false
        loaded = false
        loadFailure = nil
        sendFailure = nil
        badge = nil
        snapshotBadge = nil
    }
}

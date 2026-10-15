import Foundation
import Observation

@MainActor @Observable
final class MessageThreadState {
    let session: BackendSession
    private(set) var conversation: SMSConversation
    var draft = ""
    private var replacementProfileID: String?
    private(set) var messages: [SMSMessage] = []
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var sending = false
    private var loadFailure: BackendFailure?
    private(set) var sendFailure: BackendFailure?
    var failure: BackendFailure? { sendFailure ?? loadFailure }
    private(set) var badge: MessageBadge?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var acknowledged: Int64
    @ObservationIgnored private var manuallyUnread: Bool

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

    func load() async {
        guard !sending, !Task.isCancelled, session.canAccessDevices else { return }
        revision += 1
        let current = revision, conversation = conversation
        loading = true
        defer { if current == revision { loading = false } }
        do {
            let result = try await session.protectedRequest { api, token in
                try await api.conversationMessages(conversation.id, token: token)
            }
            guard current == revision, !Task.isCancelled else { return }
            messages = result
            let snapshot = try await session.protectedRequest { api, token in try await api.conversations(modemID: nil, token: token) }
            guard current == revision, !Task.isCancelled else { return }
            if let latest = snapshot.items.first(where: { $0.id == conversation.id }), snapshot.revision >= (badge?.revision ?? -1) {
                self.conversation = latest
                acknowledged = max(acknowledged, latest.readThroughMessageId)
                manuallyUnread = latest.markedUnread
            }
            loaded = true
            loadFailure = nil
        } catch { if current == revision, !Task.isCancelled { loadFailure = .wrapping(error) } }
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
        draft = ""
        replacementProfileID = nil
        sending = false
        loading = false
        loaded = false
        loadFailure = nil
        sendFailure = nil
        badge = nil
    }
}

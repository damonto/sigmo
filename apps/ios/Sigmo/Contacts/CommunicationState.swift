import Foundation
import Observation

@MainActor @Observable
final class CommunicationState {
    let session: BackendSession
    let section: CommunicationSection
    var search = "" {
        didSet { if search != oldValue { resetHistory() } }
    }
    var missedOnly = false {
        didSet { if missedOnly != oldValue { resetHistory() } }
    }
    private(set) var lines: [CommunicationLine] = []
    private(set) var hasLoadedMore = false
    var failure: BackendFailure?
    private(set) var loading = false
    private(set) var threads: [CommunicationThread] = []
    private(set) var calls: [CallRecord] = []
    private(set) var cursor = ""
    @ObservationIgnored private var generation = 0

    init(session: BackendSession, section: CommunicationSection) {
        self.session = session
        self.section = section
    }

    var supported: Bool {
        section != .calls || session.capabilities?.contains("callHistory") == true
    }

    var contacts: [SigmoContact] {
        guard !search.isEmpty else { return session.savedContacts }
        return session.savedContacts.filter { contact in
            contact.name.localizedStandardContains(search) || contact.numbers.contains { phone in
                phone.number.localizedStandardContains(search) || phone.key.localizedStandardContains(search)
            }
        }
    }

    private func resetHistory() {
        // Invalidate ownership immediately, including the debounce before the next request.
        generation += 1
        threads = []
        calls = []
        cursor = ""
        hasLoadedMore = false
        loading = false
        failure = nil
    }

    func load(more: Bool = false, force: Bool = false) async {
        guard supported, session.canAccessDevices, !Task.isCancelled else { return }
        guard !more || (!loading && !cursor.isEmpty) else { return }
        generation += 1
        let current = generation
        let access = session.accessRevision
        let search = search
        let cursor = more ? cursor : ""
        let missed = missedOnly
        loading = true
        defer { if current == generation { loading = false } }
        do {
            async let metadata = loadMetadata(force: force)
            switch section {
            case .contacts:
                try await session.refreshContacts(force: force)
            case .messages:
                let page = try await session.protectedRequest { api, token in try await api.messageThreads(search: search, cursor: cursor, token: token) }
                guard current == generation, access == session.accessRevision, !Task.isCancelled else { return }
                threads = more ? threads + page.items : page.items
                self.cursor = page.nextCursor
                hasLoadedMore = more
            case .calls:
                let page = try await session.protectedRequest { api, token in try await api.callHistory(search: search, cursor: cursor, missed: missed, token: token) }
                guard current == generation, access == session.accessRevision, !Task.isCancelled else { return }
                calls = more ? calls + page.items : page.items
                self.cursor = page.nextCursor
                hasLoadedMore = more
            }
            guard current == generation, access == session.accessRevision, !Task.isCancelled else { return }
            loading = false
            failure = nil
            let inventory = await metadata
            guard current == generation, access == session.accessRevision, !Task.isCancelled else { return }
            if let inventory { lines = inventory }
        } catch {
            if current == generation, access == session.accessRevision, !Task.isCancelled {
                failure = .wrapping(error)
            }
        }
    }

    private func loadMetadata(force: Bool) async -> [CommunicationLine]? {
        guard section != .contacts else { return nil }
        // Optional names and line labels must not delay or hide usable history.
        async let contacts: Void? = try? session.refreshContacts(force: force)
        if force { session.invalidateCommunicationLines() }
        let inventory = try? await session.communicationLines()
        _ = await contacts
        return inventory
    }

    func delete(_ id: String) async {
        do {
            let section = section
            try await session.protectedRequest { api, token in
                switch section {
                case .contacts: try await api.deleteContact(id, token: token)
                case .messages: try await api.deleteThread(id, token: token)
                case .calls: try await api.deleteCallHistory(id, token: token)
                }
            }
            await load(force: true)
        } catch { if !Task.isCancelled { failure = .wrapping(error) } }
    }
}

@MainActor @Observable
final class CommunicationComposerState {
    let session: BackendSession
    let calling: Bool
    let fixedModemID: String?
    private let originalModemID: String?
    var number: String
    var text = ""
    var selectedID = ""
    var failure: BackendFailure?
    private(set) var modems: [Modem] = []
    private(set) var unavailable = false
    private(set) var busy = false
    @ObservationIgnored private var revision = 0
    private var explicitModemID: String?

    init(session: BackendSession, action: CommunicationAction) {
        self.session = session
        number = action.number
        calling = action.calling
        fixedModemID = action.modemID
        originalModemID = action.modemID ?? action.preferredModemID
    }
    var availableModems: [Modem] { modems.filter { fixedModemID == nil || $0.id == fixedModemID } }
    var selected: Modem? { availableModems.first { $0.id == selectedID } }
    var canSubmit: Bool { selected != nil && !number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && (calling || !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) && !busy }
    static func choose(modems: [Modem], originalID: String) -> String {
        if modems.contains(where: { $0.id == originalID }) { return originalID }
        return modems.count == 1 ? modems[0].id : ""
    }
    func load() async {
        revision += 1
        let current = revision
        do {
            let devices = try await session.protectedRequest { api, token in try await api.modems(token: token) }
            guard current == revision, !Task.isCancelled else { return }
            modems = devices
            let preferred = explicitModemID ?? originalModemID ?? ""
            selectedID = Self.choose(modems: availableModems, originalID: preferred)
            unavailable = !preferred.isEmpty && !availableModems.contains { $0.id == preferred }
            failure = nil
        } catch { if current == revision, !Task.isCancelled { failure = .wrapping(error) } }
    }
    func selectModem(_ id: String) {
        explicitModemID = id
        selectedID = id
        if !id.isEmpty { unavailable = false }
    }
    // The user chooses a modem; expectedProfileId still protects the short submit race.
    func currentModem() async -> Modem? {
        guard let id = selected?.id else { return nil }
        await session.refreshModem(id)
        guard !Task.isCancelled, session.canAccessDevices,
              session.detailFailures[id] == nil,
              let modem = session.modems.first(where: { $0.id == id }),
              !modem.sim.identifier.isEmpty, modem.state != "locked" else {
            await load()
            failure = .local("communication.modemUnavailable")
            return nil
        }
        return modem
    }
    func send() async -> Bool {
        guard canSubmit, !Task.isCancelled else { return false }
        let recipient = PhoneNumberDisplay.submission(number, regionCode: nil)
        guard !recipient.isEmpty else { failure = .local("apiError.invalid_recipient"); return false }
        let draft = text
        busy = true
        failure = nil
        defer { busy = false }
        guard let modem = await currentModem() else { return false }
        let submission = SMSSubmission(to: recipient, text: draft, expectedProfileId: modem.sim.identifier)
        do {
            _ = try await session.protectedRequest { api, token in try await api.sendMessage(modemID: modem.id, submission: submission, token: token) }
            guard !Task.isCancelled else { return false }
            if text == draft { text = "" }
            return true
        } catch {
            let error = BackendFailure.wrapping(error)
            failure = error.unconfirmedMessageSend || Task.isCancelled ? .local("messages.resultUnknown") : error
            return false
        }
    }
}

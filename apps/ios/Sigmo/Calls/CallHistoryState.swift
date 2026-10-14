import Foundation
import Observation

@MainActor @Observable
final class CallHistoryState {
    let session: BackendSession
    let modemID: String
    var search = ""
    var missedOnly = false
    private(set) var items: [CallRecord] = []
    private(set) var loading = false
    private(set) var loaded = false
    private(set) var deleting = false
    var failure: BackendFailure?
    @ObservationIgnored private var revision = 0

    init(session: BackendSession, modemID: String) {
        self.session = session
        self.modemID = modemID
    }

    var line: CallLine? { session.modems.first { $0.id == modemID }.map { CallLine(session: session, modem: $0) } }
    var visibleItems: [CallRecord] {
        let query = CallDigits.normalize(search)
        return items.filter { (!missedOnly || $0.missed) && (query.isEmpty || CallDigits.normalize($0.number).contains(query)) }
    }

    func canMessage(_ record: CallRecord) -> Bool {
        guard session.canAccessDevices, let line, !line.profileID.isEmpty else { return false }
        return !record.number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    func messageDraft(for record: CallRecord) -> MessagesState? {
        guard canMessage(record) else { return nil }
        // Preserve the Messages tab draft and pin the SIM chosen for this action.
        let draft = MessagesState(session: session, modemID: modemID, expectedProfileID: line?.profileID)
        draft.newRecipient = record.number
        return draft
    }

    func load() async {
        guard session.canAccessDevices, let line, !deleting, !Task.isCancelled else { return }
        revision += 1
        let current = revision, modemID = modemID, query = CallDigits.normalize(search)
        loading = true
        defer { if current == revision { loading = false } }
        do {
            let aggregate = session.capabilities?.contains("callHistory") == true
            let records = try await session.protectedRequest { api, token in
                if aggregate { return try await api.callHistory(search: query, cursor: "", modemID: modemID, token: token).items }
                return try await api.calls(modemID: modemID, search: query, token: token)
            }
            guard current == revision, self.line?.matches(line) == true, !Task.isCancelled else { return }
            items = records.sorted { $0.date > $1.date }
            loaded = true
            failure = nil
        } catch { if current == revision, !Task.isCancelled { failure = .wrapping(error) } }
    }

    func delete(_ record: CallRecord) async -> Bool {
        guard record.terminal, let line, !deleting else { return false }
        revision += 1
        let current = revision, modemID = modemID
        deleting = true
        defer { if current == revision { deleting = false } }
        do {
            let aggregate = session.capabilities?.contains("callHistory") == true
            try await session.protectedRequest { api, token in
                if aggregate { try await api.deleteCallHistory(record.id, token: token) }
                else { try await api.deleteCall(modemID: modemID, id: record.id, token: token) }
            }
            guard current == revision, self.line?.matches(line) == true, !Task.isCancelled else { return false }
            items.removeAll { $0.id == record.id }
            failure = nil
            return true
        } catch { if current == revision, !Task.isCancelled { failure = .wrapping(error) }; return false }
    }

    func clear() { revision += 1; items = []; loaded = false; loading = false; deleting = false; failure = nil }
}

@MainActor @Observable
final class CallUSSD {
    private(set) var messages: [String] = []
    private(set) var busy = false
    private(set) var opened = false
    private(set) var ended = false
    var reply = ""
    var failure: BackendFailure?
    let session: BackendSession
    let line: CallLine
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var revision = 0

    init(session: BackendSession, line: CallLine) { self.session = session; self.line = line }

    func send(_ code: String, initial: Bool = false) {
        guard !busy, !ended, !code.isEmpty else { return }
        guard session.modems.contains(where: { $0.id == line.modemID && $0.sim.identifier == line.profileID }),
              session.configuration.address.absoluteString == line.address else {
            failure = .local("calls.lineUnavailable"); ended = true; return
        }
        opened = true
        busy = true
        failure = nil
        let current = revision
        task = Task {
            defer { if current == revision { busy = false } }
            do {
                let modemID = line.modemID
                let response = try await session.protectedRequest { api, token in
                    try await api.ussd(modemID: modemID, action: initial ? "initialize" : "reply", code: code, token: token)
                }
                guard current == revision, !Task.isCancelled,
                      session.modems.contains(where: { $0.id == line.modemID && $0.sim.identifier == line.profileID }) else { return }
                messages.append(response)
                reply = ""
            } catch {
                guard current == revision, !Task.isCancelled else { return }
                let error = BackendFailure.wrapping(error)
                failure = error.code == "ussd_timeout" || error.isNetwork ? .local("calls.ussdUnconfirmed") : error
                ended = true
            }
        }
    }

    func close() { revision += 1; task?.cancel(); task = nil; busy = false; ended = true }
}

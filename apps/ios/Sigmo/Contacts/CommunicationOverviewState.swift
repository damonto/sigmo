import Foundation
import Observation

struct CommunicationRecordID: Hashable {
    let backendID: UUID
    let recordID: String
}

// Backend-local IDs and matching phone numbers must never combine records from different servers.
struct CommunicationRecord<Value: Identifiable>: Identifiable, Hashable where Value.ID == String {
    let id: CommunicationRecordID
    let session: BackendSession
    let value: Value

    @MainActor init(session: BackendSession, value: Value) {
        self.id = CommunicationRecordID(backendID: session.id, recordID: value.id)
        self.session = session
        self.value = value
    }

    static func == (lhs: Self, rhs: Self) -> Bool { lhs.id == rhs.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
}

@MainActor @Observable
final class CommunicationOverviewState {
    let sources: [CommunicationState]
    private let section: CommunicationSection
    var search = "" {
        didSet { for source in sources { source.search = search } }
    }
    var unreadOnly = false
    var missedOnly = false {
        didSet { for source in sources { source.missedOnly = missedOnly } }
    }
    var failure: BackendFailure?

    init(sessions: [BackendSession], section: CommunicationSection) {
        self.section = section
        sources = sessions.map { CommunicationState(session: $0, section: section) }
    }

    var availableSources: [CommunicationState] {
        sources.filter { $0.supported && $0.session.canAccessDevices }
    }
    var loading: Bool { availableSources.contains(where: \.loading) }
    var hasMore: Bool { availableSources.contains { !$0.cursor.isEmpty } }
    var creationSessions: [BackendSession] {
        sources.filter { $0.session.canAccessDevices && $0.supportsComposer }.map(\.session)
    }

    var isEmpty: Bool {
        !availableSources.contains { source in
            switch section {
            case .contacts: !source.contacts.isEmpty
            case .messages: source.threads.contains { !unreadOnly || $0.unreadCount > 0 || $0.markedUnread }
            case .calls: source.calls.contains { !missedOnly || $0.missed }
            }
        }
    }

    var contacts: [CommunicationRecord<SigmoContact>] {
        availableSources.flatMap { source in
            source.contacts.map { CommunicationRecord(session: source.session, value: $0) }
        }.sorted {
            let order = $0.value.displayName.localizedStandardCompare($1.value.displayName)
            return order == .orderedSame ? precedes($0.id, $1.id) : order == .orderedAscending
        }
    }
    var threads: [CommunicationRecord<CommunicationThread>] {
        let records: [(record: CommunicationRecord<CommunicationThread>, date: Date)] = availableSources.flatMap { source in
            source.threads.filter { !unreadOnly || $0.unreadCount > 0 || $0.markedUnread }
                .map { (record: CommunicationRecord(session: source.session, value: $0), date: CallRecord.date($0.latestMessage.timestamp) ?? .distantPast) }
        }
        return records.sorted {
            $0.date == $1.date ? precedes($0.record.id, $1.record.id) : $0.date > $1.date
        }.map { $0.record }
    }
    var calls: [CommunicationRecord<CallRecord>] {
        let records: [(record: CommunicationRecord<CallRecord>, date: Date)] = availableSources.flatMap { source in
            source.calls.filter { !missedOnly || $0.missed }
                .map { (record: CommunicationRecord(session: source.session, value: $0), date: $0.date) }
        }
        return records.sorted {
            $0.date == $1.date ? precedes($0.record.id, $1.record.id) : $0.date > $1.date
        }.map { $0.record }
    }

    func source(for backendID: UUID) -> CommunicationState? {
        sources.first { $0.session.id == backendID }
    }

    func load(more: Bool = false, force: Bool = false, automatic: Bool = false) async {
        let current = availableSources.filter { source in
            (!more || !source.cursor.isEmpty)
                && (!automatic || (source.session.foreground && !source.hasLoadedMore))
        }
        // Each source owns its cursor, request generation and error. A slow or unavailable
        // backend does not hold back records that have already arrived from another one.
        await withTaskGroup(of: Void.self) { group in
            for source in current {
                group.addTask { await source.load(more: more, force: force) }
            }
        }
    }

    func delete(_ id: CommunicationRecordID) async {
        guard let source = source(for: id.backendID) else { return }
        await source.delete(id.recordID)
        failure = source.failure
    }

    private func precedes(_ lhs: CommunicationRecordID, _ rhs: CommunicationRecordID) -> Bool {
        (lhs.backendID.uuidString, lhs.recordID) < (rhs.backendID.uuidString, rhs.recordID)
    }
}

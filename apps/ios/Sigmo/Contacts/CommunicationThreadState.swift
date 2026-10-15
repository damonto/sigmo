import Foundation
import Observation

@MainActor @Observable
final class CommunicationThreadState {
    let session: BackendSession
    let threadID: String
    private(set) var messages: [CommunicationMessage] = []
    private(set) var cursor = ""
    private(set) var loaded = false
    private(set) var loading = false
    var failure: BackendFailure?
    @ObservationIgnored private var acknowledged: Set<Int64> = []
    @ObservationIgnored private var acknowledgmentTask: Task<MessageBadge?, Never>?
    @ObservationIgnored private var headCursor: String?

    init(session: BackendSession, threadID: String) {
        self.session = session
        self.threadID = threadID
    }

    func poll() async {
        guard session.foreground else { return }
        await load(preserve: true)
    }

    func load(more: Bool = false, preserve: Bool = false) async {
        guard !loading, !Task.isCancelled, session.canAccessDevices,
              !more || (loaded && !cursor.isEmpty) else { return }
        loading = true
        defer { loading = false }
        let id = threadID, next = more ? cursor : ""
        do {
            var page = try await session.protectedRequest { api, token in
                try await api.threadMessages(id, cursor: next, token: token)
            }
            var received = page.items
            var receivedIDs = Set(received.map(\.id))
            let refreshedHead = page.nextCursor
            let known = Set(messages.map(\.id))
            var overlapping = !known.isDisjoint(with: page.items.map(\.id))
            var visited: Set<String> = [next]
            // A changed snapshot may include backdated imports beyond the head.
            // Rebase its cursor, skipping retained pages when the reader asks for more.
            while !page.nextCursor.isEmpty {
                let catchingUp = !more && preserve && loaded && !known.isEmpty && !overlapping && received.count < 1000
                let skippingKnown = more && !page.items.isEmpty && page.items.allSatisfy { known.contains($0.id) }
                guard catchingUp || skippingKnown else { break }
                let cursor = page.nextCursor
                guard visited.insert(cursor).inserted else { throw BackendFailure.local("api.invalidResponse") }
                page = try await session.protectedRequest { api, token in
                    try await api.threadMessages(id, cursor: cursor, token: token)
                }
                let ids = Set(page.items.map(\.id))
                guard ids.isEmpty ? page.nextCursor.isEmpty : !ids.isSubset(of: receivedIDs) else {
                    throw BackendFailure.local("api.invalidResponse")
                }
                receivedIDs.formUnion(ids)
                received.append(contentsOf: page.items)
                overlapping = !known.isDisjoint(with: page.items.map(\.id))
            }
            try Task.checkCancellation()
            let retainingHistory = more || (preserve && loaded && overlapping)
            var byID: [Int64: CommunicationMessage] = [:]
            for message in retainingHistory ? messages : [] { byID[message.id] = message }
            for var message in received {
                // A read snapshot started before an acknowledgment cannot undo it.
                if acknowledged.contains(message.id) { message.read = true }
                byID[message.id] = message
            }
            // RFC3339 strings with different offsets or precision are not sortable.
            messages = byID.values.map { (message: $0, date: CallRecord.date($0.timestamp) ?? .distantPast) }
                .sorted { ($0.date, $0.message.id) < ($1.date, $1.message.id) }
                .map(\.message)
            if more || !preserve || !loaded || !overlapping || refreshedHead != headCursor { cursor = page.nextCursor }
            if !more { headCursor = refreshedHead }
            loaded = true
            failure = nil
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
        }
    }

    func acknowledge(_ message: CommunicationMessage) async -> MessageBadge? {
        await acknowledge([message.id])
    }

    func acknowledge(_ visibleIDs: Set<Int64>) async -> MessageBadge? {
        // Visibility changes may cancel the debounce caller, but not an accepted
        // read batch. Serialize successors so overlapping IDs are never skipped.
        while let pending = acknowledgmentTask {
            _ = await pending.value
            if Task.isCancelled { return nil }
        }
        guard !Task.isCancelled else { return nil }
        let task = Task { @MainActor in
            defer { acknowledgmentTask = nil }
            return await acknowledgeBatch(visibleIDs)
        }
        acknowledgmentTask = task
        return await task.value
    }

    private func acknowledgeBatch(_ visibleIDs: Set<Int64>) async -> MessageBadge? {
        let ids = messages.filter {
            visibleIDs.contains($0.id) && $0.incoming && !$0.read &&
                !acknowledged.contains($0.id)
        }.prefix(1000).map(\.id)
        guard !ids.isEmpty, !Task.isCancelled else { return nil }
        let id = threadID
        do {
            let badge = try await session.protectedRequest { api, token in
                try await api.readThread(id, messageIDs: ids, token: token)
            }
            try Task.checkCancellation()
            acknowledged.formUnion(ids)
            for index in messages.indices where acknowledged.contains(messages[index].id) { messages[index].read = true }
            return badge
        } catch {
            if !Task.isCancelled, session.canAccessDevices { failure = .wrapping(error) }
            return nil
        }
    }
}

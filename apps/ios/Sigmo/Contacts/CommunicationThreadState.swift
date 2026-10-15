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
    @ObservationIgnored private var acknowledging: Set<Int64> = []
    @ObservationIgnored private var acknowledged: Set<Int64> = []

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
            let page = try await session.protectedRequest { api, token in
                try await api.threadMessages(id, cursor: next, token: token)
            }
            try Task.checkCancellation()
            let retainingHistory = more || (preserve && loaded)
            var byID: [Int64: CommunicationMessage] = [:]
            for message in retainingHistory ? messages : [] { byID[message.id] = message }
            for var message in page.items {
                // A read snapshot started before an acknowledgment cannot undo it.
                if acknowledged.contains(message.id) { message.read = true }
                byID[message.id] = message
            }
            // RFC3339 strings with different offsets or precision are not sortable.
            messages = byID.values.map { (message: $0, date: CallRecord.date($0.timestamp) ?? .distantPast) }
                .sorted { ($0.date, $0.message.id) < ($1.date, $1.message.id) }
                .map(\.message)
            // Polls retain the older-page snapshot, but a successful first read
            // must establish a cursor even when it follows an initial failure.
            if !preserve || !loaded { cursor = page.nextCursor }
            loaded = true
            failure = nil
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
        }
    }

    func acknowledge(_ message: CommunicationMessage) async -> MessageBadge? {
        guard message.incoming, !message.read, !acknowledged.contains(message.id),
              !acknowledging.contains(message.id), !Task.isCancelled else { return nil }
        acknowledging.insert(message.id)
        defer { acknowledging.remove(message.id) }
        let id = threadID
        do {
            let badge = try await session.protectedRequest { api, token in
                try await api.readThread(id, messageIDs: [message.id], token: token)
            }
            try Task.checkCancellation()
            acknowledged.insert(message.id)
            if let index = messages.firstIndex(where: { $0.id == message.id }) { messages[index].read = true }
            return badge
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
            return nil
        }
    }
}

#if DEBUG
import Foundation

// UI and lifecycle tests use in-memory messages; they never reach a modem or APNs.
actor MessagesFixture {
    private let base = PreviewBackendServer()
    private let activeProfile = "8986000000000000000"
    private var revision: Int64 = 1
    private var nextID: Int64 = 5
    private var messages: [String: [SMSMessage]] = [:]
    private var watermarks: [String: Int64] = [:]
    private var unread: Set<String> = []
    private var sendStatus = 200
    private var offline = false
    private var legacy = false
    private var holdPath: String?
    private var held: CheckedContinuation<Void, Never>?
    private var arrived: CheckedContinuation<Void, Never>?
    private(set) var requests: [URLRequest] = []

    init(unreadMessages: Int? = nil) {
        let activeProfile = activeProfile
        let date = Date(timeIntervalSince1970: 1_790_000_000)
        func message(_ id: Int64, _ conversation: String, _ profile: String, _ text: String, incoming: Bool = true) -> SMSMessage {
            SMSMessage(id: id, conversationId: conversation, profileId: profile, sender: incoming ? "10086" : "+8613800000000", recipient: incoming ? "+8613800000000" : "10086", text: text, timestamp: date.addingTimeInterval(Double(id) * 100), status: incoming ? "received-unread" : "stored-sent", incoming: incoming)
        }
        messages["conversation-active"] = [message(1, "conversation-active", activeProfile, "Welcome to Sigmo / 欢迎使用 Sigmo"), message(2, "conversation-active", activeProfile, "Balance: 20.00. https://example.com/balance")]
        messages["conversation-inactive"] = [message(3, "conversation-inactive", "8986000000000000001", "History on your Travel line / 旅行线路历史")]
        messages["conversation-history"] = [message(4, "conversation-history", "unknown-profile", "Unassigned historical message / 历史消息")]
        if let unreadMessages {
            messages["conversation-active"] = (0..<unreadMessages).map {
                message(Int64($0 + 5), "conversation-active", activeProfile, "Unread message \($0 + 1)")
            }
            nextID = Int64(unreadMessages + 5)
        }
        watermarks["conversation-inactive"] = 3
        watermarks["conversation-history"] = 4
    }

    func setSendStatus(_ value: Int) { sendStatus = value }
    func setOffline(_ value: Bool) { offline = value }
    func setLegacy(_ value: Bool) { legacy = value }
    func holdNext(_ suffix: String) { holdPath = suffix }
    func waitForHeld() async {
        if held != nil { return }
        await withCheckedContinuation { arrived = $0 }
    }
    func release() { held?.resume(); held = nil }
    func requestCount(method: String, suffix: String) -> Int { requests.filter { $0.httpMethod == method && $0.url?.path.hasSuffix(suffix) == true }.count }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if offline { throw URLError(.notConnectedToInternet) }
        let path = request.url?.path ?? ""
        if let suffix = holdPath, path.hasSuffix(suffix) {
            holdPath = nil
            await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
        }
        let method = request.httpMethod ?? "GET"
        if path.contains("/conversations") {
            if legacy { return response(status: 404) }
            let components = path.components(separatedBy: "/conversations")
            let tail = components.last ?? ""
            if tail.isEmpty {
                let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
                var items = conversations()
                if query.contains(where: { $0.name == "unassigned" && $0.value == "true" }) { items = items.filter { $0.modemId.isEmpty } }
                if let modem = query.first(where: { $0.name == "modemId" })?.value { items = items.filter { $0.modemId == modem } }
                if let search = query.first(where: { $0.name == "q" })?.value, !search.isEmpty {
                    items = items.filter { item in item.participant.contains(search) || (messages[item.id] ?? []).contains { $0.text.localizedCaseInsensitiveContains(search) } }
                }
                struct Snapshot: Encodable { let backendId: String; let revision: Int64; let unreadCount: Int; let items: [SMSConversation] }
                return try response(Snapshot(backendId: "fixture-backend", revision: revision, unreadCount: badge, items: items))
            }
            let id = String(tail.dropFirst().split(separator: "/").first ?? "")
            guard messages[id] != nil else { return response(status: 404) }
            if method == "DELETE" { messages[id] = nil; watermarks[id] = nil; unread.remove(id); revision += 1; return response(status: 204) }
            if method == "PATCH" {
                struct Update: Decodable { let readThroughMessageId: Int64?; let markedUnread: Bool? }
                let value = try JSONDecoder().decode(Update.self, from: request.httpBody ?? Data())
                if let through = value.readThroughMessageId { watermarks[id] = max(watermarks[id] ?? 0, through); unread.remove(id) }
                if value.markedUnread == true { unread.insert(id) }
                if value.markedUnread == false { unread.remove(id) }
                revision += 1
                return try response(MessageBadge(backendId: "fixture-backend", revision: revision, unreadCount: badge))
            }
            return try response(messages[id] ?? [])
        }
        if path.contains("/messages") {
            if method == "POST" {
                struct Submission: Decodable { let to, text, expectedProfileId: String }
                let body = try JSONDecoder().decode(Submission.self, from: request.httpBody ?? Data())
                guard body.expectedProfileId == activeProfile else { return response(status: 409) }
                if sendStatus == 409 { return response(status: 409) }
                let message = SMSMessage(id: nextID, conversationId: "conversation-active", profileId: activeProfile, sender: "+8613800000000", recipient: body.to, text: body.text, timestamp: Date(), status: "stored-sent", incoming: false)
                nextID += 1; revision += 1
                messages["conversation-active", default: []].append(message)
                if sendStatus == 408 { throw URLError(.timedOut) }
                if sendStatus >= 400 { return response(status: sendStatus) }
                struct Sent: Encodable { let to, conversationId: String }
                return try response(Sent(to: body.to, conversationId: "conversation-active"))
            }
            var items = messages["conversation-active"] ?? []
            if path.hasSuffix("/messages") {
                let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
                if let search = query.first(where: { $0.name == "q" })?.value, !search.isEmpty {
                    items = items.filter {
                        $0.sender.localizedCaseInsensitiveContains(search)
                            || $0.recipient.localizedCaseInsensitiveContains(search)
                            || $0.text.localizedCaseInsensitiveContains(search)
                    }
                }
                // The legacy endpoint returns the latest matching message per participant.
                items = items.last.map { [$0] } ?? []
            }
            return try response(items)
        }
        return try await base.respond(request)
    }

    private var badge: Int { conversations().reduce(0) { $0 + $1.unreadCount } }
    private func conversations() -> [SMSConversation] {
        messages.compactMap { id, items -> SMSConversation? in
            guard let latest = items.last, let profile = latest.profileId else { return nil }
            let count = max(unread.contains(id) ? 1 : 0, items.filter { $0.incoming && $0.id > (watermarks[id] ?? 0) }.count)
            return SMSConversation(id: id, profileId: profile, participant: "10086", modemId: id == "conversation-history" ? "" : "device-1",
                lineLabel: profile == activeProfile ? "Home / 主线路" : "Travel / 旅行", lineNumber: "", readThroughMessageId: watermarks[id] ?? 0,
                markedUnread: unread.contains(id), unreadCount: count, latestMessage: latest, canSend: profile == activeProfile,
                lastMessageId: items.map(\.id).max())
        }.sorted { $0.latestMessage.id > $1.latestMessage.id }
    }
    private func response<Value: Encodable>(_ value: Value) throws -> BackendHTTPResponse {
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        return BackendHTTPResponse(data: try encoder.encode(value), status: 200)
    }
    private func response(status: Int) -> BackendHTTPResponse {
        if status < 400 { return BackendHTTPResponse(data: Data(), status: status) }
        return HTTPProblem.response(status: status, code: status == 404 ? "resource_not_found" : "remote_error", detail: "Fixture request rejected")
    }
}
#endif

import Foundation

// A demo connection owns all communication history. Device operations reuse the
// same in-memory implementation exercised by the device integration tests.
actor DemoBackend {
    let id: String
    private let devices: DetailFixtureServer
    private var settings: SettingsFixtureServer
    private var stopped = false
    private var contacts = [
        SigmoContact(id: "alice", name: "Alice", notes: "Demo contact", numbers: [ContactPhone(label: "mobile", number: "+12025550101", region: "US", key: "+12025550101")]),
        SigmoContact(id: "bob", name: "Bob", numbers: [ContactPhone(label: "mobile", number: "+12025550102", region: "US", key: "+12025550102")])
    ]
    private var avatars: [String: Data] = [:]
    private var messages: [DemoMessage] = []
    private var calls: [CallRecord] = []
    private var nextMessageID: Int64 = 1
    private var revision: Int64 = 1
    private var callSockets: [UUID: DetailFixtureBuffer] = [:]
    private var assignments: [String: String] = [:]
    private let physical: Bool

    init(id: UUID, physical: Bool = false) throws {
        self.id = id.uuidString
        self.physical = physical
        var connection = ConnectionFixture()
        connection.wifiRequiresSetup = true
        connection.wifi.emergencyAddressUpdateAvailable = true
        devices = DetailFixtureServer(physical: physical, scenario: .singleHardware, number: "+12025550100", initialConnection: connection)
        settings = try SettingsFixtureServer()
        settings.commit = "demo"
        settings.licenseDisplayName = "Demo User"
        settings.updateNotes = String(localized: "demo.updateNotes")
        settings.settings.values.auth["mode"] = .text("none")
        messages = [
            DemoMessage(id: 1, profileId: "8986000000000000001", number: "+12025550101", text: String(localized: "demo.messageWelcome"), incoming: true),
            DemoMessage(id: 2, profileId: "8986000000000000001", number: "+12025550101", text: String(localized: "demo.messageReply"), incoming: false),
            DemoMessage(id: 3, profileId: "8986000000000000002", number: "+12025550102", text: String(localized: "demo.messageTravel"), incoming: true)
        ]
        nextMessageID = 4
        calls = [Self.record(id: self.id, number: "+12025550101", incoming: false, state: "ended"),
                 Self.record(id: self.id, number: "+12025550102", incoming: true, state: "ended")]
    }

    private static func record(id: String, number: String, incoming: Bool, state: String, profile: String = "8986000000000000001", owner: String = "") -> CallRecord {
        let now = Date.now.ISO8601Format()
        return CallRecord(callID: UUID().uuidString, backendId: id, callUUID: UUID().uuidString,
                          modemId: "device-1", historyProfileId: profile, profileId: profile,
                          lineLabel: "Demo", ownerClientId: owner, route: "volte",
                          direction: incoming ? "incoming" : "outgoing", number: number, state: state,
                          hold: "none", reason: state == "ended" ? "normal" : "", startedAt: now,
                          answeredAt: incoming && state != "active" ? "" : now,
                          endedAt: state == "ended" ? now : "", updatedAt: now)
    }

    func stop() async {
        stopped = true
        callSockets.values.forEach { $0.close() }
        callSockets.removeAll()
        await devices.disconnectSessions()
    }

    func sampleMessage() async throws {
        let modem = try await currentModem()
        messages.append(DemoMessage(id: nextMessageID, profileId: modem.sim.identifier,
                                    number: "+12025550101", text: String(localized: "demo.messageIncoming"), incoming: true))
        nextMessageID += 1
        revision += 1
    }

    func sampleCall() async throws {
        guard !calls.contains(where: { !$0.terminal }) else { return }
        let modem = try await currentModem()
        try publish(Self.record(id: id, number: "+12025550101", incoming: true, state: "ringing", profile: modem.sim.identifier))
    }

    func socket(_ request: URLRequest) async throws -> BackendSocket {
        try check(request)
        if request.url?.path.hasSuffix("/calls/events") == true {
            let key = UUID(), buffer = DetailFixtureBuffer()
            callSockets[key] = buffer
            return BackendSocket(send: { _ in }, receive: { try await buffer.receive() }, close: {
                buffer.close()
                Task { await self.removeSocket(key) }
            })
        }
        let path = request.url?.path ?? ""
        guard path.contains("/esim-downloads/") || path.contains("/esim-transfers/") || path.contains("/sim-application") else {
            throw BackendFailure.local("demo.unsupportedRequest")
        }
        return try await devices.socket(request)
    }

    private func removeSocket(_ id: UUID) { callSockets[id] = nil }

    private func check(_ request: URLRequest) throws {
        try Task.checkCancellation()
        guard !stopped else { throw CancellationError() }
        guard request.url?.path.hasPrefix("/api/v1/") == true else { throw BackendFailure.local("demo.unsupportedRequest") }
    }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        try check(request)
        let path = String((request.url?.path ?? "").dropFirst("/api/v1/".count))
        let parts = path.split(separator: "/").map(String.init)
        let method = request.httpMethod ?? "GET"
        let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
        func value(_ name: String) -> String { query.first { $0.name == name }?.value ?? "" }

        if path == "license" { return try reply(DemoLicense(authorized: true, deviceId: "0123456789abcdef0123456789abcdef")) }
        if path == "app" { return try reply(settings.app) }
        if path == "capabilities" {
            return try reply(["features": ["contacts", "messageThreads", "historyAssignments", "callHistory", "nativeCalls", "wifiCalling", "volte", "esimTransfer"]])
        }
        if path == "auth/methods" { return raw(AuthenticationFixture.methods(required: settings.otpRequired)) }
        if path == "auth/otp/challenges" { return raw(AuthenticationFixture.challenge) }
        if path == "auth/sessions" {
            return AuthenticationFixture.validCode(request) ? raw(AuthenticationFixture.tokens("fixture-token")) : error("invalid_session", status: 401)
        }
        if path == "auth/sessions/current" { return empty() }
        if path == "auth/session-refreshes" { return raw(AuthenticationFixture.tokens("fixture-token")) }
        if settings.otpRequired && request.value(forHTTPHeaderField: "Authorization") != "Bearer fixture-token" { return error("invalid_session", status: 401) }
        if path == "settings/mcp/skills/sigmo-control", method == "GET" {
            return BackendHTTPResponse(data: try DemoResources.skillArchive(), status: 200)
        }
        if let response = try settings.respond(request) { return response }

        if parts.first == "contacts" { return try contactRequest(request, parts: parts) }
        if path == "modems" { return try await reply([currentModem()]) }
        if path == "sim-profiles" { return try await reply(DemoItems(items: lines())) }
        if path == "communication-preferences" {
            let profile = messages.last { $0.participant == value("participant") }?.profileId ?? ""
            return try reply(["participant": value("participant"), "profileId": profile, "updatedAt": ""])
        }
        if parts.first == "sim-profiles", parts.last == "history-assignment", parts.count == 3 {
            let target = try decode([String: String].self, request)["targetProfileId"] ?? ""
            guard try await lines().contains(where: { $0.profileId == target }) else { return error("communication_not_found", status: 404) }
            assignments[parts[1]] = target
            for index in messages.indices where messages[index].historyProfileId == parts[1] { messages[index].historyProfileId = target }
            for index in calls.indices where calls[index].historyProfileId == parts[1] { calls[index].historyProfileId = target }
            revision += 1
            return empty()
        }
        if parts.first == "conversations" || parts.first == "message-threads" {
            return try await messageRequest(request, parts: parts, query: query)
        }
        if path == "calls", method == "GET" {
            let items = calls.filter { call in
                call.terminal && (value("profileId").isEmpty || call.historyProfileId == value("profileId"))
                    && (value("missed") != "true" || call.missed)
                    && (value("q").isEmpty || call.number.localizedCaseInsensitiveContains(value("q")))
            }
            return try reply(DemoPage(items: value("cursor").isEmpty ? items : []))
        }
        if parts.first == "calls", parts.count == 2, method == "DELETE" {
            calls.removeAll { $0.id == parts[1] && $0.terminal }
            return empty()
        }
        if parts.first == "modems", parts.count >= 3 {
            guard parts[1] == "device-1" else { return error("modem_not_found", status: 404) }
            if parts[2] == "messages" { return try await sendMessage(request) }
            if parts[2] == "calls" { return try await callRequest(request, parts: Array(parts.dropFirst(2))) }
            if parts[2] == "ussd" { return try reply(["reply": String(localized: "demo.ussdReply")]) }
        }
        if path == "push-subscriptions" {
            return try reply(DemoPush(backendId: id, revision: revision, unreadCount: unreadCount, subscriptions: []))
        }
        return try await devices.respond(request)
    }

    private func currentModem() async throws -> Modem {
        let response = try await devices.respond(URLRequest(url: URL(string: "https://demo.invalid/api/v1/modems/device-1")!))
        return try JSONDecoder().decode(Modem.self, from: response.data)
    }

    private func lines() async throws -> [CommunicationLine] {
        let modem = try await currentModem()
        let response = try await devices.respond(URLRequest(url: URL(string: "https://demo.invalid/api/v1/modems/device-1/esims")!))
        let profiles = try JSONDecoder().decode(ESIMProfiles.self, from: response.data).ses.flatMap(\.profiles)
        var labels = Dictionary(profiles.map { ($0.iccid, $0.name) }, uniquingKeysWith: { first, _ in first })
        labels[modem.sim.identifier] = labels[modem.sim.identifier] ?? "Demo SIM"
        for message in messages where labels[message.historyProfileId] == nil { labels[message.historyProfileId] = "History" }
        return labels.keys.sorted().map { profile in
            let active = profile == modem.sim.identifier
            let present = active || (!physical && profiles.contains { $0.iccid == profile })
            return CommunicationLine(profileId: profile, modemId: "device-1", label: labels[profile] ?? "Demo", number: active ? modem.number : "",
                                     region: "US", kind: physical ? "physical" : "esim", seId: "se-a", deletionState: present ? "" : "deleted",
                                     successorId: assignments[profile] ?? "", messageCount: messages.filter { $0.historyProfileId == profile }.count,
                                     callCount: calls.filter { $0.historyProfileId == profile }.count, present: present, active: active,
                                     canMessage: active && !modem.airplaneMode, canCall: active && !modem.airplaneMode)
        }
    }

    private func contactRequest(_ request: URLRequest, parts: [String]) throws -> BackendHTTPResponse {
        let method = request.httpMethod ?? "GET"
        if parts.count == 1 {
            if method == "GET" { return try reply(DemoItems(items: contacts)) }
            guard method == "POST" else { return error("method_not_allowed", status: 405) }
            var contact = try decode(SigmoContact.self, request)
            contact.id = UUID().uuidString
            contacts.append(contact)
            return try reply(contact, status: 201)
        }
        guard let index = contacts.firstIndex(where: { $0.id == parts[1] }) else { return error("communication_not_found", status: 404) }
        if parts.last == "avatar" {
            if method == "GET" { return BackendHTTPResponse(data: avatars[parts[1]] ?? Data(), status: avatars[parts[1]] == nil ? 404 : 200) }
            avatars[parts[1]] = method == "DELETE" ? nil : request.httpBody
            contacts[index].hasAvatar = avatars[parts[1]] != nil
        } else if method == "DELETE" {
            avatars[parts[1]] = nil
            contacts.remove(at: index)
            return empty()
        } else if method == "PUT" {
            var contact = try decode(SigmoContact.self, request)
            contact.id = parts[1]
            contacts[index] = contact
        }
        return try reply(contacts[index])
    }

    private var unreadCount: Int { messages.filter { $0.incoming && !$0.read }.count }
    private var badge: MessageBadge { MessageBadge(backendId: id, revision: revision, unreadCount: unreadCount) }
    private func decode<Value: Decodable>(_ type: Value.Type, _ request: URLRequest) throws -> Value {
        try JSONDecoder().decode(type, from: request.httpBody ?? Data())
    }
    private func reply<Value: Encodable>(_ value: Value, status: Int = 200) throws -> BackendHTTPResponse {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return BackendHTTPResponse(data: try encoder.encode(value), status: status)
    }
    private func raw(_ text: String) -> BackendHTTPResponse { BackendHTTPResponse(data: Data(text.utf8), status: 200) }
    private func empty() -> BackendHTTPResponse { BackendHTTPResponse(data: Data(), status: 204) }
    private func error(_ code: String, status: Int) -> BackendHTTPResponse { HTTPProblem.response(status: status, code: code, detail: "Demo request rejected") }
}

private struct DemoLicense: Encodable { let authorized: Bool; let deviceId: String }
private struct DemoItems<Item: Encodable>: Encodable { let items: [Item] }
private struct DemoPage<Item: Encodable>: Encodable { let items: [Item]; var nextCursor = "" }
private struct DemoPush: Encodable { let backendId: String; let revision: Int64; let unreadCount: Int; let subscriptions: [NativeSubscription] }

private struct DemoMessage: Encodable {
    let id: Int64
    var profileId: String
    var historyProfileId: String
    let modemId = "device-1"
    let sender: String
    let recipient: String
    let text: String
    let timestamp: String
    let incoming: Bool
    var read: Bool
    let status: String
    let lineLabel = "Demo"
    let lineNumber = "+12025550100"
    var participant: String { incoming ? sender : recipient }
    let conversationId: String
    var threadID: String { participant }

    init(id: Int64, profileId: String, number: String, text: String, incoming: Bool) {
        self.id = id; self.profileId = profileId; historyProfileId = profileId
        conversationId = profileId + ":" + number
        sender = incoming ? number : "+12025550100"
        recipient = incoming ? "+12025550100" : number
        self.text = text; self.incoming = incoming; read = !incoming
        timestamp = Date.now.ISO8601Format()
        status = incoming ? "received" : "sent"
    }

    var sms: SMSMessage {
        SMSMessage(id: id, conversationId: conversationId, profileId: profileId, sender: sender,
                   recipient: recipient, text: text, timestamp: CallRecord.date(timestamp) ?? .now,
                   status: incoming ? "received-unread" : "stored-sent", incoming: incoming)
    }
}

private struct DemoThread: Encodable {
    let id, participant: String
    let latestMessage: DemoMessage
    let unreadCount: Int
    var markedUnread = false
}

extension DemoBackend {
    private func messageRequest(_ request: URLRequest, parts: [String], query: [URLQueryItem]) async throws -> BackendHTTPResponse {
        let unified = parts[0] == "message-threads"
        let method = request.httpMethod ?? "GET"
        func value(_ key: String) -> String { query.first { $0.name == key }?.value ?? "" }
        let modem = try await currentModem()
        if parts.count == 1 {
            let search = value("q")
            let filtered = messages.filter {
                (value("profileId").isEmpty || $0.historyProfileId == value("profileId")) &&
                (search.isEmpty || $0.participant.localizedCaseInsensitiveContains(search) || $0.text.localizedCaseInsensitiveContains(search))
            }
            if unified {
                let groups = Dictionary(grouping: filtered, by: \.threadID)
                let threads = groups.compactMap { key, values -> DemoThread? in
                    guard let last = values.max(by: { $0.id < $1.id }) else { return nil }
                    return DemoThread(id: key, participant: last.participant, latestMessage: last, unreadCount: values.filter { $0.incoming && !$0.read }.count)
                }.sorted { $0.latestMessage.id > $1.latestMessage.id }
                return try reply(DemoPage(items: value("cursor").isEmpty ? threads : []))
            }
            let groups = Dictionary(grouping: filtered, by: \.conversationId)
            let conversations = groups.compactMap { key, values -> SMSConversation? in
                guard value("unassigned") != "true", let last = values.max(by: { $0.id < $1.id }) else { return nil }
                return SMSConversation(id: key, profileId: last.profileId, participant: last.participant, modemId: last.modemId,
                                       lineLabel: last.lineLabel, lineNumber: last.lineNumber,
                                       readThroughMessageId: values.filter(\.read).map(\.id).max() ?? 0,
                                       markedUnread: false, unreadCount: values.filter { $0.incoming && !$0.read }.count,
                                       latestMessage: last.sms, canSend: last.profileId == modem.sim.identifier,
                                       lastMessageId: last.id)
            }.sorted { $0.latestMessage.id > $1.latestMessage.id }
            struct Snapshot: Encodable { let backendId: String; let revision: Int64; let unreadCount: Int; let items: [SMSConversation] }
            return try reply(Snapshot(backendId: id, revision: revision, unreadCount: unreadCount, items: conversations))
        }
        let key = parts[1]
        let matching: (DemoMessage) -> Bool = { unified ? $0.threadID == key : $0.conversationId == key }
        guard messages.contains(where: matching) else { return error("communication_not_found", status: 404) }
        if method == "DELETE" { messages.removeAll(where: matching); revision += 1; return empty() }
        if method == "PATCH" {
            struct Read: Decodable { let messageIds: [Int64]?; let readThroughMessageId: Int64?; let markedUnread: Bool? }
            let update = try decode(Read.self, request)
            for i in messages.indices where matching(messages[i]) {
                if update.messageIds?.contains(messages[i].id) == true || messages[i].id <= (update.readThroughMessageId ?? -1) {
                    messages[i].read = true
                }
                if let unread = update.markedUnread { messages[i].read = !unread }
            }
            revision += 1
            return try reply(badge)
        }
        let selected = messages.filter(matching).sorted { $0.id < $1.id }
        if !unified {
            let cursor = value("cursor").split(separator: ":").compactMap { Int64($0) }
            let snapshot = cursor.first ?? (nextMessageID - 1)
            let before = cursor.count == 2 ? cursor[1] : Int64.max
            let eligible = selected.filter { $0.id <= snapshot && $0.id < before }
            let items = Array(eligible.suffix(50)).map(\.sms)
            let next = eligible.count > 50 ? "\(snapshot):\(items.first!.id)" : ""
            return try reply(ConversationMessagePage(backendId: id, revision: revision, unreadCount: unreadCount,
                conversation: ConversationMetadata(id: key, readThroughMessageId: selected.filter(\.read).map(\.id).max() ?? 0,
                    markedUnread: false, unreadCount: selected.filter { $0.incoming && !$0.read }.count), items: items, nextCursor: next))
        }
        return try reply(DemoPage(items: value("cursor").isEmpty ? selected : []))
    }

    private func sendMessage(_ request: URLRequest) async throws -> BackendHTTPResponse {
        guard request.httpMethod == "POST" else { return error("method_not_allowed", status: 405) }
        struct Submission: Decodable { let to, text, expectedProfileId: String }
        let value = try decode(Submission.self, request)
        let modem = try await currentModem()
        guard value.expectedProfileId == modem.sim.identifier else { return error("message_profile_changed", status: 409) }
        guard !modem.airplaneMode else { return error("conflict", status: 409) }
        let message = DemoMessage(id: nextMessageID, profileId: modem.sim.identifier,
                                  number: value.to, text: value.text, incoming: false)
        messages.append(message)
        nextMessageID += 1; revision += 1
        return try reply(["to": value.to, "conversationId": message.conversationId])
    }

    private func callRequest(_ request: URLRequest, parts: [String]) async throws -> BackendHTTPResponse {
        let method = request.httpMethod ?? "GET"
        if parts.count == 1 {
            if method == "GET" { return try reply(calls) }
            struct Dial: Decodable { let to, clientId, expectedProfileId: String }
            let dial = try decode(Dial.self, request)
            let modem = try await currentModem()
            guard modem.sim.identifier == dial.expectedProfileId else { return error("call_profile_changed", status: 409) }
            guard !modem.airplaneMode, !calls.contains(where: { !$0.terminal }) else { return error("conflict", status: 409) }
            let record = Self.record(id: id, number: dial.to, incoming: false, state: "active", profile: modem.sim.identifier, owner: dial.clientId)
            try publish(record)
            return try reply(record, status: 201)
        }
        guard let index = calls.firstIndex(where: { $0.id == parts[1] }) else { return error("call_not_found", status: 404) }
        if method == "GET" { return try reply(calls[index]) }
        if method == "DELETE", calls[index].terminal { calls.remove(at: index); return empty() }
        if parts.last == "dtmf-events" { return empty() }
        struct Update: Decodable { let clientId: String; let state, hold: String? }
        let update = try decode(Update.self, request)
        var record = calls[index]
        if let state = update.state {
            record.state = state
            if state == "active" { record.answeredAt = Date.now.ISO8601Format(); record.ownerClientId = update.clientId }
            if state == "ended" { record.endedAt = Date.now.ISO8601Format(); record.reason = "normal" }
        }
        if let hold = update.hold { record.hold = hold }
        record.updatedAt = Date.now.ISO8601Format()
        try publish(record)
        return try reply(record)
    }

    private func publish(_ record: CallRecord) throws {
        guard !stopped else { throw CancellationError() }
        calls.removeAll { $0.id == record.id }
        calls.insert(record, at: 0)
        struct Event: Encodable { let type = "call"; let call: CallRecord }
        let data = try JSONEncoder().encode(Event(call: record))
        callSockets.values.forEach { $0.push(data) }
    }
}

#if DEBUG
import Foundation

// All communication UI tests use this in-memory backend, including mutations.
actor CommunicationFixture {
    enum HistoryAssignmentScenario: String {
        case empty = "communication-assignment-0"
        case single = "communication-assignment-1"
        case multiple = "communication-assignment-2"
        case retry = "communication-assignment-retry"
        case noHistory = "communication-assignment-no-history"
    }

    private let devices: DetailFixtureServer
    private var contacts = [SigmoContact(id: "alice", name: "Alice", notes: "Fixture note", numbers: [ContactPhone(label: "mobile", number: "+14155552671", region: "US", key: "+14155552671"), ContactPhone(label: "work", number: "+442079460018", region: "GB", key: "+442079460018")])]
    private var modemOverrides: [Modem]?
    private var avatars: [String: Data] = [:]
    private var held: CheckedContinuation<Void, Never>?
    private var arrival: CheckedContinuation<Void, Never>?
    private var heldPath: String?
    private let features = ["contacts", "messageThreads", "historyAssignments", "callHistory", "nativeCalls"]
    private var directoryUnavailable = false
    private var assignmentStatus = 204
    private var failAssignmentRead = false
    private var requests: [URLRequest] = []
    private var lines = [CommunicationFixture.line("a"), CommunicationFixture.line("b", available: false)]
    private let includesHistory: Bool
    init(contactName: String = "Alice", includesHistory: Bool = false, modemContacts: Bool = false, modemNames: [String: String] = [:], historyAssignment: HistoryAssignmentScenario? = nil) {
        devices = DetailFixtureServer(modemNames: modemNames)
        contacts[0].name = contactName
        self.includesHistory = includesHistory
        if modemContacts {
            lines = [Self.line("8986000000000000001"), Self.line("a", modemID: "device-2")]
        }
        if let historyAssignment {
            failAssignmentRead = historyAssignment == .retry
            lines = [
                Self.line("deleted", available: false, label: "Travel", deletionState: "deleted", present: false,
                          messageCount: historyAssignment == .noHistory ? 0 : 2),
                Self.line("empty-deleted", available: false, deletionState: "deleted", present: false),
                Self.line("foreign", modemID: "device-2", label: "Other Modem Line")
            ]
            if historyAssignment != .empty { lines.append(Self.line("work", label: "Work")) }
            if historyAssignment == .multiple { lines.append(Self.line("backup", available: false, label: "Backup")) }
        }
    }
    func setModems(_ value: [Modem]) { modemOverrides = value }
    func setContacts(_ value: [SigmoContact]) { contacts = value }
    func setLines(_ value: [CommunicationLine]) { lines = value }
    func setDirectoryUnavailable(_ value: Bool) { directoryUnavailable = value }
    func setAssignmentStatus(_ value: Int) { assignmentStatus = value }
    func holdNextRead(_ path: String = "/contacts") { heldPath = path }
    private func holdIfNeeded(_ path: String) async {
        guard path == heldPath else { return }
        heldPath = nil
        await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
    }
    func waitForRead() async { if held == nil { await withCheckedContinuation { arrival = $0 } } }
    func releaseRead() { held?.resume(); held = nil }
    func allRequests() -> [URLRequest] { requests }
    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        let path = request.url?.path ?? "", method = request.httpMethod ?? "GET"
        if let modemOverrides {
            if path.hasSuffix("/modems") { return try reply(modemOverrides) }
            if let id = path.components(separatedBy: "/modems/").last, !id.contains("/"), path.contains("/modems/") {
                if let modem = modemOverrides.first(where: { $0.id == id }) { return try reply(modem) }
                return HTTPProblem.response(status: 404, code: "modem_not_found", detail: "Missing modem")
            }
            if path.hasSuffix("/messages"), method == "POST" { return raw(#"{"to":"10086","conversationId":"sent"}"#) }
        }
        if path.hasSuffix("/auth/methods") { return raw(#"{"version":1,"mode":"none","methods":[]}"#) }
        if path.hasSuffix("/capabilities") { return try reply(["features": features]) }
        if path.hasSuffix("/contacts") {
            if method == "GET" {
                let response = try reply(["items": contacts])
                await holdIfNeeded("/contacts")
                return response
            }
            var contact = try JSONDecoder().decode(SigmoContact.self, from: request.httpBody ?? Data())
            contact.id = UUID().uuidString; contacts.append(contact)
            return try reply(contact)
        }
        if let part = path.range(of: "/contacts/") {
            let components = path[part.upperBound...].split(separator: "/").map(String.init)
            guard let id = components.first, let index = contacts.firstIndex(where: { $0.id == id }) else { return HTTPProblem.response(status: 404, code: "communication_not_found", detail: "Missing fixture") }
            if components.last == "avatar" {
                if method == "GET" { return BackendHTTPResponse(data: avatars[id] ?? Data(), status: avatars[id] == nil ? 404 : 200) }
                avatars[id] = method == "DELETE" ? nil : request.httpBody
                contacts[index].hasAvatar = avatars[id] != nil
                return try reply(contacts[index])
            }
            if method == "DELETE" { contacts.remove(at: index); return BackendHTTPResponse(data: Data(), status: 204) }
            if method == "PUT" { contacts[index] = try JSONDecoder().decode(SigmoContact.self, from: request.httpBody ?? Data()) }
            return try reply(contacts[index])
        }
        if path.hasSuffix("/sim-profiles") {
            if directoryUnavailable { throw URLError(.cannotConnectToHost) }
            if failAssignmentRead, request.url?.query == nil {
                failAssignmentRead = false
                return HTTPProblem.response(status: 503, code: "remote_error", detail: "Inventory unavailable")
            }
            let response = try reply(["items": lines])
            await holdIfNeeded("/sim-profiles")
            return response
        }
        if path.hasSuffix("/communication-preferences") { return raw(#"{"participant":"10086","profileId":"a","updatedAt":""}"#) }
        if path.hasSuffix("/history-assignment") {
            if assignmentStatus >= 400 {
                return HTTPProblem.response(status: assignmentStatus, code: "remote_error", detail: "Assignment unavailable")
            }
            struct Assignment: Decodable { let targetProfileId: String }
            let assignment = try JSONDecoder().decode(Assignment.self, from: request.httpBody ?? Data())
            let sourceID = request.url?.deletingLastPathComponent().lastPathComponent ?? ""
            guard let index = lines.firstIndex(where: { $0.id == sourceID && $0.deletionState == "deleted" }),
                  lines.contains(where: { $0.id == assignment.targetProfileId && $0.present && $0.deletionState.isEmpty }) else {
                return HTTPProblem.response(status: 400, code: "history_assignment_invalid", detail: "Invalid assignment")
            }
            let source = lines[index]
            lines[index] = CommunicationLine(
                profileId: source.profileId, modemId: source.modemId, label: source.label, number: source.number,
                region: source.region, kind: source.kind, seId: source.seId, deletionState: "assigned",
                successorId: assignment.targetProfileId, messageCount: source.messageCount, callCount: source.callCount,
                present: source.present, active: source.active, canMessage: source.canMessage, canCall: source.canCall
            )
            return BackendHTTPResponse(data: Data(), status: assignmentStatus)
        }
        if includesHistory {
            let message = #"{"id":1,"conversationId":"source-conversation","profileId":"a","historyProfileId":"a","modemId":"device-1","sender":"+14155552671","recipient":"+14155550000","text":"Fixture history message","timestamp":"2026-10-13T08:00:00Z","incoming":true,"read":true,"status":"received","lineLabel":"Home","lineNumber":"+14155550000"}"#
            if path.hasSuffix("/message-threads") {
                return raw(#"{"items":[{"id":"archive-thread","participant":"+14155552671","latestMessage":\#(message),"unreadCount":0,"markedUnread":false}],"nextCursor":""}"#)
            }
            if path.hasSuffix("/message-threads/archive-thread/messages") {
                return raw(#"{"items":[\#(message)],"nextCursor":""}"#)
            }
            if path.hasSuffix("/calls") {
                return raw(#"{"items":[{"callID":"archive-call","profileId":"a","historyProfileId":"a","modemId":"device-1","number":"+14155552671","route":"wifi_calling","direction":"incoming","state":"ended","reason":"normal","startedAt":"2026-10-13T08:00:00Z","answeredAt":"2026-10-13T08:00:05Z","endedAt":"2026-10-13T08:01:05Z","updatedAt":"2026-10-13T08:01:05Z"}],"nextCursor":""}"#)
            }
        }
        if path.hasSuffix("/message-threads") || path.hasSuffix("/calls") { return raw(#"{"items":[],"nextCursor":""}"#) }
        return try await devices.respond(request)
    }
    static func line(_ id: String, available: Bool = true, modemID: String = "device-1", label: String? = nil, deletionState: String = "", present: Bool = true, messageCount: Int = 0, callCount: Int = 0) -> CommunicationLine {
        CommunicationLine(profileId: id, modemId: modemID, label: label ?? id, number: "+14155550000", region: "US", kind: "esim", seId: "se-a", deletionState: deletionState, successorId: "", messageCount: messageCount, callCount: callCount, present: present, active: available, canMessage: available, canCall: available)
    }
    private func raw(_ text: String) -> BackendHTTPResponse { .init(data: Data(text.utf8), status: 200) }
    private func reply<Value: Encodable>(_ value: Value) throws -> BackendHTTPResponse { .init(data: try JSONEncoder().encode(value), status: 200) }
}
#endif

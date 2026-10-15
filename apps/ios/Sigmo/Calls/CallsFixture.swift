#if DEBUG
import Foundation

// This fixture owns only memory and local sockets. UI tests never dial a modem.
actor CallsFixture {
    private let base = PreviewBackendServer()
    private let messages = MessagesFixture()
    private var records: [CallRecord]
    private var sockets: [DetailFixtureBuffer] = []
    private var lostDial = false
    private var lostUpdate = false
    private var endFailure = false
    private var ussdTimeout = false
    private var activeProfile = CallsFixture.profile
    private var callReadFailure: BackendHTTPResponse?
    private var pagedHistory: [CallRecord]?
    private(set) var requests: [URLRequest] = []
    private(set) var digits: [String] = []
    static let profile = "8986000000000000000"
    static let backend = "fixture-call-backend"

    @MainActor static func emitRecordingAudio(to audio: CallAudio) {
        guard ProcessInfo.processInfo.environment["SIGMO_UI_RECORDINGS"] == "1" else { return }
        // A local tone exercises capture, resampling, saving and playback in UI
        // tests. Keep fixture data and launch switches out of the audio pipeline.
        let samples = (0..<240_000).map { Float(sin(2 * .pi * 440 * Double($0) / 8_000)) * 0.1 }
        audio.emitFixturePCM(CallPCM(samples: samples, sampleRate: 8_000,
            time: ProcessInfo.processInfo.systemUptime + 30), remote: true)
    }

    init(incoming: Bool = false) {
        var missed = Self.record("missed-call", state: "ended", direction: "incoming")
        missed.answeredAt = ""
        records = [Self.record("past-call", state: "ended"), missed]
        if incoming { records.insert(Self.record("incoming-call", state: "ringing", direction: "incoming", owner: ""), at: 0) }
    }

    static func record(_ id: String = "test-call", state: String = "active", direction: String = "outgoing", owner: String = "fixture-call-client") -> CallRecord {
        let now = Date.now
        return CallRecord(callID: id, backendId: backend, callUUID: UUID().uuidString, profileId: profile, ownerClientId: owner,
            route: "wifi_calling", direction: direction, number: "10086", state: state, hold: "none", reason: state == "ended" ? "normal" : "",
            startedAt: now.addingTimeInterval(-40).ISO8601Format(),
            answeredAt: ["ringing", "dialing"].contains(state) ? "" : now.addingTimeInterval(-35).ISO8601Format(),
            endedAt: state == "ended" ? now.ISO8601Format() : "", updatedAt: now.ISO8601Format())
    }

    func configure(lostDial: Bool = false, endFailure: Bool = false, ussdTimeout: Bool = false, lostUpdate: Bool = false) {
        self.lostDial = lostDial; self.endFailure = endFailure; self.ussdTimeout = ussdTimeout
        self.lostUpdate = lostUpdate
    }

    func changeSIM(to profile: String) { activeProfile = profile }
    func setCallReadFailure(_ failure: BackendHTTPResponse?) { callReadFailure = failure }
    func setPagedHistory(_ records: [CallRecord]) { pagedHistory = records.sorted { $0.date > $1.date } }

    func publish(_ record: CallRecord) throws {
        records.removeAll { $0.id == record.id }; records.insert(record, at: 0)
        struct Event: Encodable { let type = "call"; let call: CallRecord }
        let data = try JSONEncoder().encode(Event(call: record))
        for socket in sockets { socket.push(data) }
    }

    func count(method: String, suffix: String) -> Int { requests.filter { $0.httpMethod == method && $0.url?.path.hasSuffix(suffix) == true }.count }

    func socket(_ request: URLRequest) -> BackendSocket {
        let buffer = DetailFixtureBuffer()
        sockets.append(buffer)
        return BackendSocket(send: { _ in }, receive: { try await buffer.receive() }, close: { buffer.close() })
    }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        let path = request.url?.path ?? "", method = request.httpMethod ?? "GET"
        if path.contains("/messages") || path.contains("/conversations") {
            return try await messages.respond(request)
        }
        let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: String] } ?? [:]
        if path.hasSuffix("/capabilities") {
            return pagedHistory == nil ? response(#"{"features":["nativeCalls","wifiCalling","volte"]}"#)
                : response(#"{"features":["nativeCalls","wifiCalling","volte","callHistory"]}"#)
        }
        if path.hasSuffix("/api/v1/calls"), method == "GET", let pagedHistory {
            let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
            func value(_ name: String) -> String { query.first { $0.name == name }?.value ?? "" }
            let matching = pagedHistory.filter { (value("missed") != "true" || $0.missed)
                && (value("q").isEmpty || $0.number.contains(value("q"))) }
            let offset = Int(value("cursor")) ?? 0
            let items = Array(matching.dropFirst(offset).prefix(50))
            struct Page: Encodable { let items: [CallRecord]; let nextCursor: String }
            return try encoded(Page(items: items, nextCursor: offset + items.count < matching.count ? String(offset + items.count) : ""))
        }
        if path.hasSuffix("/ussd") {
            if ussdTimeout { return HTTPProblem.response(status: 408, code: "ussd_timeout", detail: "Timed out") }
            return response(body["action"] == "reply" ? #"{"reply":"Balance: 100.00 / 余额 100.00"}"# : #"{"reply":"1. Balance / 余额\n2. Usage / 用量"}"#)
        }
        if path.hasSuffix("/calls") {
            if method == "GET" { return try encoded(records) }
            if method == "POST" {
                guard body["expectedProfileId"] == activeProfile else { return HTTPProblem.response(status: 409, code: "call_profile_changed", detail: "SIM changed") }
                var record = Self.record("dialed-call", owner: body["clientId"] ?? "")
                record.profileId = activeProfile
                record.number = body["to"] ?? ""
                try publish(record)
                if lostDial { throw URLError(.networkConnectionLost) }
                return try encoded(record, status: 201)
            }
        }
        if path.contains("/calls/") {
            let pieces = path.components(separatedBy: "/calls/")
            let id = pieces.last?.components(separatedBy: "/").first ?? ""
            guard let index = records.firstIndex(where: { $0.id == id }) else { return HTTPProblem.response(status: 404, code: "call_not_found", detail: "Not found") }
            var record = records[index]
            if method == "GET" {
                if let callReadFailure { return callReadFailure }
                return try encoded(record)
            }
            if method == "DELETE", record.terminal { records.remove(at: index); return response("", status: 204) }
            guard record.ownerClientId?.isEmpty != false || record.ownerClientId == body["clientId"] else {
                return HTTPProblem.response(status: 409, code: "call_in_use", detail: "In use")
            }
            if path.hasSuffix("/dtmf-events") { digits.append(body["digits"] ?? ""); return response("", status: 204) }
            if body["state"] == "ended", endFailure { throw URLError(.networkConnectionLost) }
            if let state = body["state"] {
                record.state = state
                if state == "active" { record.ownerClientId = body["clientId"]; record.answeredAt = Date.now.ISO8601Format() }
                if state == "ended" { record.endedAt = Date.now.ISO8601Format() }
            }
            if let hold = body["hold"] { record.hold = hold }
            record.updatedAt = Date.now.ISO8601Format()
            try publish(record)
            if lostUpdate { throw URLError(.networkConnectionLost) }
            return try encoded(record)
        }
        let result = try await base.respond(request)
        if path.hasSuffix("/modems") || path.hasSuffix("/modems/device-1"),
           let text = String(data: result.data, encoding: .utf8) {
            return BackendHTTPResponse(data: Data(text.replacingOccurrences(of: Self.profile, with: activeProfile).utf8), status: result.status)
        }
        return result
    }

    private func encoded<T: Encodable>(_ value: T, status: Int = 200) throws -> BackendHTTPResponse {
        BackendHTTPResponse(data: try JSONEncoder().encode(value), status: status)
    }
    private func response(_ body: String, status: Int = 200) -> BackendHTTPResponse { BackendHTTPResponse(data: Data(body.utf8), status: status) }
}
#endif

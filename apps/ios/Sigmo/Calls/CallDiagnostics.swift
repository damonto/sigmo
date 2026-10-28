import Foundation

/// Records call lifecycle metadata without numbers, client identities or signaling payloads.
struct CallDiagnostics: Sendable {
    let store: DiagnosticLogStore

    func record(_ event: String, id: UUID? = nil, category: String = "calls",
                fields: [String: String] = [:], error: (any Error)? = nil) {
        guard let generation = store.generation else { return }
        var fields = fields
        fields["callUUID"] = id?.uuidString
        if let error { fields.merge(DiagnosticError.fields(error)) { _, new in new } }
        store.record(event, category: category,
                     level: error.map { DiagnosticError.isCancellation($0) ? .info : .error } ?? .info,
                     fields: fields, generation: generation)
    }

    static func validationIssues(content: PushContent?, id: UUID, connection: BackendConfiguration?,
                                 ended: Bool, now: Date = .now) -> [String] {
        guard let content else { return ["missing_content"] }
        var issues: [String] = []
        if content.callUUID.flatMap(UUID.init(uuidString:)) != id { issues.append("uuid_mismatch") }
        if content.kind != "call" { issues.append("wrong_kind") }
        if ended { issues.append("already_ended") }
        if (content.expiresAt ?? 0) <= Int64(now.timeIntervalSince1970) { issues.append("expired") }
        if connection == nil { issues.append("missing_connection") }
        if content.callId?.isEmpty != false { issues.append("missing_call_id") }
        if content.modemId?.isEmpty != false { issues.append("missing_modem_id") }
        if content.profileId?.isEmpty != false { issues.append("missing_profile_id") }
        return issues
    }

    static func snapshot(_ record: CallRecord, clientID: String) -> [String: String] {
        // Backend state/reason fields are strings and may contain arbitrary error
        // text. Only known values and parsed timestamps belong in diagnostics.
        var fields = [
            "state": known(record.state, ["dialing", "ringing", "answering", "early_media", "active", "confirmed", "ending", "ended", "failed"]),
            "direction": known(record.direction, ["incoming", "outgoing"]),
            "route": known(record.route, ["auto", "wifi_calling", "volte", "modem"]),
            "hold": known(record.hold ?? "", ["", "none", "local", "remote", "local_remote"]),
            "backendReason": known(record.reason, ["", "busy", "rejected", "canceled", "cancelled", "no_answer", "normal", "remote", "failed", "timeout", "hangup", "server_restarted", "call media is not available"]),
            "owner": record.ownerClientId?.isEmpty != false ? "none" : record.ownerClientId == clientID ? "self" : "other"
        ]
        fields["recordCallUUID"] = record.callUUID.flatMap(UUID.init(uuidString:))?.uuidString
        for (key, value) in [("startedAt", record.startedAt), ("answeredAt", record.answeredAt),
                             ("endedAt", record.endedAt), ("updatedAt", record.updatedAt)] {
            if let date = CallRecord.date(value) { fields[key] = date.ISO8601Format() }
        }
        return fields
    }

    static func ending(_ reason: CallEndReason) -> [String: String] {
        let name: String
        switch reason {
        case .failed: name = "failed"
        case .remoteEnded: name = "remote_ended"
        case .unanswered: name = "unanswered"
        case .answeredElsewhere: name = "answered_elsewhere"
        case .declinedElsewhere: name = "declined_elsewhere"
        }
        return ["reason": String(reason.rawValue), "endReason": name]
    }

    static func durationMS(since start: ContinuousClock.Instant) -> String {
        let duration = start.duration(to: .now).components
        return String(duration.seconds * 1000 + duration.attoseconds / 1_000_000_000_000_000)
    }

    private static func known(_ value: String, _ allowed: Set<String>) -> String {
        allowed.contains(value) ? value : "other"
    }
}

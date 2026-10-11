import Foundation

struct CallRecord: Codable, Hashable, Identifiable, Sendable {
    let callID: String
    var backendId: String?
    var callUUID: String?
    var profileId: String?
    var ownerClientId: String?
    var route: String
    var direction: String
    var number: String
    var state: String
    var hold: String?
    var reason: String
    var startedAt: String
    var answeredAt: String
    var endedAt: String
    var updatedAt: String

    var id: String { callID }
    var terminal: Bool { state == "ended" || state == "failed" }
    var incoming: Bool { direction == "incoming" && (state == "ringing" || state == "answering") }
    var connected: Bool { state == "active" || state == "confirmed" }
    var hasMedia: Bool { connected || state == "early_media" }
    var localHold: Bool { hold == "local" || hold == "local_remote" }
    var remoteHold: Bool { hold == "remote" || hold == "local_remote" }
    var missed: Bool { direction == "incoming" && terminal && answeredAt.isEmpty }
    var canSendDTMF: Bool { hasMedia && !localHold && !remoteHold }
    var stateLabel: String { NSLocalizedString("calls.state.\(state)", value: state, comment: "") }
    var routeLabel: String { NSLocalizedString("calls.route.\(route)", value: route, comment: "") }
    var reasonLabel: String { NSLocalizedString("calls.reason.\(reason)", value: reason, comment: "") }
    var date: Date { Self.date(startedAt) ?? .distantPast }

    static func date(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: value) { return date }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: value)
    }

    func duration(at now: Date = .now) -> String {
        guard let start = Self.date(answeredAt) else { return "" }
        let end = Self.date(endedAt) ?? (terminal ? Self.date(updatedAt) ?? now : now)
        let seconds = max(0, Int(end.timeIntervalSince(start)))
        if seconds >= 3600 { return String(format: "%d:%02d:%02d", seconds / 3600, seconds / 60 % 60, seconds % 60) }
        return String(format: "%d:%02d", seconds / 60, seconds % 60)
    }

    func supersedes(_ previous: CallRecord) -> Bool {
        if previous.terminal && !terminal { return false }
        return (Self.date(updatedAt) ?? .distantPast) >= (Self.date(previous.updatedAt) ?? .distantPast)
    }
}

struct CallLine: Codable, Hashable, Sendable, Identifiable {
    let backendID: UUID
    let modemID: String
    let profileID: String
    let backendName: String
    let label: String
    let region: String
    let address: String
    var id: String { "\(backendID):\(modemID):\(profileID)" }

    func matches(_ other: CallLine) -> Bool { id == other.id && address == other.address }

    @MainActor init(session: BackendSession, modem: Modem) {
        backendID = session.id
        modemID = modem.id
        profileID = modem.sim.identifier
        backendName = session.configuration.name
        label = modem.displayName
        region = modem.sim.regionCode
        address = session.configuration.address.absoluteString
    }
}

// A local connection and its server instance both matter: changing an address,
// restoring a server database, or swapping a SIM must not reuse live state.
struct CallIdentity: Hashable, Sendable {
    let connection: UUID
    let address: String
    let backend: String
    let modem: String
    let profile: String
    let call: String

    init(_ record: CallRecord, line: CallLine) {
        connection = line.backendID; address = line.address
        backend = record.backendId ?? ""; modem = line.modemID
        profile = line.profileID; call = record.id
    }
}

struct ActiveCall: Identifiable, Sendable {
    let id: UUID
    let line: CallLine
    var record: CallRecord?
    let number: String
    var muted = false
    var answering = false
    var failure: BackendFailure?
    var endRequested = false
    var ownsCall: Bool
}

struct CallEvent: Decodable, Sendable {
    let type: String
    var call: CallRecord?
    var callID: String?
    var digit: String?
}

struct CallDial: Encodable, Sendable {
    let to: String
    var route = "auto"
    let clientId: String
    let expectedProfileId: String
}

struct CallUpdate: Encodable, Sendable {
    let clientId: String
    var state: String?
    var reason: String?
    var hold: String?
}

struct CallICE: Codable, Sendable {
    let urls: [String]
    var username: String?
    var credential: String?
}

struct CallSDP: Codable, Sendable { let type, sdp: String }
struct CallCandidate: Codable, Sendable {
    let candidate: String
    let sdpMid: String?
    let sdpMLineIndex: Int32?
    var usernameFragment: String?
}
struct CallSignal: Codable, Sendable {
    let type: String
    var clientId: String?
    var offer: CallSDP?
    var answer: CallSDP?
    var candidate: CallCandidate?
    var message: String?
    var code: String?
}

enum CallDigits {
    static func normalize(_ value: String) -> String {
        value.filter { "0123456789+*#".contains($0) }
    }
    static func isService(_ value: String) -> Bool {
        value.hasPrefix("*") || value.hasPrefix("#")
    }
    static func callable(_ value: String) -> Bool {
        let digits = normalize(value)
        return !digits.isEmpty && digits.contains(where: { $0.isNumber })
            && !digits.dropFirst().contains("+") && digits.count <= 128
    }
}

func callText(_ key: String) -> String { NSLocalizedString(key, comment: "") }

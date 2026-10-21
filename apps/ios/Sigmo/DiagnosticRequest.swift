import Foundation

/// Contains only diagnostic metadata, never a URLRequest, body, header collection or URL.
struct DiagnosticRequest: Sendable {
    enum Service: String, Sendable { case backend, oauth, pushGateway, carrier }
    enum Kind: String, Sendable { case http, websocket }

    private let store: DiagnosticLogStore
    private let generation: UInt64
    private let kind: Kind
    private let fields: [String: String]
    private let started = ContinuousClock.now

    init?(store: DiagnosticLogStore, service: Service, method: String, path: [String],
          backendID: UUID? = nil, requestID: String? = nil, kind: Kind = .http) {
        guard let generation = store.generation else { return nil }
        self.store = store
        self.generation = generation
        self.kind = kind
        var fields = ["service": service.rawValue, "method": Self.method(method), "path": Self.path(path),
                      "requestID": Self.identifier(requestID) ?? UUID().uuidString]
        fields["backendID"] = backendID?.uuidString
        self.fields = fields
        record(kind == .http ? "request.started" : "socket.connecting")
    }

    func completed(_ response: BackendHTTPResponse) {
        record("request.completed", status: response.status, requestID: response.requestID)
    }

    func failed(_ error: any Error, response: BackendHTTPResponse? = nil) {
        record(DiagnosticError.isCancellation(error) ? "operation.cancelled" : "operation.failed",
               error: error, status: response?.status, requestID: response?.requestID)
    }

    func decodingFailed(_ error: any Error, response: BackendHTTPResponse) {
        record("response.decode_failed", error: error, status: response.status, requestID: response.requestID)
    }

    func record(_ event: String, error: (any Error)? = nil, status: Int? = nil,
                requestID: String? = nil, extra: [String: String] = [:]) {
        var fields = fields.merging(extra) { _, new in new }
        let duration = started.duration(to: .now).components
        fields["durationMS"] = String(duration.seconds * 1000 + duration.attoseconds / 1_000_000_000_000_000)
        fields["status"] = status.map(String.init)
        fields["serverRequestID"] = Self.identifier(requestID)
        if let error { fields.merge(DiagnosticError.fields(error)) { _, new in new } }
        store.record(event, category: kind.rawValue,
                     level: error.map { DiagnosticError.isCancellation($0) ? .info : .error } ?? .info,
                     fields: fields, generation: generation)
    }

    private static func identifier(_ value: String?) -> String? {
        guard let value, !value.isEmpty, value.utf8.count <= 128,
              value.unicodeScalars.allSatisfy({ identifierCharacters.contains($0) }) else { return nil }
        return value
    }

    private static let identifierCharacters = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._:")

    private static func method(_ value: String) -> String {
        ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"].contains(value) ? value : "OTHER"
    }

    private static let pathSegments: Set<String> = [
        "app", "capabilities", "license", "pairings", "modems", "ses", "esims", "sim-profiles", "settings",
        "activation", "nickname", "notifications", "esim-discoveries", "esim-transfers", "sources", "source-profiles",
        "networks", "registration", "network-scans", "internet-connections", "current", "preferences", "public",
        "wifi-calling", "volte", "sessions", "websheets", "emergency-address-websheets", "resources", "callback",
        "messages", "conversations", "contacts", "avatar", "cached", "calls", "events", "webrtc", "call-media", "ice-servers", "dtmf-events", "ussd",
        "communication-preferences", "message-threads", "history-assignment", "airplane-mode", "bands", "modes",
        "current-bands", "current-modes", "sim-pin", "sim-pin-changes", "sim-unlocks", "sim-application", "protection",
        "push-subscriptions", "push-installations", "mcp", "api-keys", "audit-events", "auth-tests", "channels",
        "enabled", "skills", "sigmo-control", "updates", "update-checks", "update-installations",
        "auth", "methods", "otp", "challenges", "browser-sessions", "session-refreshes",
        "oauth", "resource-metadata", "server-metadata", "token", "revocation"
    ]

    private static func path(_ segments: [String]) -> String {
        "/" + segments.prefix(24).map { pathSegments.contains($0) ? $0 : ":id" }.joined(separator: "/")
    }
}

import Foundation

// In-memory settings shared by explicit demo sessions and automated tests.
// Every mutation stays here, including notification tests and update installs.
struct SettingsFixtureServer {
    var settings: SettingsResponse
    var mcp = MCPSettings(enabled: false, endpointPath: "/mcp", auditRetentionDays: 90,
                          permissions: [.init(name: "modem.read", module: "modem"), .init(name: "sms.read", module: "sms"), .init(name: "sms.send", module: "sms")])
    var keys: [MCPAPIKey] = []
    var audit: [MCPAuditEvent] = []
    var preferences = UpdatePreferences(automatic: false, channel: "stable")
    var version = "1.0.0"
    var commit = "fixture"
    var licenseDisplayName = "Fixture User"
    var updateNotes = "Improve backend reliability.\n<strong>Plain text</strong>"
    var distribution = "standalone"
    var unsupportedReason: String?
    var updateState = "idle"
    var checkedAt: Date?
    var installationPolls = 0
    var oidcTestID = "oidc-test"
    var oidcTestState = ""
    var authTestResult = SettingsAuthTestResult(status: "succeeded", reason: nil, subject: "owner")
    var app: AppInformation { .init(version: version, commit: commit, channel: preferences.channel, target: "linux-arm64", distribution: distribution) }
    var otpRequired: Bool { settings.values.auth["mode"]?.text == "otp" }

    init() throws { settings = try JSONDecoder().decode(SettingsResponse.self, from: Data(SettingsFixtureData.settings.utf8)) }

    mutating func respond(_ request: URLRequest) throws -> BackendHTTPResponse? {
        let path = request.url?.path ?? ""
        let method = request.httpMethod ?? "GET"
        guard !path.contains("/modems/"),
              path.contains("/settings") || path.contains("/update-") else { return nil }
        if otpRequired && request.value(forHTTPHeaderField: "Authorization") != "Bearer fixture-token" {
            return HTTPProblem.response(status: 401, code: "invalid_session", detail: "Login required")
        }
        if path.hasSuffix("/settings") { return try reply(settings) }
        if path.hasSuffix("/settings/auth-tests/oidc") && method == "POST" {
            let value = try decode(OIDCTestRequest.self, request)
            oidcTestState = value.state
            let target = URL(string: "https://settings.example/api/v1/settings/auth-tests/oidc/\(oidcTestID)/browser")!
            return try reply(
                OIDCTestSession(id: oidcTestID, authorizationUrl: target, expiresAt: Date().addingTimeInterval(300)),
                status: 201
            )
        }
        if path.contains("/settings/auth-tests/oidc/") && method == "GET" {
            return try reply(authTestResult)
        }
        if path.hasSuffix("/settings/auth-tests/cloudflare-access") {
            return try reply(authTestResult)
        }
        if path.hasSuffix("/settings/auth-tests") { return BackendHTTPResponse(data: Data(), status: 201) }
        if path.hasSuffix("/settings/auth") {
            settings.values.auth = try decode(SettingsFields.self, request)
            return try reply(settings)
        }
        if path.hasSuffix("/settings/proxy") {
            settings.values.proxy = try decode(SettingsFields.self, request)
            return try reply(settings)
        }
        if path.hasSuffix("/settings/notifications") {
            settings.values.notifications = try decode(SettingsFields.self, request)
            return try reply(settings)
        }
        if path.contains("/settings/channels/") {
            let channel = request.url!.lastPathComponent
            let values = try decode(SettingsFields.self, request)
            settings.values.channels = settings.values.channels ?? [:]
            settings.values.channels?[channel] = values
            return try reply(settings)
        }
        if path.hasSuffix("/settings/mcp") {
            if method == "PUT" { mcp.enabled = try decode([String: Bool].self, request)["enabled"] ?? false }
            return try reply(mcp)
        }
        if path.hasSuffix("/settings/mcp/api-keys") {
            if method == "POST" {
                let value = try decode(MCPFixtureKeyRequest.self, request)
                let key = MCPAPIKey(id: "fixture-key-\(keys.count + 1)", name: value.name, tokenHint: "sigmo_mcp_…fixture", status: "active", allModems: value.allModems, modemIds: value.modemIds, permissions: value.permissions, createdAt: Date(), expiresAt: Date().addingTimeInterval(Double(value.validityDays) * 86400), revokedAt: nil)
                keys.insert(key, at: 0)
                return try reply(MCPCreatedKey(apiKey: key, token: "sigmo_mcp_fixture_secret"), status: 201)
            }
            return try reply(MCPKeys(apiKeys: keys))
        }
        if path.contains("/settings/mcp/api-keys/") && method == "DELETE" {
            if let index = keys.firstIndex(where: { $0.id == request.url?.lastPathComponent }) { keys[index].status = "revoked" }
            return BackendHTTPResponse(data: Data(), status: 204)
        }
        if path.hasSuffix("/settings/mcp/audit-events") { return try reply(MCPAudit(events: audit)) }
        if path.hasSuffix("/settings/mcp/skills/sigmo-control") {
            // A valid empty ZIP archive for exercising the system exporter.
            return BackendHTTPResponse(data: Data([0x50, 0x4b, 0x05, 0x06] + Array(repeating: 0, count: 18)), status: 200)
        }
        if path.hasSuffix("/settings/updates") {
            if method == "PUT" { preferences = try decode(UpdatePreferences.self, request) }
            return try reply(updateSnapshot())
        }
        if path.hasSuffix("/update-checks") { checkedAt = Date(); return try reply(updateSnapshot()) }
        if path.hasSuffix("/update-installations") {
            updateState = "downloading"; installationPolls = 0
            return try reply(updateSnapshot(), status: 202)
        }
        if path.hasSuffix("/update-installations/current") {
            installationPolls += 1
            if updateState != "idle" {
                switch installationPolls {
                case 1: updateState = "verifying"
                case 2: updateState = "restarting"; throw URLError(.networkConnectionLost)
                default: version = "1.1.0"; updateState = "idle"
                }
            }
            return try reply(updateSnapshot())
        }
        return nil
    }

    func updateSnapshot() -> UpdateSnapshot {
        .init(settings: preferences, current: app,
              latest: checkedAt == nil ? nil : .init(version: "1.1.0", notes: updateNotes, publishedAt: Date(timeIntervalSince1970: 1_790_000_000)),
              license: .init(status: "active", deviceId: "fixture-device", telegramId: 10001, displayName: licenseDisplayName, username: "fixture", expiresAt: nil, offlineUntil: nil),
              state: updateState, checkedAt: checkedAt, updateAvailable: checkedAt != nil && version != "1.1.0",
              selfUpdateSupported: unsupportedReason == nil && distribution != "container", unsupportedReason: unsupportedReason, errorCode: nil, error: nil)
    }

    private func decode<Value: Decodable>(_ type: Value.Type, _ request: URLRequest) throws -> Value {
        try JSONDecoder().decode(type, from: request.httpBody ?? Data())
    }
    private func reply<Value: Encodable>(_ value: Value, status: Int = 200) throws -> BackendHTTPResponse {
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        return BackendHTTPResponse(data: try encoder.encode(value), status: status)
    }
}

private struct MCPFixtureKeyRequest: Decodable {
    let name: String
    let validityDays: Int
    let allModems: Bool
    let modemIds: [String]
    let permissions: [String]
}

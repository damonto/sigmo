import Foundation

extension BackendAPI {
    func settings(token: String?) async throws -> SettingsResponse {
        try await decode(["settings"], token: token)
    }

    func saveSettings(_ section: SettingsSection, fields: SettingsFields, token: String?) async throws -> SettingsResponse {
        try await decode(["settings"] + section.path, method: "PUT", body: JSONEncoder().encode(fields), token: token)
    }

    func testAuthentication(_ providers: [String], token: String?) async throws {
        _ = try await request(["settings", "auth-tests"], method: "POST",
                              body: JSONEncoder().encode(["channels": providers]), token: token)
    }

    func createOIDCTest(_ auth: SettingsFields, state: String, token: String?) async throws -> OIDCTestSession {
        guard let parts = URLComponents(url: address, resolvingAgainstBaseURL: false),
              parts.percentEncodedPath.hasSuffix("/api/v1") else {
            throw BackendFailure.local("api.invalidResponse")
        }
        let basePath = String(parts.percentEncodedPath.dropLast("/api/v1".count))
        let body = OIDCTestRequest(client: "native", basePath: basePath, state: state, auth: auth)
        let result: OIDCTestSession = try await decode(
            ["settings", "auth-tests", "oidc"], method: "POST", body: JSONEncoder().encode(body), token: token
        )
        return try result.validated(backend: address)
    }

    func oidcTest(_ id: String, token: String?) async throws -> SettingsAuthTestResult {
        let result: SettingsAuthTestResult = try await decode(
            ["settings", "auth-tests", "oidc", id], token: token
        )
        return try result.validated(allowsPending: true)
    }

    func testCloudflareAccess(_ auth: SettingsFields, token: String?) async throws -> SettingsAuthTestResult {
        let result: SettingsAuthTestResult = try await decode(
            ["settings", "auth-tests", "cloudflare-access"],
            method: "POST", body: JSONEncoder().encode(auth), token: token
        )
        return try result.validated(allowsPending: false)
    }

    func mcpSettings(token: String?) async throws -> MCPSettings {
        try await decode(["settings", "mcp"], token: token)
    }

    func saveMCP(_ enabled: Bool, token: String?) async throws -> MCPSettings {
        try await decode(["settings", "mcp"], method: "PUT", body: JSONEncoder().encode(["enabled": enabled]), token: token)
    }

    func mcpKeys(token: String?) async throws -> MCPKeys {
        try await decode(["settings", "mcp", "api-keys"], token: token)
    }

    func createMCPKey(_ value: MCPKeyRequest, token: String?) async throws -> MCPCreatedKey {
        try await decode(["settings", "mcp", "api-keys"], method: "POST", body: JSONEncoder().encode(value), token: token)
    }

    func revokeMCPKey(_ id: String, token: String?) async throws {
        _ = try await request(["settings", "mcp", "api-keys", id], method: "DELETE", token: token)
    }

    func mcpAudit(token: String?) async throws -> MCPAudit {
        try await decode(["settings", "mcp", "audit-events"], token: token, query: [URLQueryItem(name: "limit", value: "50")])
    }

    func skill(token: String?) async throws -> Data {
        try await request(["settings", "mcp", "skills", "sigmo-control"], method: "GET", token: token, accept: "application/zip").data
    }

    func updates(token: String?) async throws -> UpdateSnapshot {
        try await decode(["settings", "updates"], token: token)
    }

    func saveUpdates(_ value: UpdatePreferences, token: String?) async throws -> UpdateSnapshot {
        try await decode(["settings", "updates"], method: "PUT", body: JSONEncoder().encode(value), token: token)
    }

    func checkUpdates(token: String?) async throws -> UpdateSnapshot {
        try await decode(["update-checks"], method: "POST", token: token)
    }

    func installUpdate(token: String?) async throws -> UpdateSnapshot {
        try await decode(["update-installations"], method: "POST", token: token)
    }

    func installation(token: String?) async throws -> UpdateSnapshot {
        try await decode(["update-installations", "current"], token: token)
    }
}

import Foundation
@testable import Sigmo

actor SettingsTestServer {
    var server: SettingsFixtureServer
    var requests: [URLRequest] = []
    var errors: [String: Int] = [:]
    var offline = false
    var transportError: URLError.Code?
    private var holdSuffix: String?
    private var held: CheckedContinuation<Void, Never>?
    private var arrival: CheckedContinuation<Void, Never>?

    init() throws { server = try SettingsFixtureServer() }
    func setError(_ suffix: String, status: Int?) { errors[suffix] = status }
    func setTransportError(_ code: URLError.Code?) { transportError = code }
    func setOffline(_ value: Bool) { offline = value }
    func setMCPEndpoint(_ path: String) {
        server.mcp = MCPSettings(enabled: false, endpointPath: path, auditRetentionDays: 90, permissions: [])
    }
    func setOTP(_ required: Bool) { server.settings.values.auth["mode"] = .text(required ? "otp" : "none") }
    func setProxyPort(_ port: Int) { server.settings.values.proxy["httpPort"] = .number(port) }
    func setDistribution(_ value: String, reason: String? = nil) { server.distribution = value; server.unsupportedReason = reason }
    func setEdition(_ value: String) { server.edition = value }
    func setUpdateState(_ value: String) { server.updateState = value }
    func allRequests() -> [URLRequest] { requests }
    func holdNext(_ suffix: String) { holdSuffix = suffix }
    func waitForHeldRequest() async {
        if held != nil { return }
        await withCheckedContinuation { arrival = $0 }
    }
    func release() { held?.resume(); held = nil }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if offline { throw URLError(.notConnectedToInternet) }
        if let transportError { throw URLError(transportError) }
        let path = request.url?.path ?? ""
        if let entry = errors.first(where: { path.hasSuffix($0.key) }) {
            return HTTPProblem.response(status: entry.value, code: entry.value == 401 ? "invalid_session" : "remote_error", detail: "Fixture failure")
        }
        let result: BackendHTTPResponse
        if let response = try server.respond(request) { result = response }
        else if path.hasSuffix("/license") { result = HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found") }
        else if path.hasSuffix("/app") { result = BackendHTTPResponse(data: try JSONEncoder().encode(server.app), status: 200) }
        else if path.hasSuffix("/capabilities") { result = reply(200, #"{"features":[]}"#) }
        else if path.hasSuffix("/auth/methods") { result = reply(200, AuthenticationFixture.methods(required: server.otpRequired)) }
        else if path.hasSuffix("/auth/otp/challenges") { result = reply(201, AuthenticationFixture.challenge) }
        else if path.hasSuffix("/auth/sessions/current") { result = reply(204, "") }
        else if path.hasSuffix("/auth/sessions") { result = reply(200, AuthenticationFixture.tokens("fixture-token")) }
        else if path.hasSuffix("/modems") { result = reply(200, "[\(PreviewBackendServer.modemJSON())]") }
        else { result = HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found") }
        if let suffix = holdSuffix, path.hasSuffix(suffix) {
            holdSuffix = nil
            await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
        }
        return result
    }
    private func reply(_ status: Int, _ json: String) -> BackendHTTPResponse { .init(data: Data(json.utf8), status: status) }
}

@MainActor
func settingsSession(_ server: SettingsTestServer, host: String = "settings.example", credentials: MemoryCredentials = MemoryCredentials()) throws -> BackendSession {
    let configuration = try testConfiguration(host)
    return BackendSession(configuration: configuration, credentials: credentials.store,
                          api: BackendAPI(configuration: configuration) { try await server.respond($0) })
}

// Hold a caller before it starts a request, including callers already cancelled
// by a disappearing SwiftUI view. The gate intentionally ignores cancellation.
actor SettingsRequestGate {
    private var waiting: CheckedContinuation<Void, Never>?
    private var arrival: CheckedContinuation<Void, Never>?

    func wait() async {
        await withCheckedContinuation { continuation in
            waiting = continuation
            arrival?.resume()
            arrival = nil
        }
    }

    func waitForCaller() async {
        if waiting != nil { return }
        await withCheckedContinuation { arrival = $0 }
    }

    func release() {
        waiting?.resume()
        waiting = nil
    }
}

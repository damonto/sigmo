import Foundation

struct AccessChallenge: Error, Sendable {
    let metadata: URL

    init?(response: BackendHTTPResponse, backend: URL) {
        guard let header = response.authenticate, header.count < 4096,
              header.lowercased().hasPrefix("bearer "),
              let range = header.range(of: #"resource_metadata="[^"]+""#, options: .regularExpression) else { return nil }
        let value = header[range].dropFirst("resource_metadata=\"".count).dropLast()
        guard let url = URL(string: String(value)), url.scheme == "https", url.user == nil,
              url.fragment == nil, BackendAddress.sameOrigin(backend, url) else { return nil }
        metadata = url
    }
}

struct AccessRegistration: Codable, Sendable {
    let resource: URL
    let issuer: URL
    let authorizationEndpoint: URL
    let tokenEndpoint: URL
    let revocationEndpoint: URL?
    let clientID: String
}

private final class OAuthRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        // Metadata and credential requests never follow redirects or forward secrets.
        completionHandler(nil)
    }
}

final class ManagedAccess: Sendable {
    private let session: URLSession
    private let diagnostics: DiagnosticLogStore
    private let backendID: UUID?
    init(backendID: UUID? = nil, diagnostics: DiagnosticLogStore = AppDiagnostics.store, configuration: URLSessionConfiguration = .ephemeral) {
        self.backendID = backendID
        self.diagnostics = diagnostics
        let config = configuration
        config.httpCookieStorage = nil
        config.httpShouldSetCookies = false
        config.urlCredentialStorage = nil
        config.urlCache = nil
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 30
        session = URLSession(configuration: config, delegate: OAuthRedirects(), delegateQueue: nil)
    }
    deinit { session.invalidateAndCancel() }

    private struct Resource: Decodable {
        let resource: URL
        let authorization_servers: [URL]
    }
    private struct Metadata: Decodable {
        let issuer: String
        let authorization_endpoint: URL
        let token_endpoint: URL
        let registration_endpoint: URL
        let revocation_endpoint: URL?
        let token_endpoint_auth_methods_supported: [String]
        let grant_types_supported: [String]
        let code_challenge_methods_supported: [String]
    }
    private struct RegistrationRequest: Encodable {
        let redirect_uris = [BrowserAuthentication.callback.absoluteString]
        let token_endpoint_auth_method = "none"
        let grant_types = ["authorization_code", "refresh_token"]
        let response_types = ["code"]
        let client_name = "Sigmo iOS"
        let resource: String
    }
    private struct RegistrationResponse: Decodable {
        let client_id: String
        let redirect_uris: [String]
        let token_endpoint_auth_method: String
    }
    private struct TokenResponse: Decodable {
        let access_token: String
        let refresh_token: String?
        let token_type: String
        let expires_in: Double
        let resource: URL?
    }

    func register(_ challenge: AccessChallenge, backend: URL) async throws -> AccessRegistration {
        guard BackendAddress.sameOrigin(challenge.metadata, backend) else { throw BackendFailure.local("auth.loginFailed") }
        let resource: Resource = try await json(challenge.metadata, operation: "resource-metadata")
        guard resource.resource.scheme == "https", BackendAddress.sameOrigin(resource.resource, backend),
              resource.resource.user == nil, resource.resource.query == nil, resource.resource.fragment == nil,
              resource.authorization_servers.count == 1,
              let issuer = resource.authorization_servers.first,
              issuer.scheme == "https", issuer.host?.hasSuffix(".cloudflareaccess.com") == true,
              issuer.user == nil, issuer.port == nil, issuer.query == nil, issuer.fragment == nil,
              issuer.path.isEmpty || issuer.path == "/" else { throw BackendFailure.local("auth.accessSetupRequired") }
        let metadata: Metadata = try await json(issuer.appending(path: ".well-known/oauth-authorization-server"), operation: "server-metadata")
        // Access documents its issuer as a bare team hostname; also accept the RFC URL form.
        guard [issuer.absoluteString, issuer.host ?? ""].contains(metadata.issuer),
              metadata.token_endpoint_auth_methods_supported.contains("none"),
              metadata.grant_types_supported.contains("authorization_code"),
              metadata.code_challenge_methods_supported.contains("S256") else { throw BackendFailure.local("auth.accessSetupRequired") }
        let endpoints = [metadata.authorization_endpoint, metadata.token_endpoint, metadata.registration_endpoint] + [metadata.revocation_endpoint].compactMap { $0 }
        guard endpoints.allSatisfy({ $0.scheme == "https" && $0.user == nil && $0.fragment == nil && BackendAddress.sameOrigin(issuer, $0) }) else {
            throw BackendFailure.local("auth.loginFailed")
        }
        let body = try JSONEncoder().encode(RegistrationRequest(resource: resource.resource.absoluteString))
        let registration: RegistrationResponse = try await json(metadata.registration_endpoint, operation: "registration", method: "POST", body: body)
        guard !registration.client_id.isEmpty, registration.token_endpoint_auth_method == "none",
              registration.redirect_uris == [BrowserAuthentication.callback.absoluteString] else { throw BackendFailure.local("auth.loginFailed") }
        return AccessRegistration(resource: resource.resource, issuer: issuer,
                                  authorizationEndpoint: metadata.authorization_endpoint, tokenEndpoint: metadata.token_endpoint,
                                  revocationEndpoint: metadata.revocation_endpoint, clientID: registration.client_id)
    }

    func authorizationURL(_ registration: AccessRegistration, proof: LoginProof) throws -> URL {
        guard var parts = URLComponents(url: registration.authorizationEndpoint, resolvingAgainstBaseURL: false) else { throw BackendFailure.local("auth.loginFailed") }
        parts.queryItems = [
            URLQueryItem(name: "response_type", value: "code"), URLQueryItem(name: "client_id", value: registration.clientID),
            URLQueryItem(name: "redirect_uri", value: BrowserAuthentication.callback.absoluteString),
            URLQueryItem(name: "state", value: proof.state), URLQueryItem(name: "code_challenge", value: proof.challenge),
            URLQueryItem(name: "code_challenge_method", value: "S256"), URLQueryItem(name: "resource", value: registration.resource.absoluteString),
        ]
        guard let url = parts.url else { throw BackendFailure.local("auth.loginFailed") }
        return url
    }
    func exchange(_ registration: AccessRegistration, code: String, proof: LoginProof) async throws -> SessionCredential {
        try await token(registration, fields: ["grant_type": "authorization_code", "code": code,
                                              "redirect_uri": BrowserAuthentication.callback.absoluteString, "code_verifier": proof.verifier])
    }
    func refresh(_ credential: SessionCredential) async throws -> SessionCredential {
        guard let registration = credential.access, let refresh = credential.refreshToken else { throw BackendFailure.local("auth.loginFailed") }
        var next = try await token(registration, fields: ["grant_type": "refresh_token", "refresh_token": refresh])
        // OAuth servers may retain the old refresh token when they omit a replacement.
        if next.refreshToken == nil { next.refreshToken = refresh }
        return next
    }
    func revoke(_ credential: SessionCredential) async throws {
        guard let registration = credential.access, let endpoint = registration.revocationEndpoint else {
            throw BackendFailure.local("auth.revocationUnavailable")
        }
        // Revoke both forms; RFC 7009 does not require access-token revocation to revoke a refresh token.
        for token in [credential.refreshToken, credential.accessToken].compactMap({ $0 }) {
            _ = try await data(endpoint, operation: "revocation", method: "POST", body: Self.form(["token": token, "client_id": registration.clientID]), contentType: "application/x-www-form-urlencoded")
        }
    }
    private func token(_ registration: AccessRegistration, fields: [String: String]) async throws -> SessionCredential {
        var fields = fields
        fields["client_id"] = registration.clientID
        fields["resource"] = registration.resource.absoluteString
        let result: TokenResponse = try await json(registration.tokenEndpoint, operation: "token", method: "POST", body: Self.form(fields), contentType: "application/x-www-form-urlencoded")
        guard result.token_type.lowercased() == "bearer", !result.access_token.isEmpty,
              result.expires_in.isFinite, result.expires_in > 0, result.expires_in <= 86_400,
              result.resource == nil || BackendAddress.sameOrigin(result.resource!, registration.resource) else { throw BackendFailure.local("auth.loginFailed") }
        return SessionCredential(accessToken: result.access_token, refreshToken: result.refresh_token,
                                 expiresAt: Date().addingTimeInterval(result.expires_in), access: registration)
    }
    private static func form(_ fields: [String: String]) -> Data {
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
        return Data(fields.sorted(by: { $0.key < $1.key }).map {
            "\($0.key.addingPercentEncoding(withAllowedCharacters: allowed)!)=\($0.value.addingPercentEncoding(withAllowedCharacters: allowed)!)"
        }.joined(separator: "&").utf8)
    }
    private func json<Value: Decodable>(_ url: URL, operation: String, method: String = "GET", body: Data? = nil, contentType: String = "application/json") async throws -> Value {
        let response = try await data(url, operation: operation, method: method, body: body, contentType: contentType)
        do { return try JSONDecoder().decode(Value.self, from: response.data) }
        catch {
            response.diagnosticRequest?.decodingFailed(error, response: response)
            throw error
        }
    }
    private func data(_ url: URL, operation: String, method: String, body: Data?, contentType: String) async throws -> BackendHTTPResponse {
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.httpMethod = method
        request.httpBody = body
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if body != nil { request.setValue(contentType, forHTTPHeaderField: "Content-Type") }
        // OAuth endpoint paths are server-defined and may themselves contain secrets.
        let diagnostic = DiagnosticRequest(store: diagnostics, service: .oauth, method: method, path: ["oauth", operation], backendID: backendID)
        var received: BackendHTTPResponse?
        do {
            let (bytes, response) = try await session.bytes(for: request)
            if let response = response as? HTTPURLResponse {
                received = BackendHTTPResponse(data: Data(), status: response.statusCode,
                                               requestID: response.value(forHTTPHeaderField: "X-Request-ID"))
            }
            guard let response = response as? HTTPURLResponse, (200..<300).contains(response.statusCode) else {
                bytes.task.cancel()
                throw BackendFailure.local("auth.loginFailed")
            }
            var result = Data()
            for try await byte in bytes {
                guard result.count < 65_536 else { bytes.task.cancel(); throw BackendFailure.local("api.invalidResponse") }
                result.append(byte)
            }
            let resultResponse = BackendHTTPResponse(data: result, status: response.statusCode, requestID: received?.requestID,
                                                     diagnosticRequest: diagnostic)
            diagnostic?.completed(resultResponse)
            return resultResponse
        } catch {
            diagnostic?.failed(error, response: received)
            throw error
        }
    }
}

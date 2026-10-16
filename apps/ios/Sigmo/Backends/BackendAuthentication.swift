import AuthenticationServices
import CryptoKit
import Foundation
import Security
import UIKit

struct AuthMethods: Codable, Sendable {
    let version: Int
    let mode: String
    let methods: [String]

    func validated() throws -> Self {
        guard version == 1, ["none", "otp", "oidc", "cloudflare_access"].contains(mode),
              methods == (["otp", "oidc"].contains(mode) ? [mode] : []) else { throw BackendFailure.local("api.invalidResponse") }
        return self
    }
}
struct OTPChallenge: Decodable, Sendable { let id: String; let expiresAt: Date }
struct SessionTokens: Decodable, Sendable {
    let accessToken: String
    let refreshToken: String
    let expiresAt: Date
    let sessionExpiresAt: Date

    func credential() throws -> SessionCredential {
        guard !accessToken.isEmpty, !refreshToken.isEmpty, expiresAt > Date(), sessionExpiresAt >= expiresAt else {
            throw BackendFailure.local("api.invalidResponse")
        }
        return SessionCredential(accessToken: accessToken, refreshToken: refreshToken,
                                 expiresAt: expiresAt, sessionExpiresAt: sessionExpiresAt)
    }
}
struct SessionCredential: Codable, Sendable {
    var accessToken: String
    var refreshToken: String?
    var expiresAt: Date
    var sessionExpiresAt: Date?
    var access: AccessRegistration?
}

extension BackendAPI {
    func authMethods(token: String?) async throws -> AuthMethods {
        let result: AuthMethods = try await decode(["auth", "methods"], token: token)
        return try result.validated()
    }
    func sendOTP() async throws -> OTPChallenge {
        try await decode(["auth", "otp", "challenges"], method: "POST")
    }
    func verifyOTP(_ code: String, challenge: String) async throws -> SessionTokens {
        try await decode(["auth", "sessions"], method: "POST", body: JSONEncoder().encode([
            "method": "otp", "client": "native", "challengeId": challenge, "code": code,
        ]))
    }
    func refreshSession(_ refresh: String) async throws -> SessionTokens {
        try await decode(["auth", "session-refreshes"], method: "POST",
                         body: JSONEncoder().encode(["refreshToken": refresh]))
    }
    func browserLogin(_ proof: LoginProof) async throws -> URL {
        struct Response: Decodable, Sendable { let authorizationUrl: URL }
        guard let parts = URLComponents(url: address, resolvingAgainstBaseURL: false),
              parts.percentEncodedPath.hasSuffix("/api/v1") else {
            throw BackendFailure.local("api.invalidResponse")
        }
        let basePath = String(parts.percentEncodedPath.dropLast("/api/v1".count))
        let result: Response = try await decode(["auth", "browser-sessions"], method: "POST",
                                               body: JSONEncoder().encode(["client": "native", "state": proof.state,
                                                                         "codeChallenge": proof.challenge, "basePath": basePath]))
        guard result.authorizationUrl.scheme == "https", BackendAddress.sameOrigin(address, result.authorizationUrl),
              result.authorizationUrl.user == nil, result.authorizationUrl.fragment == nil else {
            throw BackendFailure.local("api.invalidResponse")
        }
        return result.authorizationUrl
    }
    func completeBrowserLogin(code: String, proof: LoginProof) async throws -> SessionTokens {
        try await decode(["auth", "sessions"], method: "POST", body: JSONEncoder().encode([
            "method": "oidc", "client": "native", "code": code, "codeVerifier": proof.verifier,
        ]))
    }
    func revokeSession(token: String) async throws {
        _ = try await request(["auth", "sessions", "current"], method: "DELETE", token: token)
    }
}

struct LoginProof: Sendable {
    let state: String
    let verifier: String
    var challenge: String { Data(SHA256.hash(data: Data(verifier.utf8))).base64URL }

    init() throws {
        state = try Self.random()
        verifier = try Self.random()
    }
    private static func random() throws -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            throw BackendFailure.local("auth.loginFailed")
        }
        return Data(bytes).base64URL
    }
    func code(from url: URL) throws -> String {
        guard let parts = URLComponents(url: url, resolvingAgainstBaseURL: false),
              parts.scheme == "https", parts.host == BrowserAuthentication.callback.host,
              parts.path == BrowserAuthentication.callback.path, parts.port == nil,
              parts.user == nil, parts.fragment == nil else { throw BackendFailure.local("auth.loginFailed") }
        let items = parts.queryItems ?? []
        guard items.filter({ $0.name == "state" }).count == 1,
              items.first(where: { $0.name == "state" })?.value == state,
              !items.contains(where: { $0.name == "error" }),
              items.filter({ $0.name == "code" }).count == 1,
              let code = items.first(where: { $0.name == "code" })?.value, !code.isEmpty else {
            throw BackendFailure.local("auth.loginFailed")
        }
        return code
    }
}
private extension Data {
    var base64URL: String { base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "") }
}

@MainActor
protocol BrowserAuthenticating {
    func open(_ url: URL) async throws -> URL
    func cancel()
}

@MainActor
final class BrowserAuthentication: BrowserAuthenticating {
    nonisolated static let callback = URL(string: "https://sigmo-pro.952007.xyz/auth/callback")!
    private var session: ASWebAuthenticationSession?
    // The authentication session holds its presentation context weakly.
    private var presentationContext: BrowserPresentationContext?
    private var pending: CheckedContinuation<URL, any Error>?
    private var operation: UUID?

    func open(_ url: URL) async throws -> URL {
        guard session == nil, !Task.isCancelled else { throw CancellationError() }
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        guard let scene = scenes.first(where: { $0.activationState == .foregroundActive })
            ?? scenes.first(where: { $0.activationState == .foregroundInactive }) else {
            throw BackendFailure.local("auth.loginFailed")
        }
        let id = UUID()
        operation = id
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                pending = continuation
                let browser = ASWebAuthenticationSession(url: url, callback: .https(host: Self.callback.host!, path: Self.callback.path)) { [weak self] url, error in
                    Task { @MainActor in
                        guard let self, self.operation == id else { return }
                        if let url { self.finish(.success(url)) }
                        else if (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin {
                            self.finish(.failure(CancellationError()))
                        } else { self.finish(.failure(BackendFailure.local("auth.loginFailed"))) }
                    }
                }
                let context = BrowserPresentationContext(windowScene: scene)
                presentationContext = context
                browser.presentationContextProvider = context
                // Keep the IdP's browser session for the user's next explicit login.
                browser.prefersEphemeralWebBrowserSession = false
                session = browser
                if !browser.start() { finish(.failure(BackendFailure.local("auth.loginFailed"))) }
            }
        } onCancel: {
            Task { @MainActor [weak self] in
                if self?.operation == id { self?.cancel() }
            }
        }
    }
    func cancel() {
        session?.cancel()
        finish(.failure(CancellationError()))
    }
    private func finish(_ result: Result<URL, any Error>) {
        let continuation = pending
        pending = nil
        operation = nil
        session = nil
        presentationContext = nil
        continuation?.resume(with: result)
    }
}

@MainActor
private final class BrowserPresentationContext: NSObject, ASWebAuthenticationPresentationContextProviding {
    private let anchor: ASPresentationAnchor

    init(windowScene: UIWindowScene) {
        anchor = windowScene.keyWindow ?? ASPresentationAnchor(windowScene: windowScene)
        super.init()
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        anchor
    }
}

import XCTest
@testable import Sigmo

@MainActor
final class BrowserLoginTests: XCTestCase {
    func testAccessCredentialSurvivesAnonymousOriginDiscoveryAndRelaunch() async throws {
        let (session, backend, browser, credentials) = try await fixture("none", accessProtected: true)
        defer { session.stop() }
        let login = Task { await session.loginWithBrowser() }
        try await waitForOpen(browser, count: 1)
        try browser.complete()
        await login.value
        XCTAssertEqual(session.authentication, .authenticated)
        XCTAssertTrue(session.isOnline)
        let saved = try XCTUnwrap(credentials.tokens[session.id])

        await session.refresh(force: true)
        XCTAssertEqual(session.authentication, .authenticated)
        XCTAssertTrue(session.isOnline)
        XCTAssertEqual(credentials.tokens[session.id], saved)

        let restored = BackendSession(configuration: session.configuration, credentials: credentials.store,
            api: BackendAPI(configuration: session.configuration) { try await backend.respond($0) })
        defer { restored.stop() }
        await restored.refresh(force: true)
        XCTAssertEqual(restored.authentication, .authenticated)
        XCTAssertTrue(restored.isOnline)
        XCTAssertEqual(credentials.tokens[session.id], saved)
    }

    func testSceneRefreshCannotCancelBrowserLoginAndCancelledLoginCanBeRetried() async throws {
        for mode in ["oidc", "cloudflare_access"] {
            let (session, backend, browser, credentials) = try await fixture(mode)
            defer { session.stop() }
            let first = Task { await session.loginWithBrowser() }
            try await waitForOpen(browser, count: 1)
            let firstURL = try XCTUnwrap(browser.urls.first)

            // Browser presentation and returning from the IdP both reactivate the scene.
            await session.refresh(force: true)
            XCTAssertTrue(session.isBrowserLogin, mode)
            XCTAssertTrue(browser.isOpen, mode)
            let discoveryCount = await backend.discoveryCount
            XCTAssertEqual(discoveryCount, 1, mode)

            browser.cancel()
            await first.value
            XCTAssertFalse(session.isBrowserLogin, mode)
            XCTAssertEqual(session.authentication, .required, mode)
            XCTAssertNil(session.authFailure, mode)
            XCTAssertNil(credentials.tokens[session.id], mode)

            let retry = Task { await session.loginWithBrowser() }
            try await waitForOpen(browser, count: 2)
            XCTAssertNotEqual(firstURL, browser.urls.last, mode)
            await session.refresh(force: true)
            XCTAssertTrue(browser.isOpen, mode)
            try browser.complete()
            await retry.value

            XCTAssertFalse(session.isBrowserLogin, mode)
            XCTAssertEqual(session.authentication, .authenticated, mode)
            XCTAssertTrue(session.isOnline, mode)
            XCTAssertNil(session.authFailure, mode)
            let saved = try JSONDecoder().decode(SessionCredential.self, from: Data(XCTUnwrap(credentials.tokens[session.id]).utf8))
            XCTAssertEqual(saved.accessToken, "new-token", mode)
            XCTAssertEqual(saved.access != nil, mode == "cloudflare_access")
            let finalDiscoveryCount = await backend.discoveryCount
            XCTAssertEqual(finalDiscoveryCount, 2, mode)
        }
    }

    func testCancellingCallerDoesNotCancelLoginOrItsFinalRefresh() async throws {
        for mode in ["oidc", "cloudflare_access"] {
            let (session, backend, browser, credentials) = try await fixture(mode)
            defer { session.stop() }
            let caller = Task { await session.loginWithBrowser() }
            try await waitForOpen(browser, count: 1)
            caller.cancel()
            XCTAssertTrue(session.isBrowserLogin, mode)
            XCTAssertTrue(browser.isOpen, mode)

            await backend.stub.holdNextRequest("/modems")
            try browser.complete()
            await backend.stub.waitForHeldRequest()
            // Authentication has succeeded, so settings replace the login button
            // with their content while the session still owns this refresh.
            XCTAssertEqual(session.authentication, .authenticated, mode)
            XCTAssertTrue(session.isBrowserLogin, mode)
            XCTAssertFalse(session.hasLoadedModems, mode)
            await backend.stub.release()
            await caller.value
            XCTAssertTrue(session.isOnline, mode)
            XCTAssertFalse(session.isBrowserLogin, mode)
            XCTAssertNotNil(credentials.tokens[session.id], mode)
        }
    }

    func testExplicitCancellationAllowsRetryAndStopCancelsLogin() async throws {
        for mode in ["oidc", "cloudflare_access"] {
            let (session, _, browser, credentials) = try await fixture(mode)
            defer { session.stop() }
            let first = Task { await session.loginWithBrowser() }
            try await waitForOpen(browser, count: 1)
            session.cancelBrowserLogin()
            await first.value
            XCTAssertFalse(browser.isOpen, mode)
            XCTAssertFalse(session.isBrowserLogin, mode)
            XCTAssertNil(session.authFailure, mode)
            XCTAssertNil(credentials.tokens[session.id], mode)

            let retry = Task { await session.loginWithBrowser() }
            try await waitForOpen(browser, count: 2)
            session.stop()
            await retry.value
            XCTAssertFalse(browser.isOpen, mode)
            XCTAssertFalse(session.isBrowserLogin, mode)
            XCTAssertNil(credentials.tokens[session.id], mode)
        }
    }

    func testConcurrentCallersShareOneLogin() async throws {
        let (session, backend, browser, _) = try await fixture("oidc")
        defer { session.stop() }
        let first = Task { await session.loginWithBrowser() }
        try await waitForOpen(browser, count: 1)
        let joined = expectation(description: "Second caller joins login")
        let second = Task {
            joined.fulfill()
            await session.loginWithBrowser()
            XCTAssertTrue(session.isOnline)
        }
        await fulfillment(of: [joined], timeout: 2)
        try browser.complete()
        await first.value
        await second.value
        XCTAssertEqual(browser.urls.count, 1)
        let attempts = await backend.stub.count("/auth/browser-sessions")
        XCTAssertEqual(attempts, 1)
    }

    func testCancelledPreparationCannotOverwriteANewLogin() async throws {
        let (session, backend, browser, credentials) = try await fixture("oidc")
        defer { session.stop() }
        await backend.stub.holdNextRequest("/auth/browser-sessions")
        let first = Task { await session.loginWithBrowser() }
        await backend.stub.waitForHeldRequest()
        session.cancelBrowserLogin()
        XCTAssertFalse(session.isBrowserLogin)

        let retry = Task { await session.loginWithBrowser() }
        try await waitForOpen(browser, count: 1)
        // The old transport deliberately returns after cancellation and retry.
        await backend.stub.release()
        await first.value
        XCTAssertTrue(session.isBrowserLogin)
        XCTAssertTrue(browser.isOpen)
        XCTAssertNil(credentials.tokens[session.id])
        try browser.complete()
        await retry.value
        XCTAssertTrue(session.isOnline)
    }

    func testRefreshDuringBrowserPreparationDoesNotInvalidateLogin() async throws {
        let (session, backend, browser, _) = try await fixture("oidc")
        defer { session.stop() }
        await backend.stub.holdNextRequest("/auth/browser-sessions")
        let login = Task { await session.loginWithBrowser() }
        await backend.stub.waitForHeldRequest()
        await session.refresh(force: true)
        XCTAssertTrue(session.isBrowserLogin)
        let discoveryCount = await backend.discoveryCount
        XCTAssertEqual(discoveryCount, 1)
        await backend.stub.release()
        try await waitForOpen(browser, count: 1)
        try browser.complete()
        await login.value
        XCTAssertTrue(session.isOnline)
    }

    private func fixture(_ mode: String, accessProtected: Bool = false) async throws -> (BackendSession, BrowserLoginBackend, LoginBrowser, MemoryCredentials) {
        let configuration = try testConfiguration()
        let backend = BrowserLoginBackend(mode: mode, accessProtected: accessProtected)
        let browser = LoginBrowser()
        let credentials = MemoryCredentials()
        let oauthConfiguration = URLSessionConfiguration.ephemeral
        oauthConfiguration.protocolClasses = [LoginOAuthProtocol.self]
        let session = BackendSession(configuration: configuration, credentials: credentials.store,
            api: BackendAPI(configuration: configuration) { try await backend.respond($0) },
            browser: browser, managedAccess: ManagedAccess(configuration: oauthConfiguration))
        await session.refresh(force: true)
        XCTAssertEqual(session.authentication, .required)
        XCTAssertTrue(session.supportsBrowserLogin)
        return (session, backend, browser, credentials)
    }

    private func waitForOpen(_ browser: LoginBrowser, count: Int) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while browser.urls.count < count, ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertEqual(browser.urls.count, count)
        guard browser.isOpen else { throw URLError(.timedOut) }
    }
}

@MainActor
private final class LoginBrowser: BrowserAuthenticating {
    private var pending: CheckedContinuation<URL, any Error>?
    private var operation: UUID?
    private(set) var urls: [URL] = []
    var isOpen: Bool { pending != nil }

    func open(_ url: URL) async throws -> URL {
        try Task.checkCancellation()
        guard pending == nil else { throw CancellationError() }
        let id = UUID()
        operation = id
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                pending = continuation
                urls.append(url)
            }
        } onCancel: {
            Task { @MainActor in
                if self.operation == id { self.cancel() }
            }
        }
    }

    func cancel() { finish(.failure(CancellationError())) }

    func complete() throws {
        let url = try XCTUnwrap(urls.last)
        let state = try XCTUnwrap(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "state" }?.value)
        let callback = BrowserAuthentication.callback.appending(queryItems: [
            URLQueryItem(name: "state", value: state), URLQueryItem(name: "code", value: "handoff"),
        ])
        finish(.success(callback))
    }

    private func finish(_ result: Result<URL, any Error>) {
        let continuation = pending
        pending = nil
        operation = nil
        continuation?.resume(with: result)
    }
}

private actor BrowserLoginBackend {
    let mode: String
    let accessProtected: Bool
    let stub = BackendStub()
    private(set) var discoveryCount = 0

    init(mode: String, accessProtected: Bool) {
        self.mode = mode
        self.accessProtected = accessProtected || mode == "cloudflare_access"
    }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        let url = try XCTUnwrap(request.url)
        if url.path.hasSuffix("/auth/methods") { discoveryCount += 1 }
        if accessProtected, request.value(forHTTPHeaderField: "Authorization") != "Bearer new-token" {
            return BackendHTTPResponse(data: Data(), status: 401,
                authenticate: "Bearer resource_metadata=\"https://one.example/resource-metadata\"")
        }
        if url.path.hasSuffix("/auth/methods") {
            let methods = mode == "oidc" ? [mode] : []
            return BackendHTTPResponse(data: try JSONEncoder().encode(AuthMethods(version: 1, mode: mode, methods: methods)), status: 200)
        }
        if url.path.hasSuffix("/auth/browser-sessions") {
            _ = try await stub.respond(request)
            let fields = try JSONDecoder().decode([String: String].self, from: XCTUnwrap(request.httpBody))
            let target = url.appending(path: "transaction").appending(queryItems: [URLQueryItem(name: "state", value: fields["state"])])
            return BackendHTTPResponse(data: try JSONEncoder().encode(["authorizationUrl": target.absoluteString]), status: 201)
        }
        if url.path.hasSuffix("/auth/sessions") {
            return BackendHTTPResponse(data: Data(AuthenticationFixture.tokens("new-token").utf8), status: 200)
        }
        return try await stub.respond(request)
    }
}

// Installed only on the injected OAuth session; discovery and exchange use no network.
private final class LoginOAuthProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let url = request.url else { return }
        let body: String
        switch url.lastPathComponent {
        case "resource-metadata":
            body = #"{"resource":"https://one.example","authorization_servers":["https://team.cloudflareaccess.com"]}"#
        case "oauth-authorization-server":
            body = #"{"issuer":"team.cloudflareaccess.com","authorization_endpoint":"https://team.cloudflareaccess.com/authorization","token_endpoint":"https://team.cloudflareaccess.com/token","registration_endpoint":"https://team.cloudflareaccess.com/register","token_endpoint_auth_methods_supported":["none"],"grant_types_supported":["authorization_code","refresh_token"],"code_challenge_methods_supported":["S256"]}"#
        case "register":
            body = #"{"client_id":"fixture-client","redirect_uris":["\#(BrowserAuthentication.callback.absoluteString)"],"token_endpoint_auth_method":"none"}"#
        case "token":
            body = #"{"access_token":"new-token","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}"#
        default:
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        guard let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"]) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

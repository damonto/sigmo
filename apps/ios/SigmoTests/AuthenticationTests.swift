import XCTest
@testable import Sigmo

final class AuthenticationTests: XCTestCase {
    func testBrowserLoginDerivesPathFromBackendAddress() async throws {
        for basePath in ["", "/sigmo", "/my%20sigmo"] {
            let address = try BackendAddress.normalize("https://sigmo.example:8443" + basePath)
            let proof = try LoginProof()
            let target = address.appending(path: "auth/browser-sessions/transaction")
            let api = BackendAPI(configuration: BackendConfiguration(name: "Sigmo", address: address), transport: { request in
                let body = try JSONDecoder().decode([String: String].self, from: XCTUnwrap(request.httpBody))
                XCTAssertEqual(body["basePath"], basePath)
                XCTAssertEqual(body["state"], proof.state)
                XCTAssertEqual(body["codeChallenge"], proof.challenge)
                XCTAssertEqual(body["client"], "native")
                return BackendHTTPResponse(data: try JSONEncoder().encode(["authorizationUrl": target.absoluteString]), status: 201)
            })
            let actual = try await api.browserLogin(proof)
            XCTAssertEqual(actual, target)
        }
    }

    func testDiscoverySelectsExactlyOneAuthenticationMode() throws {
        for mode in ["none", "otp", "oidc", "cloudflare_access"] {
            let methods = ["otp", "oidc"].contains(mode) ? [mode] : []
            XCTAssertNoThrow(try AuthMethods(version: 1, mode: mode, methods: methods).validated())
        }
        for (mode, methods) in [("sigmo", ["otp"]), ("otp", ["otp", "oidc"]), ("oidc", ["otp"]), ("none", ["otp"])] {
            XCTAssertThrowsError(try AuthMethods(version: 1, mode: mode, methods: methods).validated())
        }
    }

    @MainActor
    func testAnonymousOriginClearsSigmoCredentials() async throws {
        let stub = BackendStub()
        let credentials = MemoryCredentials()
        let configuration = try testConfiguration()
        credentials.tokens[configuration.id] = storedCredential("old-token")
        let session = try testSession(stub, configuration: configuration, credentials: credentials)
        await session.refresh(force: true)
        XCTAssertEqual(session.authentication, .notRequired)
        XCTAssertTrue(session.isOnline)
        XCTAssertNil(credentials.tokens[configuration.id])
    }

    @MainActor
    func testLogoutRevokesTheSessionAfterAccessTokenExpiration() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let credentials = MemoryCredentials()
        let configuration = try testConfiguration()
        credentials.tokens[configuration.id] = storedCredential("old-token")
        let time = TestTime()
        time.advance(199_999_880)
        let session = try testSession(stub, configuration: configuration, credentials: credentials, clock: time.clock)
        await session.refresh(force: true)
        time.advance(121)
        await session.logout()
        let requests = await stub.allRequests()
        let revocation = try XCTUnwrap(requests.first { $0.httpMethod == "DELETE" && $0.url?.path.hasSuffix("/auth/sessions/current") == true })
        XCTAssertEqual(revocation.value(forHTTPHeaderField: "Authorization"), "Bearer rotated-token")
        XCTAssertNil(credentials.tokens[configuration.id])
        XCTAssertFalse(session.canAccessDevices)
        XCTAssertNil(session.authFailure)
    }

    @MainActor
    func testConcurrentRequestsShareOneCredentialRotation() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let credentials = MemoryCredentials()
        let configuration = try testConfiguration()
        credentials.tokens[configuration.id] = storedCredential("old-token")
        let time = TestTime()
        time.advance(199_999_880)
        let session = try testSession(stub, configuration: configuration, credentials: credentials, clock: time.clock)
        await session.refresh(force: true)
        XCTAssertTrue(session.isOnline)
        time.advance(61)
        await stub.holdNextRequest("/auth/session-refreshes")
        let first = Task { try await session.protectedRequest { _, token in token } }
        await stub.waitForHeldRequest()
        let started = expectation(description: "Second request shares refresh")
        let second = Task {
            started.fulfill()
            return try await session.protectedRequest { _, token in token }
        }
        await fulfillment(of: [started], timeout: 2)
        await stub.release()
        let values = try await [first.value, second.value]
        XCTAssertEqual(values, ["rotated-token", "rotated-token"])
        let count = await stub.count("/auth/session-refreshes")
        XCTAssertEqual(count, 1)
        let saved = try JSONDecoder().decode(SessionCredential.self, from: Data(XCTUnwrap(credentials.tokens[configuration.id]).utf8))
        XCTAssertEqual(saved.refreshToken, "refresh-rotated-token")
    }

    @MainActor
    func testLogoutRevokesAfterItsInitiatingTaskIsCancelled() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let credentials = MemoryCredentials()
        let configuration = try testConfiguration()
        credentials.tokens[configuration.id] = storedCredential("old-token")
        let time = TestTime()
        time.advance(199_999_880)
        let session = try testSession(stub, configuration: configuration, credentials: credentials, clock: time.clock)
        await session.refresh(force: true)
        time.advance(61)
        await stub.holdNextRequest("/auth/session-refreshes")
        let logout = Task { await session.logout() }
        await stub.waitForHeldRequest()
        XCTAssertTrue(session.isLoggingOut)
        XCTAssertEqual(session.authentication, .required)

        // Publishing .required removes the logout button and cancels its .task.
        logout.cancel()
        await stub.release()
        await logout.value

        let requests = await stub.allRequests()
        let revocation = try XCTUnwrap(requests.first { $0.httpMethod == "DELETE" && $0.url?.path.hasSuffix("/auth/sessions/current") == true })
        XCTAssertEqual(revocation.value(forHTTPHeaderField: "Authorization"), "Bearer rotated-token")
        XCTAssertNil(credentials.tokens[configuration.id])
        XCTAssertFalse(session.isLoggingOut)
        XCTAssertNil(session.authFailure)
    }

    @MainActor
    func testOfflineRefreshPreservesCredentialUntilConnectivityReturns() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let credentials = MemoryCredentials()
        let configuration = try testConfiguration()
        credentials.tokens[configuration.id] = storedCredential("old-token")
        let time = TestTime()
        time.advance(199_999_880)
        let session = try testSession(stub, configuration: configuration, credentials: credentials, clock: time.clock)
        await session.refresh(force: true)
        time.advance(61)
        await stub.setOffline(true)
        await session.refresh(force: true)
        XCTAssertEqual(credentials.tokens[configuration.id], storedCredential("old-token"))
        XCTAssertEqual(session.failure?.transportCode, .notConnectedToInternet)

        await stub.setOffline(false)
        await session.refresh(force: true)
        XCTAssertTrue(session.isOnline)
        let saved = try JSONDecoder().decode(SessionCredential.self, from: Data(XCTUnwrap(credentials.tokens[configuration.id]).utf8))
        XCTAssertEqual(saved.refreshToken, "refresh-rotated-token")
    }

    @MainActor
    func testUncertainRefreshOutcomesStillRequireLogin() async throws {
        for code in [URLError.Code.networkConnectionLost, .timedOut] {
            let stub = BackendStub()
            await stub.setRequired(true)
            let credentials = MemoryCredentials()
            let configuration = try testConfiguration()
            credentials.tokens[configuration.id] = storedCredential("old-token")
            let time = TestTime()
            time.advance(199_999_880)
            let session = try testSession(stub, configuration: configuration, credentials: credentials, clock: time.clock)
            await session.refresh(force: true)
            time.advance(61)
            await stub.setRefreshError(code)
            await session.refresh(force: true)
            XCTAssertNil(credentials.tokens[configuration.id], "\(code)")
            XCTAssertEqual(session.authentication, .required)
        }
    }

    @MainActor
    func testBusinessAccessChallengesEnableBrowserLoginAndClearCredentials() async throws {
        for operation in ["settings", "calls", "modem"] {
            let stub = BackendStub()
            await stub.setRequired(true)
            let credentials = MemoryCredentials()
            let configuration = try testConfiguration()
            credentials.tokens[configuration.id] = storedCredential("access-token")
            let session = try testSession(stub, configuration: configuration, credentials: credentials)
            await session.refresh(force: true)
            XCTAssertTrue(session.isOnline)
            await stub.setModemStatus(503)
            await session.refresh(force: true)
            XCTAssertNotNil(session.failure)
            await stub.setModemStatus(200)
            await stub.setAccessChallenge(operation == "modem" ? "/modems/device-1" : "/capabilities")
            if operation == "modem" {
                await session.refreshModem("device-1")
            } else {
                do {
                    if operation == "calls" {
                        _ = try await session.callRequest { api, token in try await api.capabilities(token: token) }
                    } else {
                        _ = try await session.protectedRequest { api, token in try await api.capabilities(token: token) }
                    }
                    XCTFail("Expected an Access challenge")
                } catch let error as BackendFailure {
                    XCTAssertEqual(error.code, "status.loginRequired")
                }
            }
            XCTAssertEqual(session.authentication, .required, operation)
            XCTAssertEqual(session.statusKey, "status.loginRequired", operation)
            XCTAssertNil(session.failure, operation)
            XCTAssertTrue(session.supportsBrowserLogin, operation)
            XCTAssertNil(credentials.tokens[configuration.id], operation)
            XCTAssertTrue(session.modems.isEmpty, operation)
        }
    }

    func testCallbackIsBoundToStateAndExactHTTPSAddress() throws {
        let proof = try LoginProof()
        let valid = BrowserAuthentication.callback.absoluteString + "?code=handoff&state=" + proof.state
        XCTAssertEqual(try proof.code(from: XCTUnwrap(URL(string: valid))), "handoff")
        for raw in [
            valid.replacingOccurrences(of: "https:", with: "http:"),
            valid.replacingOccurrences(of: "/auth/callback", with: "/elsewhere"),
            valid + "&state=" + proof.state,
            valid + "&code=second", valid + "&error=denied", valid + "#fragment",
            valid.replacingOccurrences(of: proof.state, with: "different-state"),
            valid.replacingOccurrences(of: "sigmo-pro.952007.xyz", with: "evil.example"),
        ] {
            XCTAssertThrowsError(try proof.code(from: XCTUnwrap(URL(string: raw))))
        }
        XCTAssertEqual(proof.verifier.count, 43)
        XCTAssertEqual(proof.challenge.count, 43)
        XCTAssertNotEqual(try LoginProof().state, proof.state)
    }

    func testAccessDiscoveryCannotRedirectMetadataToAnotherHost() throws {
        let backend = try XCTUnwrap(URL(string: "https://sigmo.example/api/v1"))
        for (metadata, accepted) in [
            ("https://sigmo.example/.well-known/cloudflare-access-protected-resource/", true),
            ("http://sigmo.example/resource", false),
            ("https://evil.example/resource", false),
            ("https://user@sigmo.example/resource", false),
        ] {
            let response = BackendHTTPResponse(data: Data(), status: 401, authenticate: "Bearer resource_metadata=\"\(metadata)\"")
            XCTAssertEqual(AccessChallenge(response: response, backend: backend) != nil, accepted)
        }
    }

    func testCarrierRequestsCannotAcquireCredentialsOutsideTheirCapability() throws {
        let root = try XCTUnwrap(URL(string: "https://sigmo.example/api/v1/websheets/secret-id"))
        let loader = CarrierResourceLoader(root: root)
        let local = CarrierResourceLoader.localURL(root.appending(path: "proxy/https/carrier.example/form"))
        var request = URLRequest(url: local)
        request.setValue("untrusted", forHTTPHeaderField: "Authorization")
        request.setValue("untrusted", forHTTPHeaderField: "Cookie")
        let result = try loader.request(request)
        XCTAssertEqual(result.url?.scheme, "https")
        XCTAssertNil(result.value(forHTTPHeaderField: "Authorization"))
        XCTAssertNil(result.value(forHTTPHeaderField: "Cookie"))
        for path in ["/api/v1/settings", "/api/v1/websheets/secret-id-other", "/api/v1/websheets/secret-id/../../settings"] {
            XCTAssertThrowsError(try loader.request(URLRequest(url: XCTUnwrap(URL(string: "sigmo-carrier://sigmo.example" + path)))))
        }
    }
}

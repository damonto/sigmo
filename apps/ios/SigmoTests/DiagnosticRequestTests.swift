import Network
import Synchronization
import XCTest
@testable import Sigmo

@MainActor
final class DiagnosticRequestTests: XCTestCase {
    private var directory: URL!
    private var log: DiagnosticLogStore!

    override func setUp() async throws {
        directory = .temporaryDirectory.appending(path: "DiagnosticRequestTests-\(UUID())")
        log = DiagnosticLogStore(directory: directory, defaults: nil)
        await log.setEnabled(true)
    }

    override func tearDown() async throws {
        _ = try? await log.snapshot()
        if FileManager.default.fileExists(atPath: directory.path) { try FileManager.default.removeItem(at: directory) }
    }

    func testBackendLogsMetadataWithoutChangingOrDisclosingTheRequest() async throws {
        let received = Mutex<[URLRequest]>([])
        let configuration = try testConfiguration()
        let api = BackendAPI(configuration: configuration, diagnostics: log, transport: { request in
            received.withLock { $0.append(request) }
            return BackendHTTPResponse(data: Data("response-body-secret".utf8), status: 200, requestID: "server-request")
        })
        _ = try await api.request(["modems", "device-secret", "ses", "se-secret", "esims", "iccid-secret"],
                                  method: "POST", body: Data("request-body-secret".utf8), token: "credential-secret",
                                  query: [URLQueryItem(name: "q", value: "query-secret")])
        let request = try XCTUnwrap(received.withLock { $0.first })
        XCTAssertEqual(request.httpBody, Data("request-body-secret".utf8))
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer credential-secret")
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.map(\.event), ["request.started", "request.completed"])
        XCTAssertTrue(events.allSatisfy { $0.fields["path"] == "/modems/:id/ses/:id/esims/:id" })
        XCTAssertEqual(events.last?.fields["status"], "200")
        XCTAssertEqual(events.last?.fields["serverRequestID"], "server-request")
        XCTAssertEqual(events.last?.fields["backendID"], configuration.id.uuidString)
        XCTAssertEqual(events.last?.fields["requestID"], request.value(forHTTPHeaderField: "X-Request-ID"))
        XCTAssertNotNil(events.last?.fields["durationMS"])
        assertNoSecrets(data, ["device-secret", "se-secret", "iccid-secret", "request-body-secret", "response-body-secret", "credential-secret", "query-secret", "Authorization"])
    }

    func testHTTPAndNetworkFailuresAndCancellationAreDistinguished() async throws {
        let cases: [(String, BackendAPI.Transport)] = [
            ("http", { _ in HTTPProblem.response(status: 429, code: "login_rate_limited", detail: "error-detail-secret", requestID: "rejected-request") }),
            ("timeout", { _ in throw URLError(.timedOut) }),
            ("offline", { _ in throw URLError(.notConnectedToInternet) }),
            ("cancelled", { _ in throw URLError(.cancelled) })
        ]
        for (name, transport) in cases {
            try await log.clear()
            let api = BackendAPI(configuration: try testConfiguration(), diagnostics: log, transport: transport)
            do {
                _ = try await api.request(["app"], method: "GET")
                XCTFail("Expected \(name) to fail")
            } catch {}
            let data = try await log.snapshot()
            let events = try diagnosticEvents(data)
            XCTAssertEqual(events.count, 2, name)
            XCTAssertEqual(events.last?.level, name == "cancelled" ? .info : .error, name)
            if name == "http" {
                XCTAssertEqual(events.last?.fields["status"], "429")
                XCTAssertEqual(events.last?.fields["serverRequestID"], "rejected-request")
                XCTAssertEqual(events.last?.fields["errorCode"], "login_rate_limited")
            }
            if name == "timeout" { XCTAssertEqual(events.last?.fields["transportCode"], "-1001") }
            if name == "offline" { XCTAssertEqual(events.last?.fields["transportCode"], "-1009") }
            if name == "cancelled" { XCTAssertEqual(events.last?.fields["errorCode"], "cancelled") }
            assertNoSecrets(data, ["error-detail-secret"])
        }
    }

    func testLiveHTTPTransportRecordsLocalServerSuccessAndFailure() async throws {
        for status in [200, 503] {
            try await log.clear()
            let parameters = NWParameters.tcp
            parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
            let listener = try NWListener(using: parameters)
            let connections = Mutex<[NWConnection]>([])
            let queue = DispatchQueue(label: "com.damon.sigmo.diagnostic-http-fixture")
            let ready = expectation(description: "Local HTTP listener ready")
            let body = Data("local-response-secret".utf8)
            let response = Data("HTTP/1.1 \(status) Fixture\r\nContent-Length: \(body.count)\r\nConnection: close\r\n\r\n".utf8) + body
            listener.stateUpdateHandler = { state in
                if case .ready = state { ready.fulfill() }
            }
            listener.newConnectionHandler = { connection in
                connections.withLock { $0.append(connection) }
                connection.start(queue: queue)
                connection.receive(minimumIncompleteLength: 1, maximumLength: 65_536) { _, _, _, _ in
                    connection.send(content: response, completion: .contentProcessed { _ in connection.cancel() })
                }
            }
            listener.start(queue: queue)
            defer {
                listener.cancel()
                connections.withLock { values in values.forEach { $0.cancel() } }
            }
            await fulfillment(of: [ready], timeout: 5)
            let port = try XCTUnwrap(listener.port)
            let address = try BackendAddress.normalize("http://127.0.0.1:\(port.rawValue)")
            let configuration = BackendConfiguration(id: UUID(), name: "Local fixture", address: address)
            let api = BackendAPI(configuration: configuration, diagnostics: log)
            do {
                let result = try await api.request(["modems", "local-path-secret"], method: "GET", token: "local-token-secret",
                    query: [.init(name: "secret", value: "local-query-secret")])
                XCTAssertEqual(status, 200)
                XCTAssertEqual(result.data, body)
            } catch let error as BackendFailure { XCTAssertEqual(error.status, 503) }
            let data = try await log.snapshot()
            let events = try diagnosticEvents(data)
            XCTAssertEqual(events.map(\.event), ["request.started", status == 200 ? "request.completed" : "operation.failed"])
            XCTAssertEqual(events.last?.fields["status"], String(status))
            XCTAssertEqual(events.last?.fields["backendID"], configuration.id.uuidString)
            assertNoSecrets(data, ["local-path-secret", "local-token-secret", "local-query-secret", "local-response-secret"])
        }
    }

    func testActualTaskCancellationIsLoggedWithoutChangingCancellation() async throws {
        let started = expectation(description: "Transport started")
        let api = BackendAPI(configuration: try testConfiguration(), diagnostics: log, transport: { _ in
            started.fulfill()
            try await Task.sleep(for: .seconds(60))
            return BackendHTTPResponse(data: Data(), status: 200)
        })
        let task = Task { try await api.request(["app"], method: "GET") }
        await fulfillment(of: [started], timeout: 2)
        task.cancel()
        do { _ = try await task.value; XCTFail("Expected cancellation") }
        catch { XCTAssertTrue(error is CancellationError) }
        let events = try diagnosticEvents(await log.snapshot())
        XCTAssertEqual(events.last?.event, "operation.cancelled")
        XCTAssertEqual(events.last?.level, .info)
    }

    func testDecodeFailureKeepsTheRequestCorrelationWithoutItsBody() async throws {
        let api = BackendAPI(configuration: try testConfiguration(), diagnostics: log, transport: { _ in
            BackendHTTPResponse(data: Data("{\"secret\":\"decode-body-secret\"}".utf8), status: 200)
        })
        do { _ = try await api.app(); XCTFail("Expected decoding failure") }
        catch let error as BackendFailure { XCTAssertEqual(error.code, "invalid_response") }
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.map(\.event), ["request.started", "request.completed", "response.decode_failed"])
        XCTAssertEqual(events.last?.fields["requestID"], events.first?.fields["requestID"])
        XCTAssertEqual(events.last?.fields["errorCode"], "invalid_response")
        assertNoSecrets(data, ["decode-body-secret"])
    }

    func testEnablingDuringARequestDoesNotBackfillItsEvents() async throws {
        await log.setEnabled(false)
        let store = try XCTUnwrap(log)
        let api = BackendAPI(configuration: try testConfiguration(), diagnostics: store, transport: { _ in
            await store.setEnabled(true)
            return BackendHTTPResponse(data: Data(), status: 200)
        })
        _ = try await api.request(["app"], method: "GET")
        let data = try await log.snapshot()
        XCTAssertTrue(data.isEmpty)
    }

    func testPushGatewayIncludesHTTPAndDecodeFailuresWithoutInstallationSecrets() async throws {
        let identity = PushIdentity(id: "installation-secret", managementToken: "management-secret")
        for status in [200, 503] {
            try await log.clear()
            let gateway = PushGateway(diagnostics: log, transport: { _ in
                BackendHTTPResponse(data: Data("invalid-body-secret".utf8), status: status, requestID: "push-request")
            })
            do { _ = try await gateway.badge(identity: identity); XCTFail("Expected a failure") }
            catch {}
            let data = try await log.snapshot()
            let events = try diagnosticEvents(data)
            XCTAssertEqual(events.last?.fields["service"], "pushGateway")
            XCTAssertEqual(events.last?.fields["path"], "/push-installations/:id")
            XCTAssertEqual(events.last?.fields["status"], String(status))
            XCTAssertEqual(events.last?.event, status == 200 ? "response.decode_failed" : "operation.failed")
            assertNoSecrets(data, ["installation-secret", "management-secret", "invalid-body-secret"])
        }
        try await log.clear()
        let backendID = UUID()
        let binding = PushBinding(id: "binding-secret", backendID: "backend-secret", sendToken: "send-token-secret")
        let gateway = PushGateway(diagnostics: log, transport: { _ in BackendHTTPResponse(data: Data(), status: 204) })
        do { _ = try await gateway.register(binding: binding, identity: identity, backendID: backendID); XCTFail("Expected decoding failure") }
        catch {}
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.last?.fields["backendID"], backendID.uuidString)
        XCTAssertEqual(events.last?.fields["path"], "/push-subscriptions")
        assertNoSecrets(data, ["installation-secret", "management-secret", "binding-secret", "backend-secret", "send-token-secret"])
    }

    func testManagedOAuthUsesOperationNamesInsteadOfServerDefinedURLs() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [DiagnosticResponseProtocol.self]
        let backendID = UUID()
        let access = ManagedAccess(backendID: backendID, diagnostics: log, configuration: configuration)
        let origin = try XCTUnwrap(URL(string: "https://diagnostic.fixture"))
        for endpoint in ["token", "bad-json", "503"] {
            try await log.clear()
            let registration = AccessRegistration(resource: origin, issuer: origin, authorizationEndpoint: origin,
                tokenEndpoint: origin.appending(path: "endpoint-path-secret/\(endpoint)"), revocationEndpoint: nil, clientID: "client-secret")
            let credential = SessionCredential(accessToken: "old-access-secret", refreshToken: "old-refresh-secret", expiresAt: .distantPast, access: registration)
            do {
                let refreshed = try await access.refresh(credential)
                XCTAssertEqual(endpoint, "token")
                XCTAssertEqual(refreshed.accessToken, "access-body-secret")
            } catch { XCTAssertNotEqual(endpoint, "token") }
            let data = try await log.snapshot()
            let events = try diagnosticEvents(data)
            XCTAssertEqual(events.last?.fields["service"], "oauth")
            XCTAssertEqual(events.last?.fields["backendID"], backendID.uuidString)
            XCTAssertEqual(events.last?.fields["path"], "/oauth/token")
            if endpoint == "503" { XCTAssertEqual(events.last?.fields["status"], "503") }
            if endpoint == "bad-json" { XCTAssertEqual(events.last?.event, "response.decode_failed") }
            assertNoSecrets(data, ["endpoint-path-secret", "client-secret", "old-access-secret", "old-refresh-secret", "access-body-secret", "refresh-body-secret", "invalid-body-secret"])
        }
    }

    func testCarrierLogsNeverIncludeCapabilityOrResourceURLs() async throws {
        let root = try XCTUnwrap(URL(string: "https://diagnostic.fixture/api/v1/websheets/capability-path-secret"))
        let backendID = UUID()
        let loader = CarrierResourceLoader(root: root, backendID: backendID, diagnostics: log) {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.protocolClasses = [DiagnosticResponseProtocol.self]
            return configuration
        }
        for resource in ["resource-path-secret", "503"] {
            try await log.clear()
            var request = URLRequest(url: root.appending(path: resource).appending(queryItems: [.init(name: "token", value: "query-secret")]))
            request.httpMethod = "POST"
            request.httpBody = Data("request-body-secret".utf8)
            do {
                let response = try await loader.load(request, token: "carrier-token-secret")
                XCTAssertEqual(response.status, 200)
                XCTAssertNotEqual(resource, "503")
            } catch { XCTAssertEqual(resource, "503") }
            let data = try await log.snapshot()
            let events = try diagnosticEvents(data)
            XCTAssertEqual(events.count, 2)
            XCTAssertEqual(events.last?.fields["service"], "carrier")
            XCTAssertEqual(events.last?.fields["backendID"], backendID.uuidString)
            XCTAssertEqual(events.last?.fields["path"], "/websheets/:id/resources")
            XCTAssertEqual(events.last?.fields["status"], resource == "503" ? "503" : "200")
            assertNoSecrets(data, ["capability-path-secret", "resource-path-secret", "query-secret", "request-body-secret", "carrier-token-secret", "carrier-body-secret"])
        }
    }

    func testInjectedSocketDoesNotClaimAHandshakeAndRecordsOneTerminalFailure() async throws {
        let api = BackendAPI(configuration: try testConfiguration(), diagnostics: log, transport: { _ in
            BackendHTTPResponse(data: Data(), status: 200)
        }, socketFactory: { _ in
            BackendSocket(send: { _ in throw URLError(.networkConnectionLost) }, receive: { throw URLError(.cancelled) }, close: {})
        })
        let socket = try await api.socket(["modems", "device-secret", "calls"], token: "socket-token-secret")
        let connecting = try diagnosticEvents(await log.snapshot())
        XCTAssertEqual(connecting.map(\.event), ["socket.connecting"])
        do { try await socket.send(Data("socket-payload-secret".utf8)); XCTFail("Expected failure") }
        catch {}
        do { _ = try await socket.receive(); XCTFail("Expected failure") }
        catch {}
        socket.close()
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.map(\.event), ["socket.connecting", "socket.failed"])
        XCTAssertEqual(events.last?.fields["path"], "/modems/:id/calls")
        assertNoSecrets(data, ["device-secret", "socket-token-secret", "socket-payload-secret"])
    }

    func testSocketLifecycleOnlyReportsOneClose() async throws {
        let diagnostic = DiagnosticSocket(request: DiagnosticRequest(store: log, service: .backend, method: "GET", path: ["calls"], kind: .websocket))
        diagnostic.opened()
        diagnostic.closed(code: 1000)
        diagnostic.closed(code: 1001)
        diagnostic.failed(URLError(.cancelled))
        let events = try diagnosticEvents(await log.snapshot())
        XCTAssertEqual(events.map(\.event), ["socket.connecting", "socket.opened", "socket.closed"])
        XCTAssertEqual(events.last?.fields["closeCode"], "1000")
    }

    func testClosingSocketReportsCloseBeforeTransportCancellation() async throws {
        let diagnostic = DiagnosticSocket(request: DiagnosticRequest(store: log, service: .backend, method: "GET", path: ["calls"], kind: .websocket))
        let socket = BackendSocket(send: { _ in }, receive: { Data() }, close: {
            diagnostic.failed(URLError(.cancelled))
        }).recording(diagnostic)
        socket.close()
        let events = try diagnosticEvents(await log.snapshot())
        XCTAssertEqual(events.map(\.event), ["socket.connecting", "socket.closed"])
        XCTAssertEqual(events.last?.level, .info)
    }

    func testRawErrorsAndUnknownServerCodesCannotDiscloseSecrets() async throws {
        let failingURL = try XCTUnwrap(URL(string: "https://url-secret.example"))
        log.record(NSError(domain: "domain-secret", code: 1, userInfo: [NSLocalizedDescriptionKey: "description-secret", NSURLErrorFailingURLErrorKey: failingURL]),
                   category: "app", operation: "local.failed")
        log.record(BackendFailure(code: "code-secret", message: "message-secret"), category: "app", operation: "local.failed")
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.first?.fields["errorDomain"], "other")
        XCTAssertEqual(events.last?.fields["errorCode"], "local_error")
        assertNoSecrets(data, ["domain-secret", "description-secret", "url-secret", "code-secret", "message-secret"])
    }

    func testServerErrorCodesUseTheHTTPProblemCatalog() async throws {
        log.record(BackendFailure(status: 400, code: "common.cancel", message: "message-secret", source: .backend),
                   category: "http", operation: "remote.failed")
        log.record(BackendFailure(status: 429, code: "login_rate_limited", message: "message-secret", source: .backend),
                   category: "http", operation: "remote.failed")
        let data = try await log.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertEqual(events.first?.fields["errorCode"], "http_error")
        XCTAssertEqual(events.last?.fields["errorCode"], "login_rate_limited")
        assertNoSecrets(data, ["message-secret", "common.cancel"])
    }

    private func assertNoSecrets(_ data: Data, _ secrets: [String], file: StaticString = #filePath, line: UInt = #line) {
        let text = String(decoding: data, as: UTF8.self)
        for secret in secrets { XCTAssertFalse(text.contains(secret), "Log contains \(secret)", file: file, line: line) }
    }
}

// Only sessions constructed in these tests install this protocol. No network is used.
private final class DiagnosticResponseProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let url = request.url else { return }
        let status = url.lastPathComponent == "503" ? 503 : 200
        let body: Data
        if url.lastPathComponent == "token" {
            body = Data(#"{"access_token":"access-body-secret","refresh_token":"refresh-body-secret","token_type":"Bearer","expires_in":3600}"#.utf8)
        } else { body = Data("invalid-body-secret carrier-body-secret".utf8) }
        guard let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1",
                                            headerFields: ["X-Request-ID": "fixture-request", "Content-Type": "text/plain"]) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

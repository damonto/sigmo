import XCTest
import WebKit
import Synchronization
@testable import Sigmo

@MainActor
final class CarrierWebsheetTests: XCTestCase {
    func testServerBridgeAcknowledgesOneCallbackThroughNativeLoader() async throws {
        try await checkServerBridge(callbackStatus: 204, delivery: .submitted)
    }

    func testServerBridgeReportsFailedDeliveryWithoutNativeFallback() async throws {
        try await checkServerBridge(callbackStatus: 500, delivery: .failed)
    }

    func testCarrierAccessChallengeUsesBackendOriginAndPreservesErrorHeaders() async throws {
        for trustedMetadata in [true, false] {
            let stub = BackendStub()
            await stub.setRequired(true)
            let configuration = try testConfiguration("carrier-fixture.example")
            let credentials = MemoryCredentials()
            credentials.tokens[configuration.id] = storedCredential("carrier-test-token")
            let session = try testSession(stub, configuration: configuration, credentials: credentials)
            await session.refresh(force: true)
            let root = try XCTUnwrap(URL(string: "https://carrier-fixture.example/prefix/api/v1/websheets/" + UUID().uuidString))
            let host = trustedMetadata ? "carrier-fixture.example" : "other.example"
            let challenge = "Bearer resource_metadata=\"https://\(host)/.well-known/oauth-protected-resource\""
            CarrierBridgeFixtureProtocol.install(root: root, html: "Access denied", callbackStatus: 204, rootStatus: 401,
                headers: ["WWW-Authenticate": challenge, "X-Request-ID": "carrier-request", "Retry-After": "120", "Allow": "GET"])
            defer { CarrierBridgeFixtureProtocol.remove(root: root) }
            let loader = CarrierResourceLoader(root: root) {
                let configuration = URLSessionConfiguration.ephemeral
                configuration.protocolClasses = [CarrierBridgeFixtureProtocol.self]
                return configuration
            }
            let request = try loader.request(URLRequest(url: CarrierResourceLoader.localURL(root)))
            do {
                _ = try await session.protectedRequest { _, token in try await loader.load(request, token: token) }
                XCTFail("Expected the carrier resource to require authentication")
            } catch let failure as BackendFailure {
                if trustedMetadata {
                    XCTAssertEqual(failure.code, "status.loginRequired")
                } else {
                    XCTAssertEqual(failure.status, 401)
                    XCTAssertEqual(failure.authenticate, challenge)
                    XCTAssertEqual(failure.requestID, "carrier-request")
                    XCTAssertEqual(failure.retryAfter, "120")
                    XCTAssertEqual(failure.allow, "GET")
                }
            }
            if trustedMetadata {
                XCTAssertEqual(session.authentication, .required)
                XCTAssertTrue(session.supportsBrowserLogin)
                XCTAssertNil(credentials.tokens[configuration.id])
                XCTAssertTrue(session.modems.isEmpty)
            } else {
                XCTAssertTrue(session.canAccessDevices)
                XCTAssertNotNil(credentials.tokens[configuration.id])
            }
        }
    }

    private func checkServerBridge(callbackStatus: Int, delivery: CarrierCallbackDelivery.Status) async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let configuration = try testConfiguration("carrier-fixture.example")
        let credentials = MemoryCredentials()
        credentials.tokens[configuration.id] = storedCredential("carrier-test-token")
        let session = try testSession(stub, configuration: configuration, credentials: credentials)
        await session.refresh(force: true)
        let id = UUID().uuidString
        let url = try XCTUnwrap(URL(string: "https://carrier-fixture.example/prefix/api/v1/websheets/" + id))
        let fixture = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "carrier_bridge", withExtension: "js"))
        var script = try String(contentsOf: fixture, encoding: .utf8)
        for (name, value) in [
            ("CALLBACK_URL", "https://carrier-fixture.example/api/v1/websheets/" + id + "/callback"),
            ("WFC_CLIENT_NAME", ""),
            ("ABSOLUTE_PATH_PROXY_PREFIX", "/api/v1/websheets/" + id + "/proxy")
        ] {
            let literal = String(decoding: try JSONEncoder().encode(value), as: UTF8.self)
            script = script.replacingOccurrences(of: "{{" + name + "}}", with: literal)
        }
        let html = """
        <!doctype html><html><body><script>\(script)</script><script>
        window.addEventListener('load', () => window.VoWiFiWebServiceFlow.entitlementChanged());
        </script></body></html>
        """
        CarrierBridgeFixtureProtocol.install(root: url, html: html, callbackStatus: callbackStatus)
        defer { CarrierBridgeFixtureProtocol.remove(root: url) }
        let received = expectation(description: "Real server bridge callback")
        var failures: [BackendFailure] = []
        let parent = CarrierWebsheetView(url: url, callback: { value in
            XCTAssertEqual(value.callback.event, "entitlementChanged")
            XCTAssertEqual(value.callback.source, "vowifi")
            XCTAssertEqual(value.status, delivery)
            received.fulfill()
        }, failure: { failures.append($0) }, session: session)
        let loader = CarrierResourceLoader(root: url) {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.protocolClasses = [CarrierBridgeFixtureProtocol.self]
            return configuration
        }
        let coordinator = CarrierWebsheetView.Coordinator(parent, resources: loader)
        let webView = parent.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        webView.load(URLRequest(url: CarrierResourceLoader.localURL(url)))
        await fulfillment(of: [received], timeout: 15)
        XCTAssertEqual(CarrierBridgeFixtureProtocol.callbackCount(root: url), 1)
        if delivery == .submitted { XCTAssertTrue(failures.isEmpty) }
        else { XCTAssertEqual(failures.first?.status, callbackStatus) }
    }

    func testNativeLoaderSupportsCarrierFetchAndXHRWithCredentials() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let configuration = try testConfiguration("carrier-fixture.example")
        let credentials = MemoryCredentials()
        credentials.tokens[configuration.id] = storedCredential("carrier-test-token")
        let session = try testSession(stub, configuration: configuration, credentials: credentials)
        await session.refresh(force: true)
        let url = try XCTUnwrap(URL(string: "https://carrier-fixture.example/prefix/api/v1/websheets/secret"))
        let completed = expectation(description: "Carrier network operations complete")
        let parent = CarrierWebsheetView(url: url, callback: { value in
            XCTAssertEqual(value.callback.event, "finishFlow")
            XCTAssertEqual(value.status, .native)
            completed.fulfill()
        }, failure: { failure in
            XCTFail(failure.message)
            completed.fulfill()
        }, session: session)
        let loader = CarrierResourceLoader(root: url) {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.protocolClasses = [CarrierFixtureProtocol.self]
            return configuration
        }
        let coordinator = CarrierWebsheetView.Coordinator(parent, resources: loader)
        let webView = parent.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        webView.load(URLRequest(url: CarrierResourceLoader.localURL(url)))
        await fulfillment(of: [completed], timeout: 15)
    }

    func testLocalCarrierPageRelaysTypedCallback() async throws {
        let received = expectation(description: "Native callback")
        var payload: CarrierCallback?
        let url = try XCTUnwrap(URL(string: "https://fixture.example/prefix/api/v1/websheets/local"))
        let html = #"""
        <!doctype html><html><body><p>Carrier fixture</p><script>
        window.addEventListener('load', () => {
          window.postMessage({type:'sigmo-websheet-callback',callback:{event:'profileReadyWithActivationCode',activationCode:'fixture-code',iccid:'8900'}}, '*');
        });
        </script></body></html>
        """#
        let session = try testSession(BackendStub())
        await session.refresh(force: true)
        let parent = CarrierWebsheetView(url: url, callback: { value in
            payload = value.callback
            XCTAssertEqual(value.status, .native)
            received.fulfill()
        }, failure: { failure in XCTFail(failure.message) }, fixtureHTML: html, session: session)
        let coordinator = parent.makeCoordinator()
        let webView = parent.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        webView.load(URLRequest(url: CarrierResourceLoader.localURL(url)))
        await fulfillment(of: [received], timeout: 10)
        XCTAssertEqual(payload?.event, "profileReadyWithActivationCode")
        XCTAssertEqual(payload?.activationCode, "fixture-code")
        XCTAssertEqual(payload?.iccid, "8900")
        XCTAssertFalse(webView.configuration.websiteDataStore.isPersistent)
    }
}

private final class CarrierBridgeFixtureProtocol: URLProtocol, @unchecked Sendable {
    private struct Fixture: Sendable {
        let html: String
        let callbackStatus: Int
        let rootStatus: Int
        let headers: [String: String]
        var callbacks = 0
    }
    private static let fixtures = Mutex<[String: Fixture]>([:])

    static func install(root: URL, html: String, callbackStatus: Int, rootStatus: Int = 200, headers: [String: String] = [:]) {
        fixtures.withLock {
            $0[root.lastPathComponent] = Fixture(html: html, callbackStatus: callbackStatus, rootStatus: rootStatus, headers: headers)
        }
    }
    static func remove(root: URL) { fixtures.withLock { _ = $0.removeValue(forKey: root.lastPathComponent) } }
    static func callbackCount(root: URL) -> Int { fixtures.withLock { $0[root.lastPathComponent]?.callbacks ?? 0 } }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let url = request.url else { return }
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer carrier-test-token")
        let callback = url.lastPathComponent == "callback"
        let id = (callback ? url.deletingLastPathComponent() : url).lastPathComponent
        let result: (body: String, status: Int, headers: [String: String])? = Self.fixtures.withLock { fixtures in
            guard let fixture = fixtures[id] else { return nil }
            if callback {
                fixtures[id]?.callbacks += 1
                return ("", fixture.callbackStatus, fixture.headers)
            }
            return (fixture.html, fixture.rootStatus, fixture.headers)
        }
        guard let result else {
            client?.urlProtocol(self, didFailWithError: URLError(.resourceUnavailable))
            return
        }
        if callback { XCTAssertEqual(request.httpMethod, "POST") }
        var headers = result.headers
        headers["Content-Type"] = callback ? "text/plain" : "text/html"
        guard let response = HTTPURLResponse(url: url, statusCode: result.status, httpVersion: "HTTP/1.1",
            headerFields: headers) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(result.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

// Only the loader's isolated URLSession uses this protocol; no real backend is contacted.
private final class CarrierFixtureProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let url = request.url else { return }
        let authorized = request.value(forHTTPHeaderField: "Authorization") == "Bearer carrier-test-token"
        XCTAssertNil(request.value(forHTTPHeaderField: "Cookie"))
        if url.lastPathComponent == "xhr" {
            XCTAssertEqual(request.httpMethod, "POST")
            var body = request.httpBody ?? Data()
            if let stream = request.httpBodyStream {
                stream.open()
                defer { stream.close() }
                var buffer = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&buffer, maxLength: buffer.count)
                    if count <= 0 { break }
                    body.append(contentsOf: buffer.prefix(count))
                }
            }
            XCTAssertEqual(String(data: body, encoding: .utf8), #"{"example":true}"#)
        }
        let html = url.lastPathComponent == "secret"
        let body = html ? #"""
        <!doctype html><html><body><script>
        const base = 'https://' + location.host + '/api/v1/websheets/secret/proxy';
        async function run() {
          const response = await fetch(base + '/fetch');
          if (await response.text() !== 'carrier-ok') throw new Error('fetch response');
          await new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open('POST', base + '/xhr'); xhr.withCredentials = true;
            xhr.setRequestHeader('Content-Type', 'application/json');
            xhr.onload = () => xhr.responseText === 'carrier-ok' ? resolve() : reject(new Error('xhr response'));
            xhr.onerror = () => reject(new Error('xhr network'));
            xhr.send(JSON.stringify({example: true}));
          });
          window.postMessage({type:'sigmo-websheet-callback',callback:{event:'finishFlow'}}, '*');
        }
        window.addEventListener('load', () => run().catch(error => {
          window.postMessage({type:'sigmo-websheet-callback',callback:{event:String(error)}}, '*');
        }));
        </script></body></html>
        """# : "carrier-ok"
        guard let response = HTTPURLResponse(url: url, statusCode: authorized ? 200 : 401, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": html ? "text/html" : "text/plain"]) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

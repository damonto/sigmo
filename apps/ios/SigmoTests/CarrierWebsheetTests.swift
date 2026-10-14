import XCTest
import WebKit
@testable import Sigmo

@MainActor
final class CarrierWebsheetTests: XCTestCase {
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
            XCTAssertEqual(value.event, "finishFlow")
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
        let parent = CarrierWebsheetView(url: url, callback: { value in
            payload = value
            received.fulfill()
        }, failure: { failure in XCTFail(failure.message) })
        let coordinator = parent.makeCoordinator()
        let webView = parent.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        let html = #"""
        <!doctype html><html><body><p>Carrier fixture</p><script>
        window.addEventListener('load', () => {
          window.postMessage({type:'sigmo-websheet-callback',callback:{event:'profileReadyWithActivationCode',activationCode:'fixture-code',iccid:'8900'}}, location.origin);
        });
        </script></body></html>
        """#
        webView.loadHTMLString(html, baseURL: url)
        await fulfillment(of: [received], timeout: 10)
        XCTAssertEqual(payload?.event, "profileReadyWithActivationCode")
        XCTAssertEqual(payload?.activationCode, "fixture-code")
        XCTAssertEqual(payload?.iccid, "8900")
        XCTAssertFalse(webView.configuration.websiteDataStore.isPersistent)
    }
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

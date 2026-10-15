import XCTest
@testable import Sigmo

final class HTTPProblemTests: XCTestCase {
    @MainActor
    func testSuccessfulResponseLimitChecksDeclaredAndStreamingSizes() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SizedResponseProtocol.self]
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        for declared in [false, true] {
            for size in [31, 32, 33] {
                let url = try XCTUnwrap(URL(string: "https://response.test/200/\(size)?declared=\(declared)"))
                do {
                    let response = try await BackendHTTPResponse.receive(URLRequest(url: url), session: session, maximumBytes: 32)
                    XCTAssertLessThanOrEqual(size, 32)
                    XCTAssertEqual(response.data.count, size)
                } catch let error as BackendFailure {
                    XCTAssertEqual(size, 33)
                    XCTAssertEqual(error.code, "response_too_large")
                }
            }
        }
    }

    @MainActor
    func testOversizedErrorRetainsHTTPMeaningDespiteSmallSuccessLimit() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SizedResponseProtocol.self]
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        let url = try XCTUnwrap(URL(string: "https://response.test/401/\(HTTPProblem.maxBytes + 1)?declared=true"))
        let response = try await BackendHTTPResponse.receive(URLRequest(url: url), session: session, maximumBytes: 32)
        XCTAssertEqual(response.status, 401)
        XCTAssertEqual(response.authenticate, "Bearer test")
        XCTAssertTrue(response.data.isEmpty)
    }

    @MainActor
    func testInjectedTransportUsesJSONAndArchiveResponseLimits() async throws {
        let body = Data(repeating: 0, count: 8 * 1024 * 1024 + 1)
        let api = BackendAPI(configuration: try testConfiguration(), transport: { _ in
            BackendHTTPResponse(data: body, status: 200)
        })
        do {
            _ = try await api.request(["settings"], method: "GET")
            XCTFail("Oversized JSON must be rejected before decoding")
        } catch let error as BackendFailure {
            XCTAssertEqual(error.code, "response_too_large")
        }
        let archive = try await api.skill(token: nil)
        XCTAssertEqual(archive.count, body.count)
    }

    private struct Fixture: Decodable {
        let name: String
        let status: Int
        let source: String
        let headers: [String: String]
        let body: String
        let `repeat`: Int
        let code: String
        let loginInvalid: Bool
    }

    func testSharedContractAndAuthenticationIsolation() throws {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "HTTPProblems", withExtension: "json"))
        let fixtures = try JSONDecoder().decode([Fixture].self, from: Data(contentsOf: url))
        let now = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-09T00:00:00Z"))
        for fixture in fixtures {
            let body = fixture.repeat > 0 ? String(repeating: fixture.body, count: fixture.repeat) : fixture.body
            let response = BackendHTTPResponse(data: Data(body.utf8), status: fixture.status,
                requestID: fixture.headers["X-Request-ID"], contentType: fixture.headers["Content-Type"],
                retryAfter: fixture.headers["Retry-After"], allow: fixture.headers["Allow"],
                authenticate: fixture.headers["WWW-Authenticate"])
            let source = try XCTUnwrap(FailureSource(rawValue: fixture.source))
            let error = HTTPProblem.failure(response, source: source, now: now)
            XCTAssertEqual(error.status, fixture.status, fixture.name)
            XCTAssertEqual(error.code, fixture.code.isEmpty ? "http_\(fixture.status)" : fixture.code, fixture.name)
            XCTAssertEqual(error.isLocalAuthenticationFailure, fixture.loginInvalid, fixture.name)
            XCTAssertEqual(error.requestID, fixture.headers["X-Request-ID"], fixture.name)
            XCTAssertEqual(error.allow, fixture.headers["Allow"], fixture.name)
            XCTAssertEqual(error.authenticate, fixture.headers["WWW-Authenticate"], fixture.name)
            XCTAssertEqual(error.retryAfter, fixture.headers["Retry-After"], fixture.name)
            if fixture.name == "numeric retry after" || fixture.name == "HTTP date retry after" {
                XCTAssertEqual(error.retryAt, now.addingTimeInterval(90), fixture.name)
            }
        }
    }

    func testRetryAfterRejectsNonHTTPValues() throws {
        let now = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-09T00:00:00Z"))
        for value in ["", "-1", "+1", "1.5", "2147483648", "2026-10-10", "Thu, 08 Oct 2026 00:00:00 GMT"] {
            XCTAssertNil(HTTPProblem.retryDate(value, now: now), value)
        }
        XCTAssertEqual(HTTPProblem.retryDate(" 90 ", now: now), now.addingTimeInterval(90))
    }

    func testGatewayRetainsMetadataAndItsOwnSource() async throws {
        var response = HTTPProblem.response(status: 429, code: "push_rate_limited", detail: "Wait", requestID: "gateway-request")
        response.retryAfter = "90"
        response.allow = "POST, OPTIONS"
        let fixture = response
        let gateway = PushGateway(address: URL(string: "https://gateway.example"), transport: { request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json, application/problem+json")
            return fixture
        })
        do {
            _ = try await gateway.badge(identity: PushIdentity(id: "installation", managementToken: "test-token"))
            XCTFail("expected rate limit")
        } catch let failure as BackendFailure {
            XCTAssertEqual(failure.source, .pushGateway)
            XCTAssertEqual(failure.code, "push_rate_limited")
            XCTAssertEqual(failure.requestID, "gateway-request")
            XCTAssertEqual(failure.allow, "POST, OPTIONS")
            XCTAssertNotNil(failure.retryAt)
            XCTAssertFalse(failure.isLocalAuthenticationFailure)
        }
    }

    @MainActor
    func testUnrelated401DoesNotRemoveLocalLogin() async throws {
        let cases: [(Int, String, FailureSource)] = [
            (401, "invalid_otp", .backend), (401, "license_lease_expired", .backend),
            (401, "invalid_session", .pushGateway), (503, "invalid_session", .backend),
        ]
        for (status, code, source) in cases {
            let stub = BackendStub()
            await stub.setRequired(true)
            let config = try testConfiguration()
            let credentials = MemoryCredentials()
            credentials.tokens[config.id] = storedCredential("login-token")
            let session = try testSession(stub, configuration: config, credentials: credentials)
            await session.refresh(force: true)
            let failure = HTTPProblem.failure(HTTPProblem.response(status: status, code: code, detail: "Rejected"), source: source)
            do {
                let _: String = try await session.protectedRequest { _, _ in throw failure }
                XCTFail("expected request failure")
            } catch let error as BackendFailure {
                XCTAssertEqual(error, failure)
            }
            XCTAssertEqual(credentials.tokens[config.id], storedCredential("login-token"), code)
            XCTAssertEqual(session.authentication, .authenticated, code)
        }
    }
}

// Only the test's isolated URLSession uses this protocol; no network is involved.
private final class SizedResponseProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let url = request.url else { return }
        let path = url.pathComponents
        guard path.count == 3, let status = Int(path[1]), let size = Int(path[2]) else { return }
        var headers = ["Content-Type": "application/json", "WWW-Authenticate": "Bearer test"]
        if url.query == "declared=true" { headers["Content-Length"] = String(size) }
        guard let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers) else { return }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(repeating: 0, count: size))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

import XCTest
@testable import Sigmo

final class HTTPProblemTests: XCTestCase {
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
            (401, "invalid_token", .pushGateway), (503, "invalid_token", .backend),
        ]
        for (status, code, source) in cases {
            let stub = BackendStub()
            await stub.setRequired(true)
            let config = try testConfiguration()
            let credentials = MemoryCredentials()
            credentials.tokens[config.id] = "login-token"
            let session = try testSession(stub, configuration: config, credentials: credentials)
            await session.refresh(force: true)
            let failure = HTTPProblem.failure(HTTPProblem.response(status: status, code: code, detail: "Rejected"), source: source)
            do {
                let _: String = try await session.protectedRequest { _, _ in throw failure }
                XCTFail("expected request failure")
            } catch let error as BackendFailure {
                XCTAssertEqual(error, failure)
            }
            XCTAssertEqual(credentials.tokens[config.id], "login-token", code)
            XCTAssertEqual(session.authentication, .authenticated, code)
        }
    }
}

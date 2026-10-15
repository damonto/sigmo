import XCTest
@testable import Sigmo

final class BackendAPITests: XCTestCase {
    @MainActor
    func testModemReadsAndESIMWritesAllowLongRequests() async throws {
        let server = DetailFixtureServer()
        let api = BackendAPI(configuration: try testConfiguration()) { try await server.respond($0) }
        _ = try await api.modem("device-1", token: nil)
        try await api.detailMutation("device-1", path: ["ses", "se-a", "esims", "8986000000000000002", "activation"],
                                     method: "PUT", token: nil)
        try await api.detailMutation("device-1", path: ["ses", "se-a", "esims", "8986000000000000002", "nickname"],
                                     method: "PUT", value: ["nickname": "Travel"], token: nil)
        try await api.detailMutation("device-1", path: ["ses", "se-b", "esims", "8986000000000000002"],
                                     method: "DELETE", token: nil)
        _ = try await api.discoverProfiles("device-1", seID: "se-a", token: nil)

        let requests = await server.allRequests()
        XCTAssertEqual(requests.count, 5)
        for request in requests {
            XCTAssertEqual(request.timeoutInterval, 600, request.url?.path ?? "")
        }
    }

    func testDisplayAddressPreservesServerPathAndRoundTripsToAPI() throws {
        for (input, display) in [
            ("https://example.com/api/v1", "https://example.com"),
            ("https://example.com/sigmo/api/v1", "https://example.com/sigmo"),
            ("https://example.com/sigmo%20home/api/v1", "https://example.com/sigmo%20home"),
            ("http://[::1]:9527/api/v1", "http://[::1]:9527"),
            ("http://sigmo.local:9527", "http://sigmo.local:9527")
        ] {
            let endpoint = try BackendAddress.normalize(input)
            let configuration = BackendConfiguration(name: "Home", address: endpoint)
            XCTAssertEqual(configuration.displayAddress, display)
            XCTAssertEqual(try BackendAddress.normalize(configuration.displayAddress), endpoint)
        }
    }

    func testAuthorizationStatusTranslationsIncludeFallbacks() throws {
        for (language, pairingTitles, licenseTitles) in [
            ("en", ["Waiting for authorization", "Authorized", "Pairing expired. Create a new pairing to continue.", "Not checked"],
             ["Active", "Revoked", "Expired", "Unknown"]),
            ("zh-Hans", ["等待授权", "已授权", "配对已过期，请创建新配对以继续。", "尚未检查"],
             ["有效", "已撤销", "已过期", "未知"])
        ] {
            let path = try XCTUnwrap(Bundle.main.path(forResource: language, ofType: "lproj"))
            let bundle = try XCTUnwrap(Bundle(path: path))
            for (status, expected) in zip(["pending", "active", "expired", "future"], pairingTitles) {
                let pairing = LicensePairing(id: "pair", activationUrl: "", status: status, expiresAt: .distantFuture)
                XCTAssertEqual(bundle.localizedString(forKey: pairing.statusKey, value: nil, table: nil), expected)
            }
            for (status, expected) in zip(["active", "revoked", "expired", "future"], licenseTitles) {
                let license = UpdateLicense(status: status, deviceId: "device", telegramId: 1, displayName: "Fixture",
                                            username: nil, expiresAt: nil, offlineUntil: nil)
                XCTAssertEqual(bundle.localizedString(forKey: license.statusKey, value: nil, table: nil), expected)
            }
        }
    }

    func testAddressNormalization() throws {
        let cases = [
            (" HTTPS://Example.COM:443/ ", "https://example.com/api/v1"),
            ("https://example.com/sigmo/", "https://example.com/sigmo/api/v1"),
            ("https://example.com/sigmo/api/v1///", "https://example.com/sigmo/api/v1"),
            ("http://10.10.10.101:9527", "http://10.10.10.101:9527/api/v1"),
            ("http://sigmo.local", "http://sigmo.local/api/v1"),
            ("http://sigmo:9527", "http://sigmo:9527/api/v1"),
            ("http://[::1]:9527", "http://[::1]:9527/api/v1"),
        ]
        for (input, expected) in cases { XCTAssertEqual(try BackendAddress.normalize(input).absoluteString, expected, input) }
        for input in ["example.com", "ftp://example.com", "http://example.com", "https://u:p@example.com", "https://example.com?q=1", "https://example.com#x", "https://example.com:99999", "https://example.com/../api/v1"] {
            XCTAssertThrowsError(try BackendAddress.normalize(input), input)
        }
    }

    func testTelegramLinkValidation() throws {
        let expiry = Date().addingTimeInterval(60)
        let good = LicensePairing(id: "pair", activationUrl: "https://t.me/SigmoProBot?start=pair", status: "pending", expiresAt: expiry)
        XCTAssertEqual(try good.validatedURL().host, "t.me")
        for url in ["http://t.me/SigmoProBot?start=pair", "https://evil.example?start=pair", "https://t.me/SigmoProBot?start=other", "https://t.me/SigmoProBot?start=pair&x=y", "https://u:p@t.me/SigmoProBot?start=pair", "https://t.me/notabot/path?start=pair"] {
            XCTAssertThrowsError(try LicensePairing(id: "pair", activationUrl: url, status: "pending", expiresAt: expiry).validatedURL())
        }
    }

    func testRedirectsDoNotForwardAcrossOriginsOrDowngrade() async throws {
        let origin = try BackendAddress.normalize("https://one.example")
        let delegate = BackendRedirects(origin: origin)
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let response = try XCTUnwrap(HTTPURLResponse(url: origin, statusCode: 302, httpVersion: nil, headerFields: nil))
        for (target, allowed) in [("https://one.example/next", true), ("https://two.example/next", false), ("http://one.example/next", false), ("https://one.example:444/next", false)] {
            let url = try XCTUnwrap(URL(string: target))
            var request = URLRequest(url: url)
            request.setValue("Bearer secret", forHTTPHeaderField: "Authorization")
            let result: URLRequest? = await withCheckedContinuation { continuation in
                delegate.urlSession(session, task: session.dataTask(with: origin), willPerformHTTPRedirection: response,
                                    newRequest: request) { continuation.resume(returning: $0) }
            }
            XCTAssertEqual(result != nil, allowed, target)
        }
    }

    @MainActor
    func testTokensOnlyGoToProtectedRoutesAndResourceIDsAreEscaped() async throws {
        let stub = BackendStub()
        let config = try testConfiguration()
        let api = BackendAPI(configuration: config) { try await stub.respond($0) }
        _ = try await api.app()
        _ = try await api.capabilities()
        _ = try await api.modems(token: "private-token")
        // The stub returns a different ID, which must also be rejected by the decoder.
        do { _ = try await api.modem("one/two ?", token: "private-token"); XCTFail("expected mismatched resource") }
        catch let error as BackendFailure { XCTAssertEqual(error.code, "api.invalidResponse") }
        let requests = await stub.allRequests()
        XCTAssertNil(requests[0].value(forHTTPHeaderField: "Authorization"))
        XCTAssertNil(requests[1].value(forHTTPHeaderField: "Authorization"))
        XCTAssertEqual(requests[2].value(forHTTPHeaderField: "Authorization"), "Bearer private-token")
        XCTAssertTrue(requests[3].url!.absoluteString.hasSuffix("/modems/one%2Ftwo%20%3F"))
    }

    @MainActor
    func testResourceIDsCannotTraverseTheAPIPath() throws {
        let api = BackendAPI(configuration: try testConfiguration())
        for id in [".", ".."] {
            XCTAssertThrowsError(try api.makeRequest(["contacts", id], method: "DELETE"))
        }
        for (id, encoded) in [("a.b", "a.b"), ("../settings", "..%2Fsettings"), ("%2e%2e", "%252e%252e")] {
            let request = try api.makeRequest(["contacts", id], method: "DELETE")
            XCTAssertTrue(try XCTUnwrap(request.url).absoluteString.hasSuffix("/contacts/" + encoded))
        }
    }

    @MainActor
    func testSettingsFixtureDoesNotInterceptDeviceStatus() async throws {
        let fixture = PreviewBackendServer()
        let config = try testConfiguration("home.sigmo.test")
        let api = BackendAPI(configuration: config) { try await fixture.respond($0) }
        let modem = try await api.modem("device-1", token: nil)
        XCTAssertTrue(modem.wifiCallingConnected == true)
        XCTAssertEqual(modem.volteStatus, .connected)
    }

    func testIMSStateDecodingPreservesMissingAndDisabledValues() throws {
        let cases: [(Bool?, Bool?, IMSConnectionState)] = [
            (false, false, .disabled), (false, true, .disabled), (false, nil, .disabled),
            (true, true, .connected), (true, false, .disconnected), (true, nil, .unknown),
            (nil, true, .connected), (nil, false, .unknown), (nil, nil, .unknown),
        ]
        let original = PreviewBackendServer.modemJSON()
        for (enabled, connected, expected) in cases {
            for service in ["wifiCalling", "volte"] {
                let json = original
                    .replacingOccurrences(of: "\"\(service)Enabled\":true", with: "\"\(service)Enabled\":\(enabled.map(String.init) ?? "null")")
                    .replacingOccurrences(of: "\"\(service)Connected\":false", with: "\"\(service)Connected\":\(connected.map(String.init) ?? "null")")
                let modem = try JSONDecoder().decode(Modem.self, from: Data(json.utf8))
                XCTAssertEqual(service == "volte" ? modem.volteStatus : modem.wifiCallingStatus, expected, json)
            }
        }
        let oldResponse = original.replacingOccurrences(of: ",\"volteEnabled\":true,\"volteConnected\":false", with: "")
        let modem = try JSONDecoder().decode(Modem.self, from: Data(oldResponse.utf8))
        XCTAssertNil(modem.volteEnabled)
        XCTAssertNil(modem.volteConnected)
        XCTAssertEqual(modem.volteStatus, .unknown)
    }

    func testNullOptionalFieldsDecodeWithoutInventingStates() throws {
        let modem = try JSONDecoder().decode(Modem.self, from: Data(PreviewBackendServer.modemJSON().utf8))
        XCTAssertNil(modem.slots)
        XCTAssertEqual(modem.simKind, "euicc")
        let unknown = PreviewBackendServer.modemJSON().replacingOccurrences(of: #""simKind":"euicc""#, with: #""simKind":"futureSIM""#)
            .replacingOccurrences(of: #""accessTechnology":"lte""#, with: #""accessTechnology":null"#)
        let future = try JSONDecoder().decode(Modem.self, from: Data(unknown.utf8))
        XCTAssertNil(future.accessTechnology)
        XCTAssertEqual(future.simKind, "futureSIM")
    }
}

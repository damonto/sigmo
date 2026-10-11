import XCTest
@testable import Sigmo

@MainActor
final class SettingsTests: XCTestCase {
    func testNativeSettingsWireContractAndBinaryDownload() async throws {
        let server = try SettingsTestServer()
        let configuration = BackendConfiguration(name: "Prefix", address: try BackendAddress.normalize("https://settings.example/sigmo"))
        let api = BackendAPI(configuration: configuration) { try await server.respond($0) }
        let settings = try await api.settings(token: "fixture-token")
        XCTAssertEqual(settings.schema.channels.count, 8)
        XCTAssertEqual(settings.values.proxy["httpPort"], .number(8080))
        _ = try await api.saveSettings(.proxy, fields: settings.values.proxy, token: "fixture-token")
        try await api.testAuthentication(["telegram"], token: "fixture-token")
        _ = try await api.mcpSettings(token: "fixture-token")
        _ = try await api.saveMCP(true, token: "fixture-token")
        _ = try await api.mcpKeys(token: "fixture-token")
        let created = try await api.createMCPKey(.init(name: "Test", permissions: ["modem.read"]), token: "fixture-token")
        try await api.revokeMCPKey(created.apiKey.id, token: "fixture-token")
        _ = try await api.mcpAudit(token: "fixture-token")
        let zip = try await api.skill(token: "fixture-token")
        XCTAssertEqual(Array(zip.prefix(4)), [0x50, 0x4b, 0x05, 0x06])
        _ = try await api.updates(token: "fixture-token")
        _ = try await api.saveUpdates(.init(automatic: true, channel: "dev"), token: "fixture-token")
        _ = try await api.checkUpdates(token: "fixture-token")
        _ = try await api.installUpdate(token: "fixture-token")
        _ = try await api.installation(token: "fixture-token")
        let requests = await server.allRequests()
        XCTAssertTrue(requests.allSatisfy { $0.url!.path.hasPrefix("/sigmo/api/v1/") })
        XCTAssertTrue(requests.allSatisfy { $0.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token" })
        XCTAssertEqual(requests.map(\.httpMethod), ["GET", "PUT", "POST", "GET", "PUT", "GET", "POST", "DELETE", "GET", "GET", "GET", "PUT", "POST", "POST", "GET"])
        XCTAssertEqual(requests[8].url?.query, "limit=50")
        XCTAssertEqual(requests[9].value(forHTTPHeaderField: "Accept"), "application/zip, \(HTTPProblem.mediaType)")
        let payload = try JSONDecoder().decode(SettingsFields.self, from: XCTUnwrap(requests[1].httpBody))
        XCTAssertEqual(payload["httpPort"], .number(8080))
    }

    func testNullAndOmittedCollectionsDecode() throws {
        let json = SettingsFixtureData.settings.replacingOccurrences(of: #""authProviders": ["#, with: #""unusedProviders": ["#)
        var response = try JSONDecoder().decode(SettingsResponse.self, from: Data(json.utf8))
        response.values.auth["authProviders"] = .null
        XCTAssertEqual(response.values.auth["authProviders"]?.strings, [])
        let keys = try JSONDecoder().decode(MCPKeys.self, from: Data(#"{"apiKeys":null}"#.utf8))
        XCTAssertNil(keys.apiKeys)
        let audit = try JSONDecoder().decode(MCPAudit.self, from: Data(#"{}"#.utf8))
        XCTAssertNil(audit.events)
        for raw in [#"{"enabled":false}"#, #"{"enabled":false,"recipients":null,"headers":null}"#] {
            let channel = try JSONDecoder().decode(SettingsFields.self, from: Data(raw.utf8))
            XCTAssertFalse(channel["enabled"]?.bool ?? true)
            XCTAssertEqual(channel["recipients"]?.strings ?? [], [])
        }
    }

    func testSchemaValidationAndPayloadNormalization() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await state.load()
        for value in ["", "0", "65536", "1.5", "abc"] {
            var proxy = state.proxy; proxy["httpPort"] = .text(value)
            XCTAssertNotNil(state.validation(fields: state.snapshot!.schema.proxy, values: proxy), value)
        }
        state.proxy["httpPort"] = .text("9090")
        let payload = SettingsState.payload(state.proxy, fields: state.snapshot!.schema.proxy)
        XCTAssertEqual(payload["httpPort"], .number(9090))
        state.auth["otpRequired"] = .toggle(true); state.auth["authProviders"] = .list([])
        XCTAssertNotNil(state.validation(for: .authentication))
        state.auth["authProviders"] = .list(["telegram"])
        for value in ["0", "181", "2.5"] { state.auth["tokenValidityDays"] = .text(value); XCTAssertNotNil(state.validation(for: .authentication)) }
        state.auth["tokenValidityDays"] = .text("180"); XCTAssertNil(state.validation(for: .authentication))
        let emailFields = state.channelSchemas.first { $0.key == "email" }!.fields
        XCTAssertNil(state.validation(fields: emailFields, values: ["enabled": .toggle(false)], enabled: false))
        let defaults = SettingsState.payload(["enabled": .toggle(false)], fields: emailFields)
        XCTAssertEqual(defaults["tlsPolicy"], .text("mandatory"))
        XCTAssertEqual(defaults["ssl"], .toggle(false))
    }

    func testFailedSavePreservesDraftAndUnrelatedDrafts() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await state.load()
        state.proxy["httpPort"] = .text("9090")
        state.auth["tokenValidityDays"] = .text("45")
        await server.setError("/settings/proxy", status: 500)
        await state.save(.proxy)
        XCTAssertEqual(state.proxy["httpPort"], .text("9090"))
        XCTAssertEqual(state.snapshot?.values.proxy["httpPort"], .number(8080))
        XCTAssertNotNil(state.failure)
        await server.setError("/settings/proxy", status: nil)
        await state.save(.proxy)
        XCTAssertEqual(state.proxy["httpPort"], .number(9090))
        XCTAssertEqual(state.auth["tokenValidityDays"], .text("45"))
        XCTAssertNil(state.failure)
    }

    func testAuthTestDoesNotSaveAndChannelChangesReconcileProviders() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await state.load()
        state.auth["tokenValidityDays"] = .number(60)
        await state.testAuth()
        XCTAssertEqual(state.snapshot?.values.auth["tokenValidityDays"], .number(30))
        state.channels["telegram"]?["enabled"] = .toggle(false)
        await state.save(.notification("telegram"))
        XCTAssertEqual(state.auth["authProviders"], .list([]))
        XCTAssertTrue(state.enabledChannels.isEmpty)
        let requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/settings/auth-tests") == true }.count, 1)
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/settings/auth") == true })
    }

    func testEnableOTPRequiresLoginAndLastProviderCannotBeDisabled() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await state.load()
        state.auth["otpRequired"] = .toggle(true)
        await state.save(.authentication)
        XCTAssertEqual(session.authentication, .required)
        XCTAssertFalse(session.canAccessDevices)
        await session.verifyOTP("123456")
        XCTAssertTrue(session.canAccessDevices)
        state.channels["telegram"]?["enabled"] = .toggle(false)
        XCTAssertNotNil(state.validation(for: .notification("telegram")))
        await state.save(.notification("telegram"))
        let requests = await server.allRequests()
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/settings/notifications/telegram") == true })
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/auth/otp") == true })
    }

    func testUnauthorizedSettingsOnlyRevokesOwningBackend() async throws {
        let firstServer = try SettingsTestServer(), secondServer = try SettingsTestServer()
        let first = try settingsSession(firstServer), second = try settingsSession(secondServer, host: "second.example")
        await first.refresh(force: true); await second.refresh(force: true)
        let state = SettingsState(session: first)
        await state.load()
        XCTAssertNotNil(state.snapshot)
        await firstServer.setError("/settings/proxy", status: 401)
        await state.save(.proxy)
        XCTAssertEqual(first.authentication, .required)
        XCTAssertEqual(first.accessRevision, 1)
        XCTAssertNil(state.snapshot)
        XCTAssertTrue(second.canAccessDevices)
        XCTAssertFalse(second.modems.isEmpty)
    }

    func testStoppedSessionDiscardsLateSettingsResponse() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await server.holdNext("/settings")
        let task = Task { await state.load() }
        await server.waitForHeldRequest()
        session.stop()
        await server.release()
        await task.value
        XCTAssertNil(state.snapshot)
        XCTAssertNil(state.failure)
        XCTAssertFalse(state.busy)
    }

    func testClearedSettingsCanReloadWithoutRestoringOlderValues() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = SettingsState(session: session)
        await server.holdNext("/settings")
        let previous = Task { await state.load() }
        await server.waitForHeldRequest()

        state.clear()
        XCTAssertFalse(state.busy)
        await server.setProxyPort(9090)
        await state.load()
        XCTAssertEqual(state.proxy["httpPort"], .number(9090))

        await server.release()
        await previous.value
        XCTAssertEqual(state.proxy["httpPort"], .number(9090))
        XCTAssertFalse(state.busy)
        XCTAssertNil(state.failure)
    }

    func testClearedMCPDiscardsLateKeyCreation() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = MCPState(session: session)
        await state.load()
        await server.holdNext("/settings/mcp/api-keys")
        let creation = Task { await state.create(.init(name: "Agent", permissions: ["modem.read"])) }
        await server.waitForHeldRequest()

        state.clear()
        XCTAssertFalse(state.busy)
        await server.release()
        await creation.value

        XCTAssertNil(state.secret)
        XCTAssertNil(state.settings)
        XCTAssertTrue(state.keys.isEmpty)
        XCTAssertNil(state.failure)
    }

    func testEndpointAndConfigPreserveProxyPrefix() throws {
        let address = try BackendAddress.normalize("https://example.com/sigmo")
        let url = try MCPState.endpoint(address: address, path: "/mcp")
        XCTAssertEqual(url.absoluteString, "https://example.com/sigmo/mcp")
        for path in ["//evil.example/mcp", "https://evil.example/mcp", "/../mcp", "/%2e%2e/mcp", "/mcp?secret=1"] {
            XCTAssertThrowsError(try MCPState.endpoint(address: address, path: path))
        }
        let config = try MCPState.clientConfig(endpoint: url.absoluteString, token: "quote\"slash\\")
        struct Config: Decodable { struct Server: Decodable { let url: String; let headers: [String: String] }; let mcpServers: [String: Server] }
        let decoded = try JSONDecoder().decode(Config.self, from: Data(config.utf8))
        XCTAssertEqual(decoded.mcpServers["sigmo"]?.url, url.absoluteString)
        XCTAssertEqual(decoded.mcpServers["sigmo"]?.headers["Authorization"], "Bearer quote\"slash\\")
    }

    func testMCPKeyLifecycleAndFailedToggle() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = MCPState(session: session)
        await state.load()
        XCTAssertEqual(state.settings?.permissions?.count, 3)
        await server.setError("/settings/mcp", status: 500)
        await state.setEnabled(true)
        XCTAssertEqual(state.settings?.enabled, false)
        await server.setError("/settings/mcp", status: nil)
        await state.setEnabled(true)
        XCTAssertEqual(state.settings?.enabled, true)
        var request = MCPKeyRequest(name: " Agent ", allModems: false, modemIds: ["device-1"], permissions: ["modem.read"])
        await state.create(request)
        XCTAssertEqual(state.secret?.apiKey.name, "Agent")
        XCTAssertEqual(state.secret?.apiKey.modemIds, ["device-1"])
        XCTAssertFalse(state.secret!.token.isEmpty)
        let id = try XCTUnwrap(state.secret?.apiKey.id)
        state.secret = nil
        await state.revoke(id)
        XCTAssertEqual(state.keys.first?.status, "revoked")
        await state.download()
        XCTAssertTrue(state.exporting)
        XCTAssertNotNil(state.skillData)
        state.finishExport(.failure(URLError(.cannotWriteToFile)))
        XCTAssertNotNil(state.failure)
        XCTAssertFalse(state.exporting)
        state.clear()
        XCTAssertNil(state.secret); XCTAssertNil(state.skillData); XCTAssertTrue(state.keys.isEmpty)
        request.permissions = []; XCTAssertFalse(request.isValid)
        request.permissions = ["modem.read"]; request.modemIds = []; XCTAssertFalse(request.isValid)
        request.allModems = true; XCTAssertTrue(request.isValid)
        request.validityDays = 181; XCTAssertFalse(request.isValid)
    }
    func testCancelledCallerDoesNotSendMutation() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let api = BackendAPI(configuration: session.configuration) { try await server.respond($0) }
        let baseline = await server.allRequests().count

        for useSession in [true, false] {
            let gate = SettingsRequestGate()
            let task = Task {
                await gate.wait()
                if useSession {
                    _ = try await session.protectedRequest { api, token in
                        try await api.saveMCP(true, token: token)
                    }
                } else {
                    _ = try await api.saveMCP(true, token: nil)
                }
            }
            await gate.waitForCaller()
            task.cancel()
            await gate.release()
            do {
                try await task.value
                XCTFail("A cancelled caller must not send a write request")
            } catch {
                XCTAssertTrue(error is CancellationError)
            }
        }
        let requests = await server.allRequests()
        XCTAssertEqual(requests.count, baseline)
    }

    func testSettingsRefreshDoesNotCancelUnrelatedOperation() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        await server.holdNext("/settings/mcp")
        let task = Task {
            try await session.protectedRequest { api, token in
                try await api.mcpSettings(token: token)
            }
        }
        await server.waitForHeldRequest()
        await session.refreshAfterSettingsChange()
        await server.release()
        let settings = try await task.value
        XCTAssertEqual(settings.endpointPath, "/mcp")
        XCTAssertTrue(session.canAccessDevices)
    }

    func testUnknownSchemaControlPreservesOtherSettings() throws {
        let response = SettingsFixtureData.settings.replacingOccurrences(of: #""control": "number""#, with: #""control": "futureWidget""#)
        let settings = try JSONDecoder().decode(SettingsResponse.self, from: Data(response.utf8))
        XCTAssertEqual(settings.schema.auth.first { $0.key == "tokenValidityDays" }?.control, .unsupported)
        XCTAssertEqual(settings.values.auth["tokenValidityDays"], .number(30))
        XCTAssertEqual(settings.schema.channels.count, 8)
    }

    func testInvalidMCPEndpointReportsLoadFailure() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        await server.setMCPEndpoint("//other.example/mcp")
        let state = MCPState(session: session)
        await state.load()
        XCTAssertNil(state.settings)
        XCTAssertNotNil(state.failure)
        XCTAssertTrue(state.endpoint.isEmpty)
    }

}

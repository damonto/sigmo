import XCTest
@testable import Sigmo

@MainActor
final class ModemSettingsTests: XCTestCase {
    private func make(_ server: DetailFixtureServer = DetailFixtureServer(), host: String = "settings.example",
                      authenticated: Bool = false) async throws -> ModemSettingsState {
        let configuration = BackendConfiguration(name: host, address: try BackendAddress.normalize("https://\(host)/prefix"))
        let credentials = MemoryCredentials()
        credentials.tokens[configuration.id] = "settings-token"
        let api = BackendAPI(configuration: configuration, transport: { request in
            if authenticated, request.url?.path.hasSuffix("/auth/otp/required") == true {
                return BackendHTTPResponse(data: Data(#"{"otpRequired":true}"#.utf8), status: 200)
            }
            return try await server.respond(request)
        })
        let session = BackendSession(configuration: configuration, credentials: credentials.store, api: api)
        await session.refresh(force: true)
        let state = ModemSettingsState(session: session, modemID: "device-1")
        await state.load()
        return state
    }

    private func save(_ change: ModemSettingsChange, _ state: ModemSettingsState, succeeds: Bool = true,
                      file: StaticString = #filePath, line: UInt = #line) async {
        let result = await state.save(change)
        XCTAssertEqual(result, succeeds, file: file, line: line)
    }

    private func writes(_ server: DetailFixtureServer) async -> [URLRequest] {
        await server.allRequests().filter { $0.httpMethod == "PUT" }
    }

    func testWireContractUsesScopedAuthenticatedRoutesAndEmptyWriteResponses() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server, authenticated: true)
        let initialWrites = await writes(server)
        XCTAssertTrue(initialWrites.isEmpty)

        await save(.mode(.init(allowed: 4, preferred: 0)), state)
        await save(.bands([.init(technology: 64, number: 1)]), state)
        await save(.device(.init(alias: "Office", mss: 128)), state)
        await save(.airplane(true), state)

        let requests = await writes(server)
        XCTAssertEqual(requests.map { $0.url?.path }, [
            "/prefix/api/v1/modems/device-1/networks/current-modes",
            "/prefix/api/v1/modems/device-1/networks/current-bands",
            "/prefix/api/v1/modems/device-1/settings",
            "/prefix/api/v1/modems/device-1/networks/airplane-mode",
        ])
        for request in requests {
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer settings-token")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "application/json")
            XCTAssertNotNil(request.value(forHTTPHeaderField: "X-Request-ID"))
        }
        XCTAssertEqual(try JSONDecoder().decode([String: UInt64].self, from: XCTUnwrap(requests[0].httpBody)), ["allowed": 4, "preferred": 0])
        struct Bands: Decodable { let bands: [ModemBandValue] }
        XCTAssertEqual(try JSONDecoder().decode(Bands.self, from: XCTUnwrap(requests[1].httpBody)).bands,
                       [.init(technology: 64, number: 1)])
        XCTAssertEqual(try JSONDecoder().decode(ModemDeviceSettings.self, from: XCTUnwrap(requests[2].httpBody)),
                       .init(alias: "Office", mss: 128))
        XCTAssertEqual(try JSONDecoder().decode([String: Bool].self, from: XCTUnwrap(requests[3].httpBody)), ["enabled": true])

        let configuration = state.session.configuration
        let api = BackendAPI(configuration: configuration, transport: { try await server.respond($0) })
        let escaped = try api.makeRequest(["modems", "one/two ?", "networks", "airplane-mode"], token: "private")
        XCTAssertTrue(try XCTUnwrap(escaped.url?.absoluteString).hasSuffix("/modems/one%2Ftwo%20%3F/networks/airplane-mode"))
    }

    func testUnsignedWireValuesAndCompositeIdentities() throws {
        let mode = try JSONDecoder().decode(ModemNetworkMode.self, from: Data(#"{"allowed":18446744073709551615,"preferred":9223372036854775808,"allowedLabel":"Future radio","preferredLabel":"Preferred","current":true}"#.utf8))
        XCTAssertEqual(mode.allowed, UInt64.max)
        XCTAssertEqual(mode.preferred, UInt64(1) << 63)
        XCTAssertEqual(mode.label, "Future radio / Preferred")
        let encoded = try JSONEncoder().encode(mode.id)
        XCTAssertEqual(try JSONDecoder().decode(ModemModeSelection.self, from: encoded), mode.id)
        let band = try JSONDecoder().decode(ModemBandValue.self, from: Data(#"{"technology":18446744073709551615,"number":65535}"#.utf8))
        XCTAssertEqual(band.technology, UInt64.max)
        XCTAssertEqual(band.number, UInt16.max)
        XCTAssertNotEqual(ModemBandValue(technology: 4, number: 1), ModemBandValue(technology: 64, number: 1))
        XCTAssertThrowsError(try JSONDecoder().decode(ModemBandValue.self, from: Data(#"{"technology":4,"number":65536}"#.utf8)))
    }

    func testAirplaneModeReconcilesDeviceAndDisablesOnlyRadioChanges() async throws {
        let state = try await make()
        await save(.airplane(true), state)
        XCTAssertEqual(state.airplane?.enabled, true)
        XCTAssertEqual(state.modem?.airplaneMode, true)
        XCTAssertEqual(state.modem?.internetConnected, false)
        XCTAssertNil(state.modem?.accessTechnology)
        XCTAssertFalse(state.canChangeMode)
        XCTAssertFalse(state.canChangeBands)
        XCTAssertTrue(state.canChangeDevice)
        XCTAssertTrue(state.canChangeAirplane)
        await save(.airplane(false), state)
        XCTAssertEqual(state.modem?.airplaneMode, false)
        XCTAssertTrue(state.canChangeMode)
        XCTAssertTrue(state.canChangeBands)
    }

    func testUnsupportedAirplaneAndSingleModeAreReadOnly() async throws {
        let server = DetailFixtureServer(scenario: .unsupportedSettings)
        let state = try await make(server)
        XCTAssertEqual(state.airplane?.supported, false)
        XCTAssertFalse(state.canChangeAirplane)
        XCTAssertFalse(state.canChangeMode)
        XCTAssertTrue(state.canChangeBands)
        await save(.airplane(true), state, succeeds: false)
        await save(.mode(.init(allowed: 4, preferred: 0)), state, succeeds: false)
        let requests = await writes(server)
        XCTAssertTrue(requests.isEmpty)
    }

    func testUnavailableModesAndBandsDoNotPreventLeavingAirplaneMode() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        var fixture = await server.currentModemSettings()
        fixture.statuses["GET /networks/modes"] = 503
        fixture.statuses["GET /networks/bands"] = 503
        await server.setModemSettings(fixture)
        await save(.airplane(true), state)
        XCTAssertTrue(state.canChangeAirplane)
        XCTAssertTrue(state.canChangeDevice)
        XCTAssertFalse(state.needsConfirmation)
        await save(.airplane(false), state)
        XCTAssertEqual(state.airplane?.enabled, false)
    }

    func testPartialReadFailurePreservesOtherModulesAndCanRetryOneSection() async throws {
        let server = DetailFixtureServer(scenario: .unavailableSettings)
        let state = try await make(server)
        XCTAssertNil(state.bands)
        XCTAssertEqual(state.failures[.bands]?.status, 503)
        XCTAssertTrue(state.canChangeMode)
        XCTAssertTrue(state.canChangeDevice)
        XCTAssertTrue(state.canChangeAirplane)
        let before = await server.allRequests()
        var fixture = await server.currentModemSettings()
        fixture.statuses = [:]
        await server.setModemSettings(fixture)
        await state.load([.bands])
        let after = await server.allRequests()
        XCTAssertNotNil(state.bands)
        XCTAssertNil(state.failures[.bands])
        XCTAssertEqual(before.filter { $0.url?.path.hasSuffix("/modes") == true }.count,
                       after.filter { $0.url?.path.hasSuffix("/modes") == true }.count)
    }

    func testFailedWritesRetainConfirmedValues() async throws {
        let changes: [ModemSettingsChange] = [.airplane(true), .mode(.init(allowed: 4, preferred: 0)),
                                             .bands([.init(technology: 64, number: 1)]), .device(.init(alias: "Office", mss: 128))]
        for change in changes {
            let server = DetailFixtureServer(scenario: .settingsWriteFailure)
            let state = try await make(server)
            let airplane = state.airplane, modes = state.modes, bands = state.bands, device = state.device
            await save(change, state, succeeds: false)
            XCTAssertEqual(state.failure?.status, 503, change.section.rawValue)
            XCTAssertEqual(state.airplane, airplane)
            XCTAssertEqual(state.modes, modes)
            XCTAssertEqual(state.bands, bands)
            XCTAssertEqual(state.device, device)
            XCTAssertFalse(state.needsConfirmation)
            let requests = await writes(server)
            XCTAssertEqual(requests.count, 1)
            await save(change, state)
        }
    }

    func testModeIncludesPreferredTechnologyAndIgnoresUnchangedOrUnsupportedChoices() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        let selection = ModemModeSelection(allowed: 68, preferred: 64)
        await save(.mode(selection), state)
        XCTAssertEqual(state.modes?.current.id, selection)
        XCTAssertEqual(state.modes?.current.label, "LTE + 5G SA / 5G SA")
        await save(.mode(selection), state, succeeds: false)
        await save(.mode(.init(allowed: 1, preferred: 0)), state, succeeds: false)
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testBandsRejectEmptyDuplicateUnsupportedAndUnchangedSelections() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        let lte = ModemBandValue(technology: 4, number: 1)
        for values in [[], [lte, lte], [.init(technology: 4, number: 99)], try XCTUnwrap(state.bands?.current)] {
            await save(.bands(values), state, succeeds: false)
        }
        let requests = await writes(server)
        XCTAssertTrue(requests.isEmpty)
        let nr = ModemBandValue(technology: 64, number: 1)
        await save(.bands([nr]), state)
        XCTAssertEqual(state.bands?.current, [nr])
        XCTAssertFalse(state.bands?.current.contains(lte) == true)
    }

    func testMSSValidationAndAliasNormalization() async throws {
        let values = [("64", true), ("254", true), ("240", true), (" 128 ", true), ("63", false), ("255", false),
                      ("64.5", false), ("64abc", false), ("1e2", false), ("", false), ("-64", false), ("+64", false),
                      ("999999999999999999999999", false)]
        for (text, valid) in values {
            var draft = ModemDeviceDraft(.init(alias: "  Office \n", mss: 240))
            draft.mss = text
            XCTAssertEqual(draft.submission != nil, valid, text)
            if valid { XCTAssertEqual(draft.submission?.alias, "Office") }
        }
        let state = try await make()
        await save(.device(.init(alias: "办公室", mss: 64)), state)
        XCTAssertEqual(state.modem?.displayName, "办公室")
        await save(.device(.init(alias: "", mss: 254)), state)
        XCTAssertEqual(state.modem?.displayName, "Home Modem")
        XCTAssertEqual(state.device?.mss, 254)
    }

    func testAcknowledgedWriteWithFailedReadbackRetriesOnlyReads() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        var fixture = await server.currentModemSettings()
        fixture.statuses["GET /settings"] = 503
        await server.setModemSettings(fixture)
        await save(.device(.init(alias: "Office", mss: 128)), state)
        XCTAssertNil(state.failure)
        XCTAssertTrue(state.writeAcknowledged)
        XCTAssertTrue(state.needsConfirmation)
        XCTAssertFalse(state.canChangeDevice)
        XCTAssertEqual(state.device?.alias, "")
        XCTAssertEqual(state.modem?.name, "Office")
        fixture = await server.currentModemSettings()
        fixture.statuses = [:]
        await server.setModemSettings(fixture)
        await state.load()
        XCTAssertFalse(state.needsConfirmation)
        XCTAssertEqual(state.device, .init(alias: "Office", mss: 128))
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testInvalidStoredMSSCanBeCorrectedWithoutInventingADefault() async throws {
        let server = DetailFixtureServer()
        var fixture = ModemSettingsFixture()
        fixture.device = .init(alias: "Legacy", mss: 0)
        await server.setModemSettings(fixture)
        let state = try await make(server)
        var draft = ModemDeviceDraft(try XCTUnwrap(state.device))
        XCTAssertEqual(draft.mss, "0")
        XCTAssertFalse(state.canSaveDevice(draft))
        draft.mss = "128"
        XCTAssertTrue(state.canSaveDevice(draft))
        await save(.device(try XCTUnwrap(draft.submission)), state)
        XCTAssertEqual(state.device?.mss, 128)
    }

    func testConnectionLossAfterApplyingWriteReconcilesWithoutRepeatingIt() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        var fixture = await server.currentModemSettings()
        fixture.disconnectAfterWrite = true
        await server.setModemSettings(fixture)
        await save(.airplane(true), state, succeeds: false)
        XCTAssertTrue(state.failure?.isNetwork == true)
        XCTAssertEqual(state.airplane?.enabled, true)
        XCTAssertEqual(state.modem?.airplaneMode, true)
        XCTAssertFalse(state.needsConfirmation)
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testWritesStayExclusiveThroughAuthoritativeRefresh() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.holdNext("/networks/modes")
        let operation = Task { await state.save(.airplane(true)) }
        await server.waitForHeldRequest()
        XCTAssertTrue(state.busy)
        await save(.airplane(false), state, succeeds: false)
        await save(.device(.init(alias: "Other", mss: 128)), state, succeeds: false)
        await server.release()
        let saved = await operation.value
        XCTAssertTrue(saved)
        XCTAssertFalse(state.busy)
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testNewReadWinsOverLateSnapshot() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.holdNext("/networks/modes")
        let oldRead = Task { await state.load([.mode]) }
        await server.waitForHeldRequest()
        var fixture = await server.currentModemSettings()
        fixture.mode = .init(allowed: 4, preferred: 0)
        await server.setModemSettings(fixture)
        await state.load([.mode])
        await server.release()
        await oldRead.value
        XCTAssertEqual(state.modes?.current.id, .init(allowed: 4, preferred: 0))
        XCTAssertTrue(state.loading.isEmpty)
    }

    func testLogoutRejectsLateProtectedResponse() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server, authenticated: true)
        await server.holdNext("/settings")
        let read = Task { await state.load([.device]) }
        await server.waitForHeldRequest()
        state.session.logout()
        state.clear()
        await server.release()
        await read.value
        XCTAssertNil(state.device)
        XCTAssertNil(state.airplane)
        XCTAssertFalse(state.hasAccess)
        XCTAssertTrue(state.loading.isEmpty)
    }

    func testUnauthorizedReadClearsOnlyOwningBackend() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server, authenticated: true)
        let other = try await make(host: "other.example")
        var fixture = await server.currentModemSettings()
        fixture.statuses["GET /networks/modes"] = 401
        await server.setModemSettings(fixture)
        await state.load([.mode])
        XCTAssertFalse(state.hasAccess)
        XCTAssertNil(state.modes)
        XCTAssertNil(state.device)
        XCTAssertTrue(other.hasAccess)
        XCTAssertNotNil(other.device)
    }

    func testRemovedDeviceClearsSettingsAndRejectsWrites() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.setStatus("/modems/device-1", 404)
        await state.load()
        XCTAssertNil(state.modem)
        XCTAssertNil(state.device)
        XCTAssertNil(state.bands)
        await save(.airplane(true), state, succeeds: false)
        let requests = await writes(server)
        XCTAssertTrue(requests.isEmpty)
    }

    func testCancelledWriteIsReconciledOnReturnWithoutReplay() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.holdNext("/networks/airplane-mode")
        let operation = Task { await state.save(.airplane(true)) }
        await server.waitForHeldRequest()
        operation.cancel()
        await server.release()
        let saved = await operation.value
        XCTAssertFalse(saved)
        XCTAssertFalse(state.busy)
        XCTAssertTrue(state.needsConfirmation)
        XCTAssertNil(state.failure)
        await state.load()
        XCTAssertEqual(state.airplane?.enabled, true)
        XCTAssertFalse(state.needsConfirmation)
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testClearedStateRejectsLateWriteCompletion() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.holdNext("/settings")
        let operation = Task { await state.save(.device(.init(alias: "First", mss: 128))) }
        await server.waitForHeldRequest()
        state.clear()
        await state.load()
        await save(.device(.init(alias: "Second", mss: 240)), state)
        await server.release()
        let oldSaved = await operation.value
        XCTAssertFalse(oldSaved)
        XCTAssertEqual(state.device?.alias, "Second")
        XCTAssertFalse(state.needsConfirmation)
        XCTAssertFalse(state.busy)
    }

    func testSessionStopLeavesUncertainWriteSilentUntilReload() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        await server.holdNext("/networks/airplane-mode")
        let operation = Task { await state.save(.airplane(true)) }
        await server.waitForHeldRequest()
        state.session.stop()
        await server.release()
        let saved = await operation.value
        XCTAssertFalse(saved)
        XCTAssertNil(state.failure)
        XCTAssertTrue(state.needsConfirmation)
        XCTAssertEqual(state.airplane?.enabled, false)
        await state.load()
        XCTAssertEqual(state.airplane?.enabled, true)
        XCTAssertFalse(state.needsConfirmation)
        let requests = await writes(server)
        XCTAssertEqual(requests.count, 1)
    }

    func testBackendAndModemSettingsAreIsolated() async throws {
        let server = DetailFixtureServer()
        let first = try await make(server)
        let second = ModemSettingsState(session: first.session, modemID: "device-2")
        await second.load()
        let otherBackend = try await make(host: "other.example")
        await save(.device(.init(alias: "Office", mss: 128)), first)
        await second.load()
        XCTAssertEqual(second.device, .init(alias: "", mss: 240))
        XCTAssertEqual(second.modem?.name, "Source Modem")
        XCTAssertEqual(otherBackend.device, .init(alias: "", mss: 240))
        XCTAssertEqual(otherBackend.modem?.name, "Home Modem")
    }

    func testEmptyBandsAndUnavailableModesDoNotInventChoices() async throws {
        let server = DetailFixtureServer()
        var fixture = ModemSettingsFixture()
        fixture.supportedBands = []
        fixture.selectedBands = []
        fixture.mode = .init(allowed: 999, preferred: 0)
        await server.setModemSettings(fixture)
        let state = try await make(server)
        XCTAssertEqual(state.bands?.supported, [])
        XCTAssertFalse(state.canChangeBands)
        XCTAssertNil(state.modes)
        XCTAssertFalse(state.canChangeMode)
        XCTAssertTrue(state.canChangeDevice)
    }

    func testDeviceSavePreservesOutstandingRadioConfirmation() async throws {
        let server = DetailFixtureServer()
        let state = try await make(server)
        var fixture = await server.currentModemSettings()
        fixture.statuses["GET /networks/airplane-mode"] = 503
        await server.setModemSettings(fixture)
        await save(.airplane(true), state)
        await save(.device(.init(alias: "Office", mss: 128)), state)
        XCTAssertTrue(state.needsConfirmation)
        XCTAssertEqual(state.confirmationsPending[.airplane], true)
        XCTAssertNil(state.confirmationsPending[.device])
        XCTAssertFalse(state.canChangeMode)
        fixture = await server.currentModemSettings()
        fixture.statuses = [:]
        await server.setModemSettings(fixture)
        await state.load()
        XCTAssertFalse(state.needsConfirmation)
        XCTAssertEqual(state.airplane?.enabled, true)
    }
}

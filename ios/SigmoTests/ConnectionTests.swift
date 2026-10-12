import XCTest
@testable import Sigmo

@MainActor
final class ConnectionTests: XCTestCase {
    private let profileID = "se-a:8986000000000000001"

    private func make(_ server: DetailFixtureServer, host: String = "connections.example") async throws -> ModemDetailState {
        let configuration = BackendConfiguration(name: host, address: try BackendAddress.normalize("https://\(host)/prefix"))
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        XCTAssertTrue(detail.isActiveProfile(profileID))
        return detail
    }

    private func writes(_ server: DetailFixtureServer, suffix: String) async -> [URLRequest] {
        await server.allRequests().filter { $0.httpMethod != "GET" && $0.url?.path.hasSuffix(suffix) == true }
    }

    func testReadFailureAlertsPreserveUnavailableControlsAfterDismissal() async throws {
        let detail = try await make(DetailFixtureServer(scenario: .connectionReadFailure))
        let internet = InternetState(detail: detail, profileID: profileID)
        await internet.reload()
        XCTAssertEqual(internet.failure?.status, 503)
        internet.failure = nil
        XCTAssertNotNil(internet.loadFailure)
        XCTAssertFalse(internet.canChange)

        let network = NetworkSelectionState(detail: detail, profileID: profileID)
        await network.reload()
        XCTAssertEqual(network.failure?.status, 503)
        network.failure = nil
        XCTAssertNotNil(network.loadFailure)
        XCTAssertFalse(network.canChange)

        let wifi = WiFiCallingState(detail: detail, profileID: profileID)
        await wifi.reload()
        XCTAssertEqual(wifi.failure?.status, 503)
        wifi.failure = nil
        XCTAssertNotNil(wifi.loadFailure)
        XCTAssertFalse(wifi.canChange)

        let volte = VoLTEState(detail: detail, profileID: profileID)
        await volte.reload()
        XCTAssertEqual(volte.failure?.status, 503)
        volte.failure = nil
        XCTAssertNotNil(volte.loadFailure)
        XCTAssertFalse(volte.canChange)
    }

    func testManualSelectionCommitsOnlyAfterChoosingAnOperator() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = NetworkSelectionState(detail: detail, profileID: profileID)
        await state.load()
        XCTAssertFalse(state.manual)
        await state.setAutomatic(false)
        XCTAssertTrue(state.manual)
        XCTAssertEqual(state.registration?.mode, "automatic")
        XCTAssertEqual(state.networks.count, 2)
        XCTAssertEqual(state.networks.last?.accessTechnologies, ["LTE", "5G SA"])
        let pendingWrites = await writes(server, suffix: "/networks/registration")
        XCTAssertTrue(pendingWrites.isEmpty)
        state.leave()
        await state.load()
        XCTAssertFalse(state.manual)

        await state.setAutomatic(false)
        await state.select(try XCTUnwrap(state.networks.last))
        XCTAssertEqual(state.registration?.operatorCode, "23410")
        XCTAssertEqual(detail.modem?.networkSelectionName, "Travel Mobile")
        await state.setAutomatic(true)
        XCTAssertFalse(state.manual)
        let requests = await writes(server, suffix: "/networks/registration")
        XCTAssertEqual(requests.count, 2)
        XCTAssertTrue(requests.allSatisfy { $0.timeoutInterval == 120 })
        let payload = try JSONDecoder().decode(NetworkRegistration.self, from: XCTUnwrap(requests.last?.httpBody))
        XCTAssertEqual(payload.mode, "automatic")
        XCTAssertNil(payload.operatorCode)
    }

    func testSavedManualModeAndAsynchronousScan() async throws {
        let server = DetailFixtureServer()
        var fixture = ConnectionFixture()
        fixture.registration = .init(mode: "manual", operatorCode: "23410")
        fixture.scanPolls = 1
        await server.setConnectionFixture(fixture)
        let detail = try await make(server)
        let state = NetworkSelectionState(detail: detail, profileID: profileID)
        await state.load()
        XCTAssertTrue(state.manual)
        XCTAssertEqual(state.selectedOperator, "23410")
        await state.scan()
        XCTAssertEqual(state.networks.count, 2)
        let requests = await server.allRequests()
        XCTAssertTrue(requests.contains { $0.httpMethod == "GET" && $0.url?.path.hasSuffix("/network-scans/scan-1") == true })
        XCTAssertTrue(requests.filter { $0.httpMethod == "GET" }.allSatisfy { $0.timeoutInterval == 15 })
    }

    func testScanFailuresAndEmptyResultsPreserveRegistration() async throws {
        for outcome in ["failed", "canceled", "unexpected", "completed"] {
            let server = DetailFixtureServer()
            var fixture = ConnectionFixture()
            fixture.scanOutcome = outcome
            fixture.networks = []
            await server.setConnectionFixture(fixture)
            let detail = try await make(server)
            let state = NetworkSelectionState(detail: detail, profileID: profileID)
            await state.load()
            await state.setAutomatic(false)
            XCTAssertTrue(state.networks.isEmpty, outcome)
            XCTAssertEqual(state.scanFailure == nil, outcome == "completed", outcome)
            XCTAssertEqual(state.failure, state.scanFailure, outcome)
            XCTAssertEqual(state.registration?.mode, "automatic", outcome)
        }
    }

    func testLeavingManualSelectionDiscardsLateScanResults() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = NetworkSelectionState(detail: detail, profileID: profileID)
        await state.load()
        await server.holdNext("/network-scans")
        let scan = Task { await state.setAutomatic(false) }
        await server.waitForHeldRequest()
        state.leave()
        scan.cancel()
        await server.release()
        await scan.value
        XCTAssertFalse(state.scanning)
        XCTAssertFalse(state.manual)
        XCTAssertTrue(state.networks.isEmpty)
        let requests = await writes(server, suffix: "/networks/registration")
        XCTAssertTrue(requests.isEmpty)
        let fixture = await server.currentConnections()
        XCTAssertTrue(fixture.scanStarted)
    }

    func testRegistrationTimeoutReadsAppliedStateWithoutRepeatingWrite() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = NetworkSelectionState(detail: detail, profileID: profileID)
        await state.load()
        await state.setAutomatic(false)
        var fixture = await server.currentConnections()
        fixture.disconnectAfterWrite = true
        await server.setConnectionFixture(fixture)
        await state.select(try XCTUnwrap(state.networks.last))
        XCTAssertEqual(state.registration?.mode, "manual")
        XCTAssertEqual(state.registration?.operatorCode, "23410")
        XCTAssertNotNil(state.failure)
        let requests = await writes(server, suffix: "/networks/registration")
        XCTAssertEqual(requests.count, 1)
    }

    func testUnknownModeAndRegistrationFailureDoNotInventASelection() async throws {
        let server = DetailFixtureServer()
        var fixture = ConnectionFixture()
        fixture.registration = .init(mode: "new_mode", operatorCode: nil)
        await server.setConnectionFixture(fixture)
        let detail = try await make(server)
        let state = NetworkSelectionState(detail: detail, profileID: profileID)
        await state.load()
        XCTAssertNil(state.registration?.automatic)
        XCTAssertFalse(state.canChange)
        XCTAssertNotNil(state.loadFailure)
        fixture.registration = .init(mode: "automatic", operatorCode: nil)
        fixture.statuses["PUT /networks/registration"] = 503
        await server.setConnectionFixture(fixture)
        await state.load()
        await state.setAutomatic(false)
        await state.select(try XCTUnwrap(state.networks.last))
        XCTAssertEqual(state.registration?.mode, "automatic")
        XCTAssertEqual(state.failure?.status, 503)
    }

    func testNetworkSummaryDoesNotUseSIMBrandWhenUnregistered() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        var fixture = ConnectionFixture()
        fixture.registeredOperator = .init(name: "", code: "")
        for (state, key) in [("Searching", "modem.value.searching"), ("searching", "modem.value.searching"),
                             ("Idle", "connection.noService"), ("Denied", "connection.noService"),
                             ("Unknown", "common.unknown"), ("", "common.unknown"), ("future", "common.unknown")] {
            fixture.registrationState = state
            await server.setConnectionFixture(fixture)
            await detail.session.refreshModem(detail.modemID)
            XCTAssertNotEqual(detail.modem?.networkSelectionName, "Example Mobile", state)
            XCTAssertEqual(detail.modem?.networkSelectionName, detailText(key), state)
        }
    }

    func testNetworkSummaryUsesBackendRegistrationStatesAndOperator() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        var fixture = ConnectionFixture()
        for state in ["Home", "Roaming", "home", "roaming", "registered", "Unknown", ""] {
            fixture.registrationState = state
            fixture.registeredOperator = .init(name: "Travel Mobile", code: "23410")
            await server.setConnectionFixture(fixture)
            await detail.session.refreshModem(detail.modemID)
            XCTAssertEqual(detail.modem?.networkSelectionName, "Travel Mobile", state)
        }
        fixture.registeredOperator = .init(name: "  ", code: "23410")
        await server.setConnectionFixture(fixture)
        await detail.session.refreshModem(detail.modemID)
        XCTAssertEqual(detail.modem?.networkSelectionName, "23410")
    }

    func testInternetDraftSurvivesReadsAndSubmitsCompletePreferences() async throws {
        let server = DetailFixtureServer()
        var fixture = ConnectionFixture()
        fixture.internet.connected = false
        await server.setConnectionFixture(fixture)
        let detail = try await make(server)
        let state = InternetState(detail: detail, profileID: profileID)
        await state.load()
        state.draft.apn = ""
        state.draft.apnUsername = " alice "
        state.draft.apnPassword = " secret with spaces "
        state.draft.apnAuth = "default"
        state.draft.ipType = "ipv6"
        await state.updateOptions(.init(defaultRoute: true, proxyEnabled: false, alwaysOn: true))
        let draft = state.draft
        await state.load()
        XCTAssertEqual(state.draft, draft)
        let preferenceWrites = await writes(server, suffix: "/current/preferences")
        XCTAssertTrue(preferenceWrites.isEmpty)
        await state.setConnected(true)
        XCTAssertTrue(state.connected)
        XCTAssertFalse(state.canEditAPN)
        let requests = await writes(server, suffix: "/internet-connections")
        let request = try XCTUnwrap(requests.last)
        let sent = try JSONDecoder().decode(InternetPreferences.self, from: XCTUnwrap(request.httpBody))
        XCTAssertEqual(sent, draft.submission)
        XCTAssertEqual(sent.apnPassword, " secret with spaces ")
        XCTAssertEqual(sent.apnAuth, "")
        XCTAssertEqual(request.timeoutInterval, 120)
        await state.setConnected(false)
        XCTAssertFalse(state.connected)
        XCTAssertEqual(state.draft.apnPassword, sent.apnPassword)
        XCTAssertTrue(state.draft.alwaysOn)
    }

    func testInternetOptionFailurePreservesConfirmedValuesAndAPN() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = InternetState(detail: detail, profileID: profileID)
        await state.load()
        let original = state.options
        var fixture = await server.currentConnections()
        fixture.statuses["PUT /internet-connections/current/preferences"] = 503
        await server.setConnectionFixture(fixture)
        await state.updateOptions(.init(defaultRoute: true, proxyEnabled: false, alwaysOn: false))
        XCTAssertEqual(state.options, original)
        XCTAssertEqual(state.failure?.status, 503)
        fixture.statuses = [:]
        await server.setConnectionFixture(fixture)
        let next = InternetConnectionOptions(defaultRoute: true, proxyEnabled: false, alwaysOn: true)
        await state.updateOptions(next)
        XCTAssertEqual(state.options, next)
        XCTAssertEqual(state.connection?.preferences.apn, "example.apn")
        XCTAssertEqual(state.connection?.preferences.apnPassword, "secret")
        let requests = await writes(server, suffix: "/current/preferences")
        let body = try JSONDecoder().decode([String: Bool].self, from: XCTUnwrap(requests.last?.httpBody))
        XCTAssertEqual(Set(body.keys), ["defaultRoute", "proxyEnabled", "alwaysOn"])
    }

    func testInternetTimeoutReconcilesWithoutConnectingAgain() async throws {
        let server = DetailFixtureServer()
        var fixture = ConnectionFixture()
        fixture.internet.connected = false
        fixture.disconnectAfterWrite = true
        await server.setConnectionFixture(fixture)
        let detail = try await make(server)
        let state = InternetState(detail: detail, profileID: profileID)
        await state.load()
        await state.setConnected(true)
        XCTAssertTrue(state.connected)
        XCTAssertNotNil(state.failure)
        let requests = await writes(server, suffix: "/internet-connections")
        XCTAssertEqual(requests.count, 1)
    }

    func testPublicInformationRejectsOldBearerAndRecoversOnRefresh() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = InternetState(detail: detail, profileID: profileID)
        await state.load()
        await server.holdNext("/internet-connections/public")
        let oldRead = Task { await state.loadPublic() }
        await server.waitForHeldRequest()
        var fixture = await server.currentConnections()
        fixture.internet.bearer = 9
        await server.setConnectionFixture(fixture)
        await state.load()
        await server.release()
        await oldRead.value
        XCTAssertNil(state.publicInfo)
        fixture.statuses["GET /internet-connections/public"] = 503
        await server.setConnectionFixture(fixture)
        await state.loadPublic()
        XCTAssertNotNil(state.publicFailure)
        XCTAssertEqual(state.failure, state.publicFailure)
        state.failure = nil
        XCTAssertNotNil(state.publicFailure)
        XCTAssertTrue(state.connected)
        XCTAssertTrue(state.canChange)
        fixture.statuses = [:]
        await server.setConnectionFixture(fixture)
        await state.reload()
        XCTAssertEqual(state.publicInfo?.ip, "203.0.113.9")
        XCTAssertNil(state.publicFailure)
        await state.setConnected(false)
        XCTAssertNil(state.publicInfo)
    }

    func testProxyURLPreservesIPv6AndEscapesCredentials() throws {
        let proxy = InternetProxy(enabled: true, username: "user:name@domain", password: "a:/?#%测试", httpAddress: "192.0.2.1:8080", socks5Address: "[2001:db8::1]:1080")
        for socks in [false, true] {
            let text = try XCTUnwrap(proxy.url(socks: socks))
            let url = try XCTUnwrap(URLComponents(string: text))
            XCTAssertEqual(url.user, "user:name@domain")
            XCTAssertEqual(url.password, "a:/?#%测试")
            XCTAssertEqual(url.scheme, socks ? "socks5h" : "http")
            XCTAssertEqual(url.port, socks ? 1080 : 8080)
        }
        let unavailable = InternetProxy(enabled: false, username: "user", password: "secret", httpAddress: "host:80", socks5Address: nil)
        XCTAssertNil(unavailable.url(socks: false))
    }

    func testAPNValidationAcceptsAutomaticAndPreservesPasswordWhitespace() {
        var preferences = InternetPreferences.automatic
        XCTAssertTrue(preferences.valid)
        preferences.apn = String(repeating: "a", count: 101)
        XCTAssertFalse(preferences.valid)
        preferences.apn = " carrier.apn "
        preferences.apnPassword = " password "
        XCTAssertEqual(preferences.submission.apn, "carrier.apn")
        XCTAssertEqual(preferences.submission.apnPassword, " password ")
        preferences.ipType = "future-ip"
        XCTAssertFalse(preferences.valid)
    }

    func testWiFiEnableDisconnectAndReconnectHaveDistinctRequests() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = WiFiCallingState(detail: detail, profileID: profileID)
        await state.load()
        await state.setEnabled(true)
        XCTAssertTrue(state.settings?.connected == true)
        let sessionsBeforeReconnect = await writes(server, suffix: "/wifi-calling/sessions")
        XCTAssertTrue(sessionsBeforeReconnect.isEmpty)
        await state.disconnect()
        XCTAssertTrue(state.settings?.enabled == true)
        XCTAssertFalse(state.settings?.connected ?? true)
        await state.reconnect()
        XCTAssertTrue(state.settings?.connected == true)
        await state.setEnabled(false)
        XCTAssertFalse(state.settings?.enabled ?? true)
        XCTAssertEqual(state.settings?.underlay, WiFiUnderlay(mode: "modem", modemId: "device-2"))
        let settings = await writes(server, suffix: "/wifi-calling/settings")
        let sessions = await writes(server, suffix: "/wifi-calling/sessions")
        XCTAssertEqual(settings.count, 2)
        XCTAssertEqual(sessions.count, 1)
    }

    func testUnavailableUplinkRemainsSelectedWithoutImplicitFallback() async throws {
        let server = DetailFixtureServer()
        var other = ConnectionFixture()
        other.internet.connected = false
        await server.setConnectionFixture(other, modemID: "device-2")
        let detail = try await make(server)
        let state = WiFiCallingState(detail: detail, profileID: profileID)
        await state.load()
        let selected = try XCTUnwrap(state.settings?.underlay)
        XCTAssertEqual(state.uplinks.first { $0.underlay == selected }?.available, false)
        await state.selectUplink(selected)
        let before = await writes(server, suffix: "/wifi-calling/settings")
        XCTAssertTrue(before.isEmpty)
        await state.selectUplink(.system)
        XCTAssertEqual(state.settings?.underlay, .system)
        XCTAssertFalse(state.settings?.enabled ?? true)
        let unchanged = await server.currentConnections("device-2")
        XCTAssertEqual(unchanged.wifi.underlay.mode, "modem")
    }

    func testCarrierSetupIsOfferedOnceAndWaitsForTerminalCallback() async throws {
        let server = DetailFixtureServer(scenario: .connectionSetup)
        let detail = try await make(server)
        let state = WiFiCallingState(detail: detail, profileID: profileID)
        await state.load()
        await state.setEnabled(true)
        XCTAssertTrue(state.setupRequired)
        XCTAssertNotNil(state.websheet)
        state.closeWebsheet()
        await state.load()
        XCTAssertNil(state.websheet)
        XCTAssertTrue(state.settings?.enabled == true)
        await state.openWebsheet()
        let progress = CarrierCallback(event: "progress")
        await state.completeWebsheet(progress)
        XCTAssertNotNil(state.websheet)
        await state.completeWebsheet(progress)
        let callbacks = await writes(server, suffix: "/callback")
        XCTAssertEqual(callbacks.count, 1)
        await state.completeWebsheet(.init(event: "entitlementChanged"))
        XCTAssertNil(state.websheet)
        XCTAssertTrue(state.settings?.connected == true)
        XCTAssertFalse(state.setupRequired)
    }

    func testCarrierCallbackFailureAllowsLaterCarrierEvent() async throws {
        let server = DetailFixtureServer(scenario: .connectionSetup)
        let detail = try await make(server)
        let state = WiFiCallingState(detail: detail, profileID: profileID)
        await state.load()
        await state.setEnabled(true)
        await server.setStatus("/callback", 503)
        let callback = CarrierCallback(event: "entitlementChanged")
        await state.completeWebsheet(callback)
        XCTAssertNotNil(state.websheet)
        XCTAssertEqual(state.failure?.status, 503)
        await server.setStatus("/callback", 200)
        await state.completeWebsheet(callback)
        XCTAssertNil(state.websheet)
        XCTAssertNil(state.failure)
    }

    func testEmergencyAddressAvailabilityAndTerminalEventClassification() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = WiFiCallingState(detail: detail, profileID: profileID)
        await state.load()
        await state.openWebsheet(emergencyAddress: true)
        XCTAssertNil(state.websheet)
        var fixture = await server.currentConnections()
        fixture.wifi.emergencyAddressUpdateAvailable = true
        await server.setConnectionFixture(fixture)
        await state.load()
        await state.openWebsheet(emergencyAddress: true)
        XCTAssertEqual(state.websheet?.id, "e911-device-1")
        let requests = await writes(server, suffix: "/emergency-address-websheets")
        XCTAssertEqual(requests.count, 1)
        XCTAssertEqual(requests.first?.timeoutInterval, 120)
        for event in ["phoneServicesAccountStatusChanged", "FinishFlow", "cancel-button-clicked", "workflowCompleted"] {
            XCTAssertTrue(WiFiCallingState.terminalCallback(.init(event: event)), event)
        }
        XCTAssertFalse(WiFiCallingState.terminalCallback(.init(event: "progress")))
        XCTAssertFalse(WiFiCallingState.terminalCallback(.init()))
    }

    func testVoLTEDataPathChangesOnlyWhileDisabled() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = VoLTEState(detail: detail, profileID: profileID)
        await state.load()
        await state.selectDataPath(.legacy)
        XCTAssertEqual(state.settings?.dataPath, "qmap")
        await state.setEnabled(false)
        await state.selectDataPath(.legacy)
        XCTAssertEqual(state.settings?.dataPath, "legacy_bam_dmux")
        await state.setEnabled(true)
        XCTAssertTrue(state.settings?.enabled == true)
        XCTAssertEqual(state.settings?.dataPath, "legacy_bam_dmux")
        let requests = await writes(server, suffix: "/volte/settings")
        XCTAssertEqual(requests.count, 3)
        XCTAssertTrue(requests.allSatisfy { $0.timeoutInterval == 120 })
    }

    func testMBIMOmitsQMIDataPathAndModemManagedStatusIsDistinct() async throws {
        let server = DetailFixtureServer(scenario: .connectionMBIM)
        let detail = try await make(server)
        let state = VoLTEState(detail: detail, profileID: profileID)
        await state.load()
        XCTAssertFalse(state.canChangeDataPath)
        await state.setEnabled(false)
        let requests = await writes(server, suffix: "/volte/settings")
        let payload = try JSONDecoder().decode([String: Bool].self, from: XCTUnwrap(requests.first?.httpBody))
        XCTAssertEqual(payload, ["enabled": false])
        var fixture = await server.currentConnections()
        fixture.volte.modemRegistered = true
        await server.setConnectionFixture(fixture)
        await state.load()
        XCTAssertTrue(state.settings?.modemManaged == true)
        XCTAssertEqual(state.settings?.statusKey, "connection.modemManaged")
    }

    func testVoLTEWriteRejectsOlderPollAndDuplicateSubmission() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = VoLTEState(detail: detail, profileID: profileID)
        await state.load()
        await server.holdNext("/volte/settings")
        let oldRead = Task { await state.load() }
        await server.waitForHeldRequest()
        await state.setEnabled(false)
        await server.release()
        await oldRead.value
        XCTAssertFalse(state.settings?.enabled ?? true)
        await server.holdNext("/volte/settings")
        let write = Task { await state.setEnabled(true) }
        await server.waitForHeldRequest()
        await state.setEnabled(true)
        await server.release()
        await write.value
        let requests = await writes(server, suffix: "/volte/settings")
        XCTAssertEqual(requests.count, 2)
    }

    func testAirplaneModeBlocksCellularActionsButKeepsWiFiCalling() async throws {
        let server = DetailFixtureServer()
        var radio = ModemSettingsFixture()
        radio.airplaneEnabled = true
        await server.setModemSettings(radio)
        let detail = try await make(server)
        let network = NetworkSelectionState(detail: detail, profileID: profileID)
        let internet = InternetState(detail: detail, profileID: profileID)
        let wifi = WiFiCallingState(detail: detail, profileID: profileID)
        let volte = VoLTEState(detail: detail, profileID: profileID)
        await network.load(); await internet.load(); await wifi.load(); await volte.load()
        XCTAssertFalse(network.canChange)
        XCTAssertFalse(internet.canChange)
        XCTAssertFalse(volte.canChange)
        XCTAssertTrue(internet.canEditAPN)
        XCTAssertTrue(wifi.canChange)
        await network.setAutomatic(false)
        await internet.setConnected(true)
        await volte.setEnabled(false)
        await wifi.setEnabled(true)
        let writes = await server.allRequests().filter { $0.httpMethod != "GET" }
        XCTAssertEqual(writes.count, 1)
        XCTAssertTrue(writes.first?.url?.path.hasSuffix("/wifi-calling/settings") == true)
    }

    func testProfileSwitchInvalidatesEveryConnectionPage() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let network = NetworkSelectionState(detail: detail, profileID: profileID)
        let internet = InternetState(detail: detail, profileID: profileID)
        let wifi = WiFiCallingState(detail: detail, profileID: profileID)
        let volte = VoLTEState(detail: detail, profileID: profileID)
        await network.load(); await internet.load(); await wifi.load(); await volte.load()
        let profile = try XCTUnwrap(detail.currentProfile("se-a:8986000000000000002"))
        let enabled = await detail.enable(profile)
        XCTAssertTrue(enabled)
        await network.load(); await internet.load(); await wifi.load(); await volte.load()
        XCTAssertFalse(internet.scope.available)
        XCTAssertNil(network.registration)
        XCTAssertNil(internet.connection)
        XCTAssertEqual(internet.draft.apnPassword, "")
        XCTAssertNil(wifi.settings)
        XCTAssertNil(volte.settings)
    }

    func testConnection401RevokesOnlyOwningBackend() async throws {
        let firstServer = DetailFixtureServer(scenario: .connectionSetup)
        let first = try await make(firstServer)
        let other = try await make(DetailFixtureServer(), host: "other.example")
        let state = WiFiCallingState(detail: first, profileID: profileID)
        await state.load()
        await state.setEnabled(true)
        await firstServer.setStatus("/callback", 401)
        await state.completeWebsheet(.init(event: "entitlementChanged"))
        XCTAssertFalse(first.session.canAccessDevices)
        XCTAssertTrue(other.session.canAccessDevices)
        XCTAssertNil(state.websheet)
        XCTAssertNil(state.settings)
    }

    func testCanceledReadDoesNotReplaceSnapshotAndForegroundReloads() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let state = InternetState(detail: detail, profileID: profileID)
        await state.load()
        var fixture = await server.currentConnections()
        fixture.internet.connected = false
        await server.setConnectionFixture(fixture)
        await server.holdNext("/internet-connections/current")
        let read = Task { await state.load() }
        await server.waitForHeldRequest()
        read.cancel()
        await server.release()
        await read.value
        XCTAssertTrue(state.connected)
        await state.load()
        XCTAssertFalse(state.connected)
        XCTAssertNil(state.loadFailure)
    }
}

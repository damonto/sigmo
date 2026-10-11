import XCTest
@testable import Sigmo

@MainActor
final class BackendSessionTests: XCTestCase {
    func testInitialLoadConnectsButRefreshKeepsOnlineStatus() async throws {
        let stub = BackendStub()
        let session = try testSession(stub)
        XCTAssertFalse(session.isOnline)
        XCTAssertEqual(session.statusKey, "status.notConnected")

        for wasOnline in [false, true] {
            await stub.holdNextModemRequest()
            let refresh = Task { await session.refresh(force: true) }
            await stub.waitForHeldRequest()
            XCTAssertTrue(session.isRefreshing)
            XCTAssertEqual(session.isOnline, wasOnline)
            XCTAssertEqual(session.statusKey, wasOnline ? "status.online" : "status.connecting")
            await stub.release()
            await refresh.value
            XCTAssertTrue(session.isOnline)
            XCTAssertEqual(session.statusKey, "status.online")
        }
    }

    func testBackendWithoutDevicesIsOnline() async throws {
        let stub = BackendStub()
        let configuration = try testConfiguration()
        let api = BackendAPI(configuration: configuration) { request in
            if request.url?.path.hasSuffix("/modems") == true {
                return BackendHTTPResponse(data: Data("[]".utf8), status: 200)
            }
            return try await stub.respond(request)
        }
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store, api: api)
        await session.refresh(force: true)
        XCTAssertTrue(session.modems.isEmpty)
        XCTAssertTrue(session.isOnline)
        XCTAssertEqual(session.statusKey, "status.online")
    }

    func testCancelledAuthenticationActionsReleaseBusyStateAndAllowRetry() async throws {
        for path in ["/auth/otp", "/auth/otp/verify", "/license/pairings"] {
            let stub = BackendStub()
            await stub.setRequired(true)
            if path == "/license/pairings" { await stub.setLicense(authorized: false) }
            let session = try testSession(stub)
            await session.refresh(force: true)
            await stub.holdNextRequest(path)
            let perform: @MainActor () async -> Void = {
                switch path {
                case "/auth/otp": await session.sendOTP()
                case "/auth/otp/verify": await session.verifyOTP("123456")
                default: await session.createPairing()
                }
            }
            let task = Task { await perform() }
            await stub.waitForHeldRequest()
            task.cancel()
            await stub.release()
            await task.value
            XCTAssertFalse(session.isSendingOTP, path)
            XCTAssertFalse(session.isVerifyingOTP, path)
            XCTAssertFalse(session.isCreatingPairing, path)
            XCTAssertNil(session.authFailure, path)
            XCTAssertNil(session.pairingFailure, path)
            await perform()
            let count = await stub.count(path)
            XCTAssertEqual(count, 2, path)
        }
    }

    func testAlreadyCancelledLoginDoesNotStopAnUnrelatedRefresh() async throws {
        let stub = BackendStub()
        let session = try testSession(stub)
        await stub.holdNextModemRequest()
        let refresh = Task { await session.refresh(force: true) }
        await stub.waitForHeldRequest()
        let login = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            await session.verifyOTP("123456")
        }
        await login.value
        XCTAssertTrue(session.isRefreshing)
        let count = await stub.count("/auth/otp/verify")
        XCTAssertEqual(count, 0)
        await stub.release()
        await refresh.value
        XCTAssertEqual(session.modems.count, 1)
    }

    func testIMSStatesComeFromDeviceSnapshotsWithoutExtraQueries() async throws {
        let stub = BackendStub()
        await stub.setCapabilities(status: 200, json: #"["volte","wifiCalling"]"#)
        await stub.setModemJSON(PreviewBackendServer.modemJSON(wifiCalling: true, volte: true))
        let session = try testSession(stub)
        await session.refresh(force: true)
        XCTAssertEqual(session.modems.count, 1)
        XCTAssertEqual(session.modems.first?.volteStatus, .connected)
        XCTAssertEqual(session.modems.first?.wifiCallingStatus, .connected)
        let count = await stub.count("/volte/settings")
        XCTAssertEqual(count, 0)
        await stub.setModemJSON(PreviewBackendServer.modemJSON(imsEnabled: false))
        await session.refresh(force: true)
        XCTAssertEqual(session.modems.count, 1)
        XCTAssertNil(session.failure)
        XCTAssertEqual(session.modems.first?.volteStatus, .disabled)
        XCTAssertEqual(session.modems.first?.wifiCallingStatus, .disabled)
    }

    func testDeviceResponseAfterLogoutCannotRestoreIMSState() async throws {
        let stub = BackendStub()
        let session = try testSession(stub)
        await stub.holdNextModemRequest()
        let request = Task { await session.refresh(force: true) }
        await stub.waitForHeldRequest()
        session.logout()
        await stub.release()
        await request.value
        XCTAssertTrue(session.modems.isEmpty)
        XCTAssertEqual(session.authentication, .required)
    }

    func testTwoBackendsKeepSameDeviceIDsCapabilitiesAndFailuresIndependent() async throws {
        let firstStub = BackendStub()
        let secondStub = BackendStub()
        await firstStub.setModemJSON(PreviewBackendServer.modemJSON(volte: true))
        await secondStub.setModemJSON(PreviewBackendServer.modemJSON())
        await secondStub.setCapabilities(status: 200)
        let first = try testSession(firstStub, configuration: testConfiguration("one.example"))
        let second = try testSession(secondStub, configuration: testConfiguration("two.example"))
        async let a: Void = first.refresh(force: true)
        async let b: Void = second.refresh(force: true)
        _ = await (a, b)
        XCTAssertEqual(first.modems.first?.id, second.modems.first?.id)
        XCTAssertNotEqual(first.id, second.id)
        XCTAssertEqual(first.modems.first?.volteStatus, .connected)
        XCTAssertEqual(second.modems.first?.volteStatus, .disconnected)
        XCTAssertEqual(first.capabilities, ["wifiCalling", "futureFeature"])
        XCTAssertEqual(second.capabilities, [])
        await firstStub.setOffline(true)
        await first.refresh(force: true)
        XCTAssertEqual(first.modems.count, 1)
        XCTAssertTrue(first.failure?.isNetwork == true)
        XCTAssertNil(second.failure)
        XCTAssertEqual(second.statusKey, "status.online")
        await firstStub.setOffline(false)
        await first.refresh(force: true)
        XCTAssertNil(first.failure)
    }

    func testUnauthorizedOnlyRevokesTheMatchingBackend() async throws {
        let credentials = MemoryCredentials()
        let firstConfig = try testConfiguration("one.example")
        let secondConfig = try testConfiguration("two.example")
        credentials.tokens = [firstConfig.id: "one-token", secondConfig.id: "two-token"]
        let stub = BackendStub()
        await stub.setRequired(true)
        let first = try testSession(stub, configuration: firstConfig, credentials: credentials)
        await first.refresh(force: true)
        XCTAssertEqual(first.modems.count, 1)
        await stub.setModemStatus(401)
        await first.refresh(force: true)
        XCTAssertEqual(first.authentication, .required)
        XCTAssertTrue(first.modems.isEmpty)
        XCTAssertNil(credentials.tokens[first.id])
        XCTAssertEqual(credentials.tokens[secondConfig.id], "two-token")
    }

    func testCapabilityFailureDoesNotBlockDevices() async throws {
        let stub = BackendStub()
        await stub.setCapabilities(status: 503)
        let session = try testSession(stub)
        await session.refresh(force: true)
        XCTAssertEqual(session.modems.count, 1)
        XCTAssertNil(session.capabilities)
        XCTAssertEqual(session.capabilityFailure?.status, 503)
        XCTAssertEqual(session.statusKey, "status.online")
    }

    func testCommunityRequiresValidAppAndActivationSkipsBusinessRoutes() async throws {
        let stub = BackendStub()
        await stub.setAppStatus(404)
        let session = try testSession(stub)
        await session.refresh(force: true)
        XCTAssertEqual(session.license, .unknown)
        XCTAssertNotNil(session.failure)
        await stub.setLicense(authorized: false)
        await session.refresh(force: true)
        XCTAssertEqual(session.license, .activation)
        let count = await stub.count("/modems")
        XCTAssertEqual(count, 0)
    }

    func testOTPIsExplicitCooldownAndInvalidCodeAreIsolated() async throws {
        let stub = BackendStub()
        await stub.setRequired(true)
        let time = TestTime()
        let session = try testSession(stub, clock: time.clock)
        await session.refresh(force: true)
        let initial = await stub.count("/auth/otp")
        XCTAssertEqual(initial, 0)
        await session.sendOTP()
        await session.sendOTP()
        let once = await stub.count("/auth/otp")
        XCTAssertEqual(once, 1)
        time.advance(60)
        await stub.setSendStatus(429)
        await session.sendOTP()
        XCTAssertEqual(session.authFailure?.status, 429)
        XCTAssertEqual(session.resendAvailableAt, time.now().addingTimeInterval(60))
        await session.verifyOTP("000000")
        XCTAssertEqual(session.authFailure?.code, "invalid_otp")
        XCTAssertEqual(session.authFailure?.requestID, "otp-request")
        XCTAssertEqual(session.authentication, .required)
        await session.verifyOTP("123456")
        XCTAssertEqual(session.authentication, .authenticated)
        XCTAssertEqual(session.modems.count, 1)
    }

    func testOldUnauthorizedResponseCannotClearNewLogin() async throws {
        let stub = BackendStub()
        let credentials = MemoryCredentials()
        let config = try testConfiguration()
        credentials.tokens[config.id] = "old-token"
        await stub.setRequired(true)
        await stub.setModemStatus(401)
        await stub.holdNextModemRequest()
        let session = try testSession(stub, configuration: config, credentials: credentials)
        let old = Task { await session.refresh(force: true) }
        await stub.waitForHeldRequest()
        await stub.setModemStatus(200)
        await session.verifyOTP("123456")
        await stub.release()
        await old.value
        XCTAssertEqual(credentials.tokens[config.id], "new-token")
        XCTAssertEqual(session.authentication, .authenticated)
        XCTAssertEqual(session.modems.count, 1)
    }

    func testStopDiscardsAnInFlightResult() async throws {
        let stub = BackendStub()
        await stub.holdNextModemRequest()
        let session = try testSession(stub)
        let task = Task { await session.refresh(force: true) }
        await stub.waitForHeldRequest()
        session.stop()
        await stub.release()
        await task.value
        XCTAssertTrue(session.modems.isEmpty)
        XCTAssertFalse(session.isRefreshing)
        XCTAssertNil(session.failure)
    }

    func testBackoffAndManualRetry() async throws {
        let stub = BackendStub()
        await stub.setOffline(true)
        let time = TestTime()
        let session = try testSession(stub, clock: time.clock)
        for delay in [30.0, 60, 120, 120] {
            await session.refresh(force: true)
            XCTAssertEqual(session.nextRefresh, time.now().addingTimeInterval(delay))
        }
        let before = await stub.count("/license")
        await session.refresh()
        let after = await stub.count("/license")
        XCTAssertEqual(before, after)
        await stub.setOffline(false)
        await session.refresh(force: true)
        XCTAssertNil(session.failure)
        XCTAssertEqual(session.nextRefresh, time.now().addingTimeInterval(30))
    }

    func testKeychainReadAndWriteFailuresAreVisible() async throws {
        let credentials = MemoryCredentials()
        credentials.readFailure = true
        let stub = BackendStub()
        await stub.setRequired(true)
        let session = try testSession(stub, credentials: credentials)
        await session.refresh(force: true)
        XCTAssertEqual(session.failure?.code, "storage.keychainError")
        credentials.readFailure = false
        credentials.writeFailure = true
        await session.verifyOTP("123456")
        XCTAssertEqual(session.authFailure?.code, "storage.keychainError")
        XCTAssertTrue(session.modems.isEmpty)
    }

    func testDeviceDisappearanceRemovesItsSnapshot() async throws {
        let stub = BackendStub()
        let session = try testSession(stub)
        await session.refresh(force: true)
        await stub.setModemStatus(404)
        await session.refreshModem("device-1")
        XCTAssertTrue(session.modems.isEmpty)
        XCTAssertEqual(session.detailFailures["device-1"]?.status, 404)
    }

    func testActivationCreationRaceRecoversUsingLicenseAndApp() async throws {
        let stub = BackendStub()
        await stub.setLicense(authorized: false)
        let session = try testSession(stub)
        await session.refresh(force: true)
        await stub.setLicense(authorized: true)
        await stub.setPairing(status: "pending", createStatus: 404)
        await session.createPairing()
        XCTAssertEqual(session.license, .authorized)
        XCTAssertNil(session.pairingFailure)
        XCTAssertEqual(session.modems.count, 1)
    }

    func testExpiredPairingCanBeRecreated() async throws {
        let stub = BackendStub()
        let time = TestTime()
        await stub.setLicense(authorized: false)
        let session = try testSession(stub, clock: time.clock)
        await session.refresh(force: true)
        await stub.setExpiry(time.now().addingTimeInterval(-1))
        await session.createPairing()
        XCTAssertEqual(session.pairingFailure?.code, "activation.expired")
        await stub.setExpiry(time.now().addingTimeInterval(300))
        await session.createPairing()
        XCTAssertNotNil(session.pairing)
        XCTAssertNil(session.pairingFailure)
    }
    func testActivationPollingRecoversAfterLostSuccessResponse() async throws {
        let stub = BackendStub()
        let time = TestTime()
        let clock = BackendClock(now: { time.now() }, sleep: { _ in throw CancellationError() })
        await stub.setLicense(authorized: false)
        let session = try testSession(stub, clock: clock)
        await session.refresh(force: true)
        await session.createPairing()
        XCTAssertNotNil(session.pairing)
        await stub.setLicense(authorized: true)
        await stub.setPairing(status: "pending", pollStatus: 404)
        await session.observePairing()
        XCTAssertTrue(session.activationReady)
        XCTAssertNil(session.pairingFailure)
        XCTAssertEqual(session.modems.count, 1)
    }

    func testActivationWaitsForBusinessServerReadiness() async throws {
        let stub = BackendStub()
        await stub.setLicense(authorized: false)
        let session = try testSession(stub)
        await session.refresh(force: true)
        await stub.setLicense(authorized: true)
        await stub.setAppStatus(503)
        await session.refresh(force: true)
        XCTAssertEqual(session.license, .activation)
        XCTAssertFalse(session.activationReady)
        await stub.setAppStatus(200)
        await session.refresh(force: true)
        XCTAssertTrue(session.activationReady)
        XCTAssertEqual(session.modems.count, 1)
    }

    func testPendingPairingExpiresAndObservationCanStop() async throws {
        let stub = BackendStub()
        let time = TestTime()
        let clock = BackendClock(now: { time.now() }, sleep: { _ in throw CancellationError() })
        await stub.setLicense(authorized: false)
        let session = try testSession(stub, clock: clock)
        await session.refresh(force: true)
        await session.createPairing()
        time.advance(301)
        await session.observePairing()
        XCTAssertEqual(session.pairing?.status, "expired")
        XCTAssertFalse(session.activationReady)
    }

}

import XCTest
@testable import Sigmo

@MainActor
final class UpdatesTests: XCTestCase {
    func testBackgroundingFinishesCurrentPollWithoutStartingAnother() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        await server.setUpdateState("downloading")
        let state = UpdatesState(session: session, clock: BackendClock(sleep: { _ in try Task.checkCancellation() }))
        await state.load()
        await server.holdNext("/update-installations/current")
        let poll = Task { await state.observe() }
        await server.waitForHeldRequest()
        session.foreground = false
        await server.release()
        await poll.value
        XCTAssertNotNil(state.snapshot)
        XCTAssertNil(state.failure)
        let requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/update-installations/current") == true }.count, 1)
    }

    func testClearedUpdatesCanReloadWithoutRestoringOlderSnapshot() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = UpdatesState(session: session)
        await server.holdNext("/settings/updates")
        let previous = Task { await state.load() }
        await server.waitForHeldRequest()

        state.clear()
        XCTAssertFalse(state.working)
        await server.setEdition("community")
        await state.load()
        XCTAssertEqual(state.snapshot?.current.edition, "community")

        await server.release()
        await previous.value
        XCTAssertEqual(state.snapshot?.current.edition, "community")
        XCTAssertFalse(state.working)
        XCTAssertNil(state.failure)
    }

    func testCommunityAndUnsupportedDistributions() async throws {
        for (edition, distribution, reason) in [
            ("community", "standalone", nil as String?),
            ("pro", "container", "container"),
            ("pro", "developer", "developer_build"),
            ("pro", "standalone", "release_key_missing")
        ] {
            let server = try SettingsTestServer()
            await server.setEdition(edition)
            await server.setDistribution(distribution, reason: reason)
            let session = try settingsSession(server)
            await session.refresh(force: true)
            let state = UpdatesState(session: session)
            await state.load()
            XCTAssertTrue(state.canCheck)
            XCTAssertFalse(state.canInstall)
            await state.check()
            XCTAssertEqual(state.canInstall, reason == nil)
            await state.save(automatic: false, channel: "dev")
            XCTAssertEqual(state.snapshot?.settings.channel, edition == "community" ? "stable" : "dev")
        }
    }

    func testChannelChangeChecksOnceAndFailedSaveKeepsPreferences() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = UpdatesState(session: session)
        await state.load()
        await state.save(automatic: true, channel: "dev")
        XCTAssertEqual(state.snapshot?.settings, .init(automatic: true, channel: "dev"))
        var requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/update-checks") == true }.count, 1)
        await state.save(automatic: false, channel: "dev")
        requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.url?.path.hasSuffix("/update-checks") == true }.count, 1)
        await server.setError("/settings/updates", status: 500)
        await state.save(automatic: true, channel: "stable")
        XCTAssertEqual(state.snapshot?.settings, .init(automatic: false, channel: "dev"))
        XCTAssertNotNil(state.failure)
    }

    func testInstallationRecoversAcrossRestartWithoutRepeatingPost() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let time = TestTime()
        let state = UpdatesState(session: session, clock: BackendClock(now: { time.now() }, sleep: { _ in time.advance(5); try Task.checkCancellation() }))
        await state.load(); await state.check(); await state.install()
        XCTAssertEqual(state.snapshot?.state, "downloading")
        XCTAssertFalse(state.canInstall)
        await state.observe()
        XCTAssertEqual(state.snapshot?.state, "idle")
        XCTAssertEqual(state.snapshot?.current.version, "1.1.0")
        XCTAssertEqual(session.information?.version, "1.1.0")
        XCTAssertFalse(state.snapshot?.updateAvailable ?? true)
        XCTAssertNil(state.failure)
        let requests = await server.allRequests()
        XCTAssertEqual(requests.filter { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/update-installations") == true }.count, 1)
    }

    func testRestartRecoveryAuthenticatesTheAppReadinessProbe() async throws {
        let server = try SettingsTestServer()
        await server.setOTP(true)
        await server.requireAppToken("fixture-token")
        let credentials = MemoryCredentials()
        let session = try settingsSession(server, credentials: credentials)
        credentials.tokens[session.id] = storedCredential("fixture-token")
        await session.refresh(force: true)
        let time = TestTime()
        let state = UpdatesState(session: session, clock: BackendClock(now: { time.now() }, sleep: { _ in
            time.advance(5)
            try Task.checkCancellation()
        }))
        await state.load()
        await state.check()
        await state.install()
        await state.observe()

        XCTAssertEqual(state.snapshot?.state, "idle")
        XCTAssertEqual(state.snapshot?.current.version, "1.1.0")
        XCTAssertNil(state.failure)
        let requests = await server.allRequests().filter { $0.url?.path.hasSuffix("/app") == true }
        XCTAssertGreaterThanOrEqual(requests.count, 3)
        XCTAssertTrue(requests.allSatisfy { $0.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token" })
    }

    func testRestartTimeoutRequiresReadBeforeAnotherInstallation() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let time = TestTime()
        let state = UpdatesState(session: session, clock: BackendClock(now: { time.now() }, sleep: { _ in time.advance(30); try Task.checkCancellation() }))
        await state.load(); await state.check(); await state.install()
        await server.setOffline(true)
        await state.observe()
        XCTAssertTrue(state.timedOut)
        XCTAssertEqual(state.failure?.code, "settings.native.restartTimeout")
        XCTAssertFalse(state.canInstall)
        XCTAssertFalse(state.shouldPoll)
        await server.setOffline(false)
        await state.load()
        XCTAssertFalse(state.timedOut)
        XCTAssertTrue(state.shouldPoll)
        await state.observe()
        XCTAssertEqual(state.snapshot?.state, "idle")
    }

    func testReentryObservesExistingInstallationAndCancellationStopsReads() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        await server.setUpdateState("downloading")
        let state = UpdatesState(session: session, clock: BackendClock(sleep: { _ in throw CancellationError() }))
        await state.load()
        XCTAssertTrue(state.shouldPoll)
        await state.observe()
        XCTAssertNil(state.failure)
        let requests = await server.allRequests()
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/update-installations/current") == true })
        XCTAssertFalse(requests.contains { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/update-installations") == true })
    }

    func testInstallationFailureAndLostResponse() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = UpdatesState(session: session)
        await state.load(); await state.check()
        await server.setError("/update-installations", status: 409)
        await state.install()
        XCTAssertNotNil(state.failure)
        XCTAssertFalse(state.recovering)
        await server.setError("/update-installations", status: 502)
        await state.install()
        XCTAssertTrue(state.recovering)
        XCTAssertFalse(state.canInstall)
        XCTAssertNil(state.failure)
    }
    func testCertificateFailureIsNotTreatedAsRestart() async throws {
        let server = try SettingsTestServer()
        let session = try settingsSession(server)
        await session.refresh(force: true)
        let state = UpdatesState(session: session)
        await state.load()
        await state.check()
        await server.setTransportError(.serverCertificateUntrusted)
        await state.install()
        XCTAssertNotNil(state.failure)
        XCTAssertFalse(state.recovering)
        XCTAssertFalse(state.shouldPoll)
    }

}

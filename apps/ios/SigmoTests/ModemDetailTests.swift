import XCTest
@testable import Sigmo

@MainActor
final class ModemDetailTests: XCTestCase {
    func testSecureElementStorageUsesWholeKiB() {
        let cases: [(bytes: Int64?, expected: String)] = [
            (nil, "—"), (0, "0KiB"), (1023, "0KiB"),
            (1024, "1KiB"), (1536, "1KiB"), (65536, "64KiB"),
            (1048576, "1024KiB")
        ]
        for item in cases {
            XCTAssertEqual(SecureElementMetadata.storageText(item.bytes), item.expected)
        }
    }

    private func make(_ server: DetailFixtureServer = DetailFixtureServer(), host: String = "detail.example") async throws -> ModemDetailState {
        let config = BackendConfiguration(name: host, address: try BackendAddress.normalize("https://\(host)/prefix"))
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: config, transport: { try await server.respond($0) }, socketFactory: { try await server.socket($0) }))
        await session.refresh(force: true)
        let detail = ModemDetailState(session: session, modemID: "device-1")
        await detail.load()
        return detail
    }
    private func wait(_ predicate: @MainActor () -> Bool, file: StaticString = #filePath, line: UInt = #line) async throws {
        for _ in 0..<400 {
            if predicate() { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        XCTFail("State did not arrive", file: file, line: line)
    }
    private func event(_ text: String) throws -> ESIMServerMessage { try JSONDecoder().decode(ESIMServerMessage.self, from: Data(text.utf8)) }

    func testDataReadinessPreservesSnapshotDuringRefresh() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        detail.clear()
        XCTAssertFalse(detail.elementsLoaded)
        XCTAssertFalse(detail.profilesLoaded)

        await server.holdNext("/ses")
        let initialLoad = Task { await detail.load() }
        await server.waitForHeldRequest()
        XCTAssertFalse(detail.elementsLoaded)
        XCTAssertFalse(detail.profilesLoaded)
        await server.release()
        await initialLoad.value
        XCTAssertTrue(detail.elementsLoaded)
        XCTAssertTrue(detail.profilesLoaded)

        let profileIDs = detail.groups.flatMap(\.profiles).map(\.id)
        await server.holdNext("/ses")
        let refresh = Task { await detail.load() }
        await server.waitForHeldRequest()
        XCTAssertTrue(detail.elementsLoaded)
        XCTAssertTrue(detail.profilesLoaded)
        XCTAssertEqual(detail.groups.flatMap(\.profiles).map(\.id), profileIDs)
        await server.release()
        await refresh.value
    }

    func testEmptyAndFailedProfileReadsFinishLoading() async throws {
        for status in [200, 503] {
            let server = DetailFixtureServer()
            await server.setProfilesData(Data(#"{"ses":[]}"#.utf8))
            await server.setStatus("/esims", status)
            let detail = try await make(server)
            XCTAssertTrue(detail.elementsLoaded, "HTTP \(status)")
            XCTAssertTrue(detail.profilesLoaded, "HTTP \(status)")
            XCTAssertTrue(detail.groups.isEmpty, "HTTP \(status)")
            XCTAssertEqual(detail.profilesFailure?.status, status == 503 ? 503 : nil)

            await server.setProfilesData(nil)
            await server.setStatus("/esims", 200)
            await detail.load()
            XCTAssertNil(detail.profilesFailure)
            XCTAssertFalse(detail.groups.isEmpty)
        }
    }

    func testOldSIMReplyFailureDoesNotCloseReplacementSession() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let sim = SIMApplicationState(session: detail.session, modemID: detail.modemID)
        let oldStream = Task { await sim.observe() }
        try await wait { sim.available }
        await server.holdNextSocketSend()
        let reply = Task { await sim.send(.init(type: "menu_selection", itemId: 1)) }
        await server.waitForHeldRequest()
        sim.stop()
        let replacement = Task { await sim.observe() }
        try await wait { sim.available }
        await server.release()
        await reply.value
        XCTAssertTrue(sim.available)
        XCTAssertNotNil(sim.root)
        XCTAssertNil(sim.failure)
        sim.stop()
        await oldStream.value
        await replacement.value
    }

    func testOldDownloadReplyFailureDoesNotChangeRestartedFlow() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")

        flow.input.smdp = "smdp.example"
        flow.startDownload()
        let oldRun = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await server.holdNextSocketSend()
        let reply = Task { await flow.send(.init(type: "confirm", accept: true)) }
        await server.waitForHeldRequest()
        await server.emitRaw(#"{"type":"error","message":"Disconnected"}"#)
        try await wait { flow.phase == .failed }
        flow.backToForm()
        flow.input.smdp = "smdp.example"
        flow.startDownload()
        let replacement = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await server.release()
        await reply.value
        XCTAssertEqual(flow.phase, .preview)
        XCTAssertNil(flow.failure)
        XCTAssertFalse(flow.sending)
        await flow.cancel()
        await oldRun.value
        await replacement.value
    }

    func testDuplicateWriteIsBlockedUntilSnapshotRefreshCompletes() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let profile = detail.groups[0].profiles[1]
        await server.holdNext("/esims")
        let request = Task { await detail.rename(profile, name: "First") }
        await server.waitForHeldRequest()

        XCTAssertTrue(detail.busy)
        let duplicate = await detail.rename(profile, name: "Second")
        XCTAssertFalse(duplicate)
        await server.release()
        let saved = await request.value
        XCTAssertTrue(saved)
        XCTAssertFalse(detail.busy)
        XCTAssertEqual(detail.currentProfile(profile.id)?.name, "First")
        let writes = await server.allRequests().filter { $0.url?.path.hasSuffix("/nickname") == true }
        XCTAssertEqual(writes.count, 1)
    }

    func testStaleDisabledProfileCannotEnableOrDeleteActiveLine() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let stale = detail.groups[0].profiles[1]
        let enabled = await detail.enable(stale)
        XCTAssertTrue(enabled)
        let count = await server.allRequests().count

        let duplicate = await detail.enable(stale)
        let deleted = await detail.delete(stale)
        XCTAssertFalse(duplicate)
        XCTAssertFalse(deleted)
        let updatedCount = await server.allRequests().count
        XCTAssertEqual(updatedCount, count)
        XCTAssertEqual(detail.currentProfile(stale.id)?.enabled, true)
    }

    func testNewConnectionReadWinsOverOlderResponse() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let internet = InternetState(detail: detail, profileID: "se-a:8986000000000000001")
        await server.holdNext("/internet-connections/current")
        let oldRead = Task { await internet.load() }
        await server.waitForHeldRequest()
        let id = detail.modemID
        // Simulate another client disconnecting without changing this view's operation revision.
        try await detail.session.protectedRequest { api, token in
            try await api.detailMutation(id, path: ["internet-connections", "current"], method: "DELETE", token: token)
        }
        await internet.load()
        XCTAssertEqual(internet.connection?.status, "disconnected")

        await server.release()
        await oldRead.value
        XCTAssertEqual(internet.connection?.status, "disconnected")
        XCTAssertNil(internet.loadFailure)
    }

    func testSlotChangeDoesNotRetainPreviousSIMDataOnPartialFailure() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let internet = InternetState(detail: detail, profileID: "se-a:8986000000000000001")
        await internet.load()
        await server.setStatus("/ses", 503)
        await server.setStatus("/esims", 503)

        let switched = await detail.switchSlot(2)
        XCTAssertTrue(switched)
        XCTAssertEqual(detail.modem?.sim.slot, 2)
        XCTAssertTrue(detail.elements.isEmpty)
        XCTAssertTrue(detail.groups.isEmpty)
        await internet.load()
        XCTAssertNil(internet.connection)
        XCTAssertFalse(internet.scope.available)
        XCTAssertEqual(detail.elementsFailure?.status, 503)
        XCTAssertEqual(detail.profilesFailure?.status, 503)
    }

    func testRemovedModemClearsSIMAndConnectionSnapshots() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let internet = InternetState(detail: detail, profileID: "se-a:8986000000000000001")
        await internet.load()
        await server.setStatus("/modems/device-1", 404)
        await detail.load()

        XCTAssertNil(detail.modem)
        XCTAssertTrue(detail.elements.isEmpty)
        XCTAssertTrue(detail.groups.isEmpty)
        await internet.load()
        XCTAssertNil(internet.connection)
        XCTAssertFalse(internet.scope.available)
        XCTAssertEqual(detail.failure?.status, 404)
    }

    func testOldCancellationDoesNotInterruptReplacementDownload() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")

        flow.input.smdp = "smdp.example"
        flow.startDownload()
        let oldRun = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await server.holdNextSocketSend()
        let cancellation = Task { await flow.cancel() }
        await server.waitForHeldRequest()

        flow.interrupt()
        await oldRun.value
        flow.backToForm()
        flow.input.smdp = "smdp.example"
        flow.startDownload()
        let replacement = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        let replacementID = flow.runID
        await server.release()
        await cancellation.value

        XCTAssertEqual(flow.runID, replacementID)
        XCTAssertEqual(flow.phase, .preview)
        XCTAssertNil(flow.failure)
        await flow.cancel()
        await replacement.value
    }

    func testOldSIMDismissalDoesNotHideReplacementSession() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let sim = SIMApplicationState(session: detail.session, modemID: detail.modemID)
        let oldStream = Task { await sim.observe() }
        try await wait { sim.available }
        sim.open()
        await sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = sim.page { true } else { false } }
        await server.holdNextSocketSend()
        let dismissal = Task { await sim.dismiss() }
        await server.waitForHeldRequest()

        sim.stop()
        await oldStream.value
        let replacement = Task { await sim.observe() }
        try await wait { sim.available }
        sim.open()
        await server.release()
        await dismissal.value

        XCTAssertTrue(sim.available)
        XCTAssertTrue(sim.presented)
        XCTAssertNotNil(sim.page)
        XCTAssertNil(sim.failure)
        sim.stop()
        await replacement.value
    }

    func testEditedTransferIMEIIgnoresObsoleteProfileResponsesAndErrors() async throws {
        for status in [200, 503] {
            let server = DetailFixtureServer()
            let detail = try await make(server)
            let flow = ESIMFlowState(detail: detail, seID: "se-a")

            await flow.loadSources()
            flow.selectSource(try XCTUnwrap(flow.sources.first { $0.requiresSourceImei }))
            flow.sourceIMEI = "123456789012345"
            await server.setStatus("/esim-transfers/source-profiles", status)
            await server.holdNext("/esim-transfers/source-profiles")
            let request = Task { await flow.loadProfiles() }
            await server.waitForHeldRequest()
            flow.sourceIMEI = "543210987654321"
            await server.release()
            await request.value

            XCTAssertTrue(flow.profiles.isEmpty, "HTTP \(status)")
            XCTAssertNil(flow.failure, "HTTP \(status)")
            XCTAssertFalse(flow.busy)
            XCTAssertFalse(flow.canTransfer)
        }
    }

    func testEditedTransferIMEIRequiresReloadBeforeStarting() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")

        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first { $0.requiresSourceImei }))
        flow.sourceIMEI = "123456789012345"
        await flow.loadProfiles()
        let stale = try XCTUnwrap(flow.profiles.first)
        flow.selectProfile(stale)
        XCTAssertTrue(flow.canTransfer)

        flow.sourceIMEI = "543210987654321"
        XCTAssertFalse(flow.canTransfer)
        flow.startTransfer()
        XCTAssertEqual(flow.phase, .profiles)
        XCTAssertNil(flow.runID)
        await flow.loadProfiles()
        XCTAssertNil(flow.selectedProfile)
        flow.selectProfile(try XCTUnwrap(flow.profiles.first))
        XCTAssertTrue(flow.canTransfer)
    }

    func testLeavingTransferSetupIgnoresLateSourceResponse() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")

        await server.holdNext("/esim-transfers/sources")
        let request = Task { await flow.loadSources() }
        await server.waitForHeldRequest()
        flow.backToForm()
        await server.release()
        await request.value

        XCTAssertEqual(flow.phase, .form)
        XCTAssertFalse(flow.transfer)
        XCTAssertFalse(flow.busy)
        XCTAssertTrue(flow.sources.isEmpty)
        XCTAssertNil(flow.failure)
    }

    func testLPAAndNicknameValidation() throws {
        let values: [(String, String, String, Bool)] = [
            ("LPA:1$smdp.example$code", "smdp.example", "code", false),
            (" LPA:1 $ smdp.example $ abc def $$1 ", "smdp.example", "abcdef", true),
            ("LPA:1$smdp.example$$oid$0", "smdp.example", "oid", false),
            ("LPA:1$smdp.example$", "smdp.example", "", false)
        ]
        for (raw, address, code, required) in values {
            let value = try XCTUnwrap(LPAInput.parse(raw))
            XCTAssertEqual(value.smdp, address); XCTAssertEqual(value.activationCode, code); XCTAssertEqual(value.confirmationRequired, required)
        }
        for invalid in ["", "https://example.com", "LPA:1$$code", "LPA:1$only"] { XCTAssertNil(LPAInput.parse(invalid)) }
        XCTAssertFalse(ModemDetailState.validNickname("  "))
        XCTAssertTrue(ModemDetailState.validNickname(String(repeating: "中", count: 21)))
        XCTAssertFalse(ModemDetailState.validNickname(String(repeating: "中", count: 22)))
        XCTAssertEqual(PhoneNumberDisplay.submission("138 0000 0000", regionCode: "CN"), "13800000000")
        XCTAssertEqual(PhoneNumberDisplay.submission("+86 138 0000 0000", regionCode: "CN"), "+8613800000000")
        XCTAssertEqual(PhoneNumberDisplay.submission("10086", regionCode: "CN"), "10086")
    }

    func testCompositeProfileIdentityAndMutations() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        XCTAssertEqual(detail.elements.count, 2)
        let profiles = detail.groups.flatMap(\.profiles)
        XCTAssertEqual(profiles.count, Set(profiles.map(\.id)).count)
        let first = try XCTUnwrap(profiles.first(where: { $0.enabled }))
        let rejected = await detail.delete(first)
        XCTAssertFalse(rejected)
        let second = profiles[1]
        let renamed = await detail.rename(second, name: "旅行")
        XCTAssertTrue(renamed)
        XCTAssertEqual(detail.groups[0].profiles[1].name, "旅行")
        XCTAssertEqual(detail.groups[1].profiles[0].name, "Backup")
        let enabled = await detail.enable(second)
        XCTAssertTrue(enabled)
        XCTAssertTrue(detail.groups[0].profiles[1].enabled)
        let removed = await detail.delete(detail.groups[1].profiles[0])
        XCTAssertTrue(removed)
        XCTAssertTrue(detail.groups[1].profiles.isEmpty)
        let requests = await server.allRequests()
        let rename = try XCTUnwrap(requests.first { $0.url?.path.hasSuffix("/nickname") == true })
        XCTAssertEqual(rename.httpMethod, "PUT")
        XCTAssertEqual(rename.url?.path, "/prefix/api/v1/modems/device-1/ses/se-a/esims/8986000000000000002/nickname")
    }

    func testConfirmedDeletionRemovesProfileWhenRefreshFails() async throws {
        for suffix in ["/modems/device-1", "/ses", "/esims"] {
            let server = DetailFixtureServer()
            let detail = try await make(server)
            let profile = detail.groups[0].profiles[1]
            let other = detail.groups[1].profiles[0]
            await server.setStatus(suffix, 503)

            let deleted = await detail.delete(profile)

            XCTAssertTrue(deleted, suffix)
            XCTAssertNil(detail.currentProfile(profile.id), suffix)
            XCTAssertNotNil(detail.currentProfile(other.id), suffix)
            XCTAssertEqual((detail.failure ?? detail.elementsFailure ?? detail.profilesFailure)?.status, 503, suffix)
            XCTAssertFalse(detail.busy)
            let count = await server.allRequests().count
            let enabled = await detail.enable(profile)
            let repeated = await detail.delete(profile)
            XCTAssertFalse(enabled)
            XCTAssertFalse(repeated)
            let updatedCount = await server.allRequests().count
            XCTAssertEqual(updatedCount, count)
        }
    }

    func testDeletingProfileBlocksMutationsAndFailureAllowsRetry() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let profile = detail.groups[0].profiles[1]
        let suffix = "/ses/\(profile.seId)/esims/\(profile.iccid)"
        await server.setStatus(suffix, 503)
        await server.holdNext(suffix)
        let deletion = Task { await detail.delete(profile) }
        await server.waitForHeldRequest()

        XCTAssertTrue(detail.busy)
        XCTAssertNotNil(detail.currentProfile(profile.id))
        let count = await server.allRequests().count
        let enabled = await detail.enable(profile)
        let renamed = await detail.rename(profile, name: "Changed")
        let reminder = await detail.saveReminder(profile: profile, value: nil)
        let repeated = await detail.delete(profile)
        XCTAssertFalse(enabled)
        XCTAssertFalse(renamed)
        XCTAssertFalse(reminder)
        XCTAssertFalse(repeated)
        let updatedCount = await server.allRequests().count
        XCTAssertEqual(updatedCount, count)

        await server.release()
        let deleted = await deletion.value
        XCTAssertFalse(deleted)
        XCTAssertFalse(detail.busy)
        XCTAssertNotNil(detail.currentProfile(profile.id))
        XCTAssertEqual(detail.failure?.status, 503)

        await server.setStatus(suffix, 200)
        let retried = await detail.delete(profile)
        XCTAssertTrue(retried)
        XCTAssertNil(detail.currentProfile(profile.id))
    }

    func testConnectionsRetainPreferencesAndWiFiDisconnectDoesNotDisable() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let internet = InternetState(detail: detail, profileID: "se-a:8986000000000000001")
        let wifi = WiFiCallingState(detail: detail, profileID: "se-a:8986000000000000001")
        await internet.load()
        await wifi.load()
        await internet.setConnected(false)
        XCTAssertEqual(internet.connection?.status, "disconnected")
        await internet.setConnected(true)
        XCTAssertEqual(internet.connection?.status, "connected")
        await wifi.setEnabled(true)
        XCTAssertTrue(wifi.settings?.connected == true)
        await wifi.disconnect()
        XCTAssertTrue(wifi.settings?.enabled == true)
        XCTAssertFalse(wifi.settings?.connected ?? true)
        let requests = await server.allRequests()
        let connect = try XCTUnwrap(requests.first { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/internet-connections") == true })
        let preferences = try JSONDecoder().decode(InternetPreferences.self, from: XCTUnwrap(connect.httpBody))
        XCTAssertEqual(preferences.apn, "example.apn"); XCTAssertEqual(preferences.apnPassword, "secret")
        XCTAssertFalse(preferences.defaultRoute); XCTAssertTrue(preferences.proxyEnabled); XCTAssertTrue(preferences.alwaysOn)
        let settings = requests.filter { $0.httpMethod == "PUT" && $0.url?.path.hasSuffix("/wifi-calling/settings") == true }
        XCTAssertEqual(settings.count, 1)
        let payload = try JSONDecoder().decode(DetailWiFiPayload.self, from: XCTUnwrap(settings.first?.httpBody))
        XCTAssertEqual(payload.underlay.mode, "modem"); XCTAssertEqual(payload.underlay.modemId, "device-2")
    }

    func testPINSlotNumberAndReminders() async throws {
        let server = DetailFixtureServer(physical: true, locked: true), detail = try await make(server)
        XCTAssertTrue(detail.needsPIN)
        let badPIN = await detail.unlock("0000")
        XCTAssertFalse(badPIN)
        let unlocked = await detail.unlock("1234")
        XCTAssertTrue(unlocked); XCTAssertFalse(detail.needsPIN)
        let switched = await detail.switchSlot(2)
        XCTAssertTrue(switched); XCTAssertEqual(detail.modem?.sim.slot, 2)
        let updated = await detail.updateNumber("10086")
        XCTAssertTrue(updated); XCTAssertEqual(detail.modem?.number, "10086")
        let value = ReminderSubmission(scheduledAt: Date(timeIntervalSince1970: 1_900_000_000), repeatDays: 7, content: "Renew")
        let saved = await detail.saveReminder(profile: nil, value: value)
        XCTAssertTrue(saved); XCTAssertEqual(detail.modem?.sim.reminder?.repeatDays, 7)
        let cleared = await detail.saveReminder(profile: nil, value: nil)
        XCTAssertTrue(cleared); XCTAssertNil(detail.modem?.sim.reminder)
    }

    func testPartialFailuresRetainSnapshotAnd401IsIsolated() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let other = try await make(host: "other.example")
        await server.setStatus("/ses", 503)
        await detail.load()
        XCTAssertEqual(detail.elements.count, 2)
        XCTAssertEqual(detail.elementsFailure?.status, 503)
        XCTAssertEqual(detail.groups.count, 2)
        await server.setStatus("/esims", 401)
        await detail.load()
        XCTAssertEqual(detail.session.authentication, .required)
        XCTAssertTrue(detail.session.modems.isEmpty)
        XCTAssertTrue(detail.elements.isEmpty)
        XCTAssertTrue(detail.groups.isEmpty)
        XCTAssertEqual(other.session.modems.count, 2)
    }

    func testClearedStateRejectsLateProfileResponse() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        await server.holdNext("/esims")
        let request = Task { await detail.load() }
        await server.waitForHeldRequest()
        detail.clear()
        await server.release()
        await request.value
        XCTAssertTrue(detail.groups.isEmpty); XCTAssertTrue(detail.elements.isEmpty)
    }

    func testDuplicateWritesDoNotRepeatSubmission() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let profile = detail.groups[0].profiles[1]
        await server.holdNext("/nickname")
        let request = Task { await detail.rename(profile, name: "First") }
        await server.waitForHeldRequest()
        let duplicate = await detail.rename(profile, name: "Second")
        XCTAssertFalse(duplicate)
        await server.release()
        let saved = await request.value
        XCTAssertTrue(saved)
        let writes = await server.allRequests().filter { $0.url?.path.hasSuffix("/nickname") == true }
        XCTAssertEqual(writes.count, 1); XCTAssertEqual(detail.groups[0].profiles[1].name, "First")
    }

    func testDiscoveryRunsOnceForTheOwningSecureElement() async throws {
        for seID in ["se-a", "se-b"] {
            let server = DetailFixtureServer()
            let detail = try await make(server)
            let flow = ESIMFlowState(detail: detail, seID: seID)
            await server.holdNext("/esim-discoveries")
            let request = Task { await flow.discover() }
            await server.waitForHeldRequest()
            await flow.discover()
            await server.release()
            await request.value
            XCTAssertEqual(flow.discoveries?.count, 1)
            XCTAssertNil(flow.runID)
            XCTAssertEqual(flow.phase, .form)

            flow.backToForm()
            await flow.discover()
            let requests = await server.allRequests().filter { $0.url?.path.hasSuffix("/esim-discoveries") == true }
            XCTAssertEqual(requests.count, 1)
            XCTAssertTrue(requests.first?.url?.path.hasSuffix("/ses/\(seID)/esim-discoveries") == true)
            let messages = await server.allMessages()
            XCTAssertTrue(messages.isEmpty)

            let reopened = ESIMFlowState(detail: detail, seID: seID)
            await reopened.discover()
            let reopenedRequests = await server.allRequests().filter { $0.url?.path.hasSuffix("/esim-discoveries") == true }
            XCTAssertEqual(reopenedRequests.count, 2)
        }
    }

    func testEmptyOrFailedDiscoveryKeepsManualSetupAvailable() async throws {
        for status in [200, 503] {
            let server = DetailFixtureServer()
            await server.setDiscoveries([])
            await server.setStatus("/esim-discoveries", status)
            let detail = try await make(server)
            let flow = ESIMFlowState(detail: detail, seID: "se-a")
            await flow.discover()
            XCTAssertEqual(flow.phase, .form)
            XCTAssertNil(flow.runID)
            XCTAssertFalse(flow.busy)
            XCTAssertTrue(flow.didDiscover)
            XCTAssertEqual(flow.discoveries?.isEmpty, status == 200 ? true : nil)
            XCTAssertEqual(flow.failure?.status, status == 503 ? 503 : nil)
            flow.input = LPAInput(smdp: "smdp.example")
            XCTAssertTrue(flow.canStart)
            await flow.discover()
            let requests = await server.allRequests().filter { $0.url?.path.hasSuffix("/esim-discoveries") == true }
            XCTAssertEqual(requests.count, 1)
        }
    }

    func testInterruptedDiscoveryDiscardsLateResults() async throws {
        for status in [200, 503] {
            let server = DetailFixtureServer()
            await server.setStatus("/esim-discoveries", status)
            let detail = try await make(server)
            let flow = ESIMFlowState(detail: detail, seID: "se-b")
            await server.holdNext("/esim-discoveries")
            let request = Task { await flow.discover() }
            await server.waitForHeldRequest()
            flow.interrupt()
            await server.release()
            await request.value
            XCTAssertNil(flow.discoveries)
            XCTAssertNil(flow.failure)
            XCTAssertNil(flow.runID)
            XCTAssertFalse(flow.busy)
            XCTAssertEqual(flow.phase, .form)
        }
    }

    func testInvalidSecureElementCannotDiscoverOrDownload() async throws {
        let server = DetailFixtureServer()
        let detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "missing")
        flow.input = LPAInput(smdp: "smdp.example")
        XCTAssertFalse(flow.validSE)
        XCTAssertFalse(flow.canStart)
        await flow.discover()
        flow.startDownload()
        XCTAssertFalse(flow.didDiscover)
        XCTAssertNil(flow.runID)
        let requests = await server.allRequests().filter { $0.url?.path.hasSuffix("/esim-discoveries") == true }
        XCTAssertTrue(requests.isEmpty)
    }

    func testDownloadPreviewConfirmationAndAuthoritativeCompletion() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-b")
        XCTAssertTrue(flow.validSE)
        await flow.discover()
        let discovery = try XCTUnwrap(flow.discoveries?.first)
        flow.selectDiscovery(discovery)
        XCTAssertEqual(flow.phase, .form)
        XCTAssertEqual(flow.input.smdp, "smdp.example")
        XCTAssertEqual(flow.discoveries?.first?.address, discovery.address)
        XCTAssertNil(flow.runID)
        flow.input = LPAInput(smdp: "smdp.example", activationCode: " secret ")
        flow.startDownload()
        let run = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await flow.send(.init(type: "confirm", accept: true))
        try await wait { flow.phase == .confirmation }
        await flow.send(.init(type: "confirmation_code", code: "1234"))
        await run.value
        XCTAssertEqual(flow.phase, .completed)
        XCTAssertEqual(detail.groups[1].profiles.count, 2)
        XCTAssertTrue(flow.input.activationCode.isEmpty)
        let requests = await server.allRequests()
        let socketRequest = try XCTUnwrap(requests.first { $0.url?.scheme == "wss" })
        XCTAssertTrue(socketRequest.url!.absoluteString.contains("/prefix/api/v1/modems/device-1/esim-downloads/sessions"))
        let messages = await server.allMessages()
        let start = try JSONDecoder().decode(DetailStart.self, from: XCTUnwrap(messages.first))
        XCTAssertEqual(start.seId, "se-b"); XCTAssertEqual(start.activationCode, "secret")
    }

    func testDownloadCompletesWhileInBackgroundWithoutReplay() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-b")
        flow.input = LPAInput(smdp: "smdp.example")
        flow.startDownload()
        let run = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await flow.send(.init(type: "confirm", accept: true))
        try await wait { flow.phase == .confirmation }
        detail.session.foreground = false
        await flow.send(.init(type: "confirmation_code", code: "1234"))
        await run.value
        XCTAssertEqual(flow.phase, .completed)
        XCTAssertEqual(detail.groups[1].profiles.count, 2)
        XCTAssertNil(flow.failure)
        detail.session.foreground = true
        let commands = try await server.allMessages().map { try JSONDecoder().decode(DetailCommand.self, from: $0).type }
        XCTAssertEqual(commands.filter { $0 == "start" }.count, 1)
    }

    func testCancellationDoesNotReplayDownload() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")
        flow.input = LPAInput(smdp: "smdp.example")
        flow.startDownload()
        let run = Task { await flow.run() }
        try await wait { flow.phase == .preview }
        await flow.cancel()
        await run.value
        await flow.run()
        XCTAssertEqual(flow.phase, .failed)
        XCTAssertNil(flow.runID)
        let commands = try await server.allMessages().map { try JSONDecoder().decode(DetailCommand.self, from: $0).type }
        XCTAssertEqual(commands.filter { $0 == "start" }.count, 1)
        XCTAssertTrue(commands.contains("cancel"))
        XCTAssertEqual(detail.groups[0].profiles.count, 2)
    }

    func testTransferIncludesUserInputSourceDeletionAndSourceRefresh() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-b")

        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first))
        await flow.loadProfiles()
        flow.selectProfile(try XCTUnwrap(flow.profiles.first))
        flow.startTransfer()
        let run = Task { await flow.run() }
        try await wait { flow.phase == .userInput }
        await flow.send(.init(type: "user_input", accept: true, response: "1234"))
        try await wait { flow.phase == .sourceDeletion }
        await flow.send(.init(type: "source_deletion", accept: true))
        await run.value
        XCTAssertEqual(flow.phase, .completed)
        let requests = await server.allRequests()
        XCTAssertTrue(requests.contains { $0.url?.path.hasSuffix("/modems/device-2") == true })
        let messages = await server.allMessages()
        let start = try JSONDecoder().decode(DetailStart.self, from: XCTUnwrap(messages.first))
        XCTAssertEqual(start.seId, "se-b")
    }

    func testUnknownMessagesAndLateTerminalEvents() async throws {
        let detail = try await make(), flow = ESIMFlowState(detail: detail, seID: "se-a")
        flow.input = LPAInput(smdp: "smdp.example"); flow.startDownload()
        XCTAssertThrowsError(try flow.consume(event(#"{"type":"unexpected"}"#)))
        XCTAssertThrowsError(try flow.consume(event(#"{"type":"preview"}"#)))
        try flow.consume(event(#"{"type":"completed"}"#))
        try flow.consume(event(#"{"type":"error","message":"late"}"#))
        XCTAssertEqual(flow.phase, .completed)
    }

    func testSIMApplicationMessagesAndResponseTypes() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let sim = SIMApplicationState(session: detail.session, modemID: detail.modemID)
        let task = Task { await sim.observe() }
        try await wait { sim.available }
        sim.open()
        await sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = sim.page { true } else { false } }
        for (type, json) in [
            ("display_text", #"{"type":"display_text","text":"hello"}"#),
            ("input", #"{"type":"input","text":"secret","hideInput":true,"minLength":1,"maxLength":8}"#),
            ("inkey", #"{"type":"inkey","yesNo":true}"#),
            ("confirm", #"{"type":"confirm","command":"send_sms"}"#),
            ("menu", #"{"type":"menu","kind":"select-item","items":[{"id":2,"label":"More"}]}"#)
        ] {
            try sim.consume(JSONDecoder().decode(SIMApplicationMessage.self, from: Data(json.utf8)))
            XCTAssertNotNil(sim.page, type); XCTAssertTrue(sim.presented, type)
        }
        await sim.send(.init(type: "input_response", text: "1234"))
        await sim.send(.init(type: "inkey_response", text: "Y"))
        await sim.send(.init(type: "confirm_response", accepted: false))
        sim.stop(); await task.value
        XCTAssertFalse(sim.available); XCTAssertNil(sim.page); XCTAssertTrue(sim.input.isEmpty)
        let commands = try await server.allMessages().map { try JSONDecoder().decode(DetailCommand.self, from: $0).type }
        XCTAssertTrue(commands.contains("input_response")); XCTAssertTrue(commands.contains("inkey_response")); XCTAssertTrue(commands.contains("confirm_response"))
    }

    func testNotificationsRetainFullSequenceAndScopedPaths() async throws {
        let server = DetailFixtureServer(scenario: .notifications), detail = try await make(server)
        let state = ESIMNotificationsState(session: detail.session, modemID: detail.modemID)
        await state.load()
        let item = try XCTUnwrap(state.groups.first?.notifications.first)
        XCTAssertEqual(item.sequenceNumber, "18446744073709551615")
        await state.perform(item, delete: false)
        XCTAssertEqual(state.groups.flatMap(\.notifications).count, 5)
        await state.perform(item, delete: true)
        let remaining = state.groups.flatMap(\.notifications)
        XCTAssertEqual(remaining.count, 4)
        XCTAssertFalse(remaining.contains { $0.id == item.id })
        XCTAssertTrue(remaining.contains { $0.seId == "se-b" && $0.sequenceNumber == item.sequenceNumber })
        let requests = await server.allRequests()
        XCTAssertTrue(requests.contains { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/ses/se-a/notifications/18446744073709551615/deliveries") == true })
        XCTAssertTrue(requests.contains { $0.httpMethod == "DELETE" && $0.url?.path.hasSuffix("/ses/se-a/notifications/18446744073709551615") == true })
    }

    func testInterruptedNotificationRefreshDoesNotPublishSavedMessage() async throws {
        for cancelling in [false, true] {
            let server = DetailFixtureServer(scenario: .notifications)
            let detail = try await make(server)
            let state = ESIMNotificationsState(session: detail.session, modemID: detail.modemID)
            await state.load()
            let item = try XCTUnwrap(state.groups.first?.notifications.first)
            await server.holdNext("/notifications")
            let operation = Task { await state.perform(item, delete: false) }
            await server.waitForHeldRequest()

            if cancelling { operation.cancel() }
            else { state.clear() }
            await server.release()
            await operation.value

            XCTAssertNil(state.message)
            XCTAssertFalse(state.busy)
            XCTAssertNil(state.failure)
            if !cancelling {
                XCTAssertTrue(state.groups.isEmpty)
                XCTAssertFalse(state.loaded)
            }
        }
    }

    func testUnknownHandshake401PreservesBackendAccess() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let other = try await make(host: "other.example")
        await server.setStatus("/esim-downloads/sessions", 401)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")
        flow.input = LPAInput(smdp: "smdp.example")
        flow.startDownload()
        await flow.run()
        // A handshake status has no verified Problem Details type to revoke login.
        XCTAssertEqual(flow.phase, .failed)
        XCTAssertEqual(flow.failure?.status, 401)
        XCTAssertEqual(flow.failure?.code, "socket_handshake")
        XCTAssertEqual(detail.session.authentication, .notRequired)
        XCTAssertEqual(detail.session.modems.count, 2)
        XCTAssertEqual(other.session.modems.count, 2)
    }

    func testMalformedStreamAndExplicitInterruption() async throws {
        for malformed in [true, false] {
            let server = DetailFixtureServer(), detail = try await make(server)
            let flow = ESIMFlowState(detail: detail, seID: "se-a")
            flow.input = LPAInput(smdp: "smdp.example")
            flow.startDownload()
            let run = Task { await flow.run() }
            try await wait { flow.phase == .preview }
            if malformed { await server.emitRaw("invalid json") }
            else { flow.interrupt(); detail.session.stop() }
            await run.value
            XCTAssertEqual(flow.phase, .failed)
            XCTAssertNotNil(flow.failure)
            let commands = try await server.allMessages().map { try JSONDecoder().decode(DetailCommand.self, from: $0).type }
            XCTAssertEqual(commands.filter { $0 == "start" }.count, 1)
        }
    }

    func testTransferWebsheetCallbackUsesBackendAuthenticationBoundary() async throws {
        let server = DetailFixtureServer(), detail = try await make(server)
        let flow = ESIMFlowState(detail: detail, seID: "se-a")

        await flow.loadSources()
        flow.selectSource(try XCTUnwrap(flow.sources.first))
        await flow.loadProfiles()
        flow.selectProfile(try XCTUnwrap(flow.profiles.first))
        flow.startTransfer()
        try flow.consume(event(#"{"type":"websheet","websheet":{"id":"sheet","embedUrl":"/api/v1/websheets/sheet","url":"https://carrier.example","method":"POST"}}"#))
        XCTAssertEqual(flow.phase, .websheet)
        await flow.completeWebsheet(CarrierCallback(event: "finishFlow", nextAction: "acquireConfiguration"))
        XCTAssertEqual(flow.phase, .progress)
        let requests = await server.allRequests()
        let callback = try XCTUnwrap(requests.first { $0.url?.path.hasSuffix("/websheets/sheet/callback") == true })
        XCTAssertEqual(callback.httpMethod, "POST")
        XCTAssertEqual(callback.url?.path, "/prefix/api/v1/websheets/sheet/callback")
    }

    func testExpiredOrEmptyReminderCannotSubmit() {
        let future = Date().addingTimeInterval(3600)
        XCTAssertFalse(ReminderSubmission(scheduledAt: .distantPast, repeatDays: nil, content: "Renew").isValid())
        XCTAssertFalse(ReminderSubmission(scheduledAt: future, repeatDays: 0, content: "Renew").isValid())
        XCTAssertFalse(ReminderSubmission(scheduledAt: future, repeatDays: 3651, content: "Renew").isValid())
        XCTAssertFalse(ReminderSubmission(scheduledAt: future, repeatDays: nil, content: " ").isValid())
        XCTAssertTrue(ReminderSubmission(scheduledAt: future, repeatDays: 3650, content: "Renew").isValid())
    }

    func testWebsheetAndHTTPPathsPreservePrefixAndRejectOtherOrigins() async throws {
        let config = try testConfiguration()
        let api = BackendAPI(configuration: BackendConfiguration(name: "Proxy", address: try BackendAddress.normalize("https://proxy.example/sigmo")), transport: { _ in .init(data: Data(), status: 204) })
        let request = try api.makeRequest(["modems", "one/two ?", "ses", "a/b"], token: "secret")
        XCTAssertTrue(request.url!.absoluteString.hasSuffix("/modems/one%2Ftwo%20%3F/ses/a%2Fb"))
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer secret")
        let sheet = CarrierWebsheet(id: "sheet", embedUrl: "/api/v1/websheets/sheet", title: nil, url: "https://carrier.example", method: "GET")
        let url = try CarrierWebsheetAddress.url(sheet, api: api, token: "secret")
        XCTAssertEqual(url.path, "/sigmo/api/v1/websheets/sheet")
        XCTAssertEqual(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first?.value, "secret")
        let evil = CarrierWebsheet(id: "sheet", embedUrl: "https://evil.example/api/v1/websheets/sheet", title: nil, url: "https://carrier.example", method: "GET")
        XCTAssertThrowsError(try CarrierWebsheetAddress.url(evil, api: api, token: "secret"))
        XCTAssertNotEqual(config.id, api.backendID)
    }
}

private struct DetailWiFiPayload: Decodable { let enabled: Bool; let underlay: WiFiUnderlay }
private struct DetailStart: Decodable { let seId: String; let activationCode: String? }
private struct DetailCommand: Decodable { let type: String }

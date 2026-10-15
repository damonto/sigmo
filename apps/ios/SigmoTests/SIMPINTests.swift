import XCTest
@testable import Sigmo

@MainActor
final class SIMPINTests: XCTestCase {
    private func make(
        locked: Bool = false,
        legacy: Bool = false,
        unlockFailure: String? = nil,
        statusResponse: String? = nil
    ) async throws -> (SIMPINState, DetailFixtureServer) {
        let server = DetailFixtureServer(physical: true, locked: locked)
        let configuration = BackendConfiguration(name: "PIN", address: try BackendAddress.normalize("https://pin.example/prefix"))
        let api = BackendAPI(configuration: configuration, transport: { request in
            if legacy, request.url?.path.hasSuffix("/sim-pin") == true { return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found") }
            if let unlockFailure, request.url?.path.hasSuffix("/sim-unlocks") == true {
                return HTTPProblem.response(status: 409, code: unlockFailure, detail: "Unlock rejected")
            }
            if let statusResponse, request.url?.path.hasSuffix("/sim-pin") == true {
                return BackendHTTPResponse(data: Data(statusResponse.utf8), status: 200)
            }
            return try await server.respond(request)
        })
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store, api: api)
        await session.refresh(force: true)
        let state = SIMPINState(session: session, modemID: "device-1")
        await state.load()
        return (state, server)
    }
    func testValidationAndUnknownValues() throws {
        for invalid in ["", "123", "123456789", "１２３４", " 1234", "123\n"] { XCTAssertFalse(validSIMPIN(invalid)) }
        XCTAssertTrue(validSIMPIN("0001")); XCTAssertTrue(validSIMPUK("00123456"))
        let unknown = try JSONDecoder().decode(SIMPINStatus.self, from: Data(#"{"supported":true,"enabled":null,"lock":"unknown","pinRetries":null,"pukRetries":null,"simToken":"target"}"#.utf8))
        XCTAssertNil(unknown.enabled); XCTAssertNil(unknown.pinRetries); XCTAssertNil(unknown.pukRetries)
    }
    func testProtectionChangeLeadingZerosAndCleanup() async throws {
        let (state, server) = try await make()
        state.choose(.change); state.pin = "1234"; state.newPIN = "0001"; state.confirmation = "0002"
        XCTAssertFalse(state.canSubmit)
        state.confirmation = "0001"
        await state.submit()
        XCTAssertTrue(state.acknowledged); XCTAssertEqual(state.pin, ""); XCTAssertEqual(state.newPIN, "")
        state.choose(.disable); state.pin = "0001"
        await state.submit()
        XCTAssertEqual(state.status?.enabled, false)
        let writes = await server.allRequests().filter { $0.httpMethod == "POST" || $0.httpMethod == "PUT" }
        XCTAssertEqual(writes.count, 2)
        XCTAssertTrue(String(data: try XCTUnwrap(writes.first?.httpBody), encoding: .utf8)?.contains("0001") == true)
        state.pin = "secret"; state.leave(); XCTAssertEqual(state.pin, "")
    }
    func testPINToPUKAndUnblock() async throws {
        let (state, _) = try await make(locked: true)
        for _ in 0..<3 { state.pin = "0000"; await state.submit() }
        XCTAssertEqual(state.status?.lock, .puk); XCTAssertEqual(state.status?.pinRetries, 0)
        XCTAssertEqual(state.action, .puk); XCTAssertEqual(state.pin, "")
        state.pin = "12345678"; state.newPIN = "0001"; state.confirmation = "0001"
        await state.submit()
        XCTAssertTrue(state.acknowledged); XCTAssertEqual(state.status?.lock, .unlocked)
    }

    func testUnlockSheetCompletesBeforeRefreshingModem() async throws {
        let (state, server) = try await make(locked: true)
        let before = await server.allRequests().count
        state.pin = "1234"
        await state.submit(refreshAfterSuccess: false)
        XCTAssertTrue(state.acknowledged)
        XCTAssertEqual(state.pin, ""); XCTAssertNil(state.action)
        let requests = await server.allRequests()
        XCTAssertEqual(requests.count, before + 1)
        XCTAssertTrue(requests.last?.url?.path.hasSuffix("/sim-unlocks") == true)
        XCTAssertFalse(state.canSubmit)
    }

    func testChangedTargetClosesWithoutCredentialAlert() async throws {
        let (state, _) = try await make(locked: true, unlockFailure: "sim_target_changed")
        state.pin = "1234"
        await state.submit(refreshAfterSuccess: false)
        XCTAssertTrue(state.targetChanged); XCTAssertNil(state.failure)
        XCTAssertEqual(state.pin, ""); XCTAssertFalse(state.canSubmit)
    }
    func testPermanentBlockStopsSubmissions() async throws {
        let (state, server) = try await make(locked: true)
        for _ in 0..<3 { state.pin = "0000"; await state.submit() }
        for _ in 0..<10 { state.pin = "00000000"; state.newPIN = "0001"; state.confirmation = "0001"; await state.submit() }
        XCTAssertEqual(state.status?.lock, .permanentlyBlocked); XCTAssertNil(state.action)
        XCTAssertFalse(state.canSubmit)
        let writes = await server.allRequests().filter { $0.httpMethod == "POST" }
        XCTAssertEqual(writes.count, 13)
    }
    func testOldBackendRetainsPINUnlockOnly() async throws {
        let (state, server) = try await make(locked: true, legacy: true)
        XCTAssertTrue(state.legacy); XCTAssertFalse(state.available); XCTAssertEqual(state.action, .pin)
        state.pin = "1234"; await state.submit()
        XCTAssertTrue(state.acknowledged)
        let writes = await server.allRequests().filter { $0.httpMethod == "POST" }
        XCTAssertEqual(writes.count, 1)
        XCTAssertEqual(try JSONDecoder().decode([String: String].self, from: XCTUnwrap(writes.first?.httpBody)), ["pin": "1234"])
    }
    func testDuplicateSubmissionAndLateReadAfterLeaving() async throws {
        let (state, server) = try await make()
        state.choose(.disable); state.pin = "1234"
        await server.holdNext("/sim-pin/protection")
        let write = Task { await state.submit() }
        await server.waitForHeldRequest()
        await state.submit()
        let pending = await server.allRequests().filter { $0.httpMethod == "PUT" }
        XCTAssertEqual(pending.count, 1)
        await server.release(); await write.value
        XCTAssertEqual(state.pin, ""); XCTAssertEqual(state.status?.enabled, false)

        await server.holdNext("/sim-pin")
        let read = Task { await state.load() }
        await server.waitForHeldRequest()
        state.reset()
        await server.release(); await read.value
        XCTAssertNil(state.status); XCTAssertEqual(state.pin, "")
    }

    func testAcknowledgementSurvivesStatusFailure() async throws {
        let (state, server) = try await make()
        state.choose(.disable); state.pin = "1234"
        await server.setStatus("/sim-pin", 503)
        await state.submit()
        XCTAssertTrue(state.acknowledged); XCTAssertNil(state.status); XCTAssertNil(state.action)
        XCTAssertEqual(state.errorCode, "sim_pin_unavailable")
        XCTAssertFalse(state.canSubmit); XCTAssertTrue(state.available)
        await server.setStatus("/sim-pin", 200)
        await state.load()
        XCTAssertNil(state.errorCode); XCTAssertEqual(state.status?.enabled, false)
        let writes = await server.allRequests().filter { $0.httpMethod == "PUT" }
        XCTAssertEqual(writes.count, 1)
    }

    func testUnknownPUKAttemptsDoNotUsePINAttempts() async throws {
        let (state, _) = try await make(
            locked: true,
            statusResponse: #"{"supported":true,"enabled":true,"lock":"puk","pinRetries":0,"pukRetries":null,"simToken":"target"}"#
        )
        XCTAssertEqual(state.action, .puk)
        XCTAssertNil(state.attemptMessage)
        state.pin = "12345678"
        state.newPIN = "0001"
        state.confirmation = "0001"
        XCTAssertTrue(state.canSubmit)
    }

    func testUnsupportedAndFutureLocksDoNotOfferCredentials() async throws {
        let responses = [
            #"{"supported":false,"enabled":null,"lock":"pin","pinRetries":null,"pukRetries":null,"simToken":"target"}"#,
            #"{"supported":true,"enabled":null,"lock":"future_lock","pinRetries":null,"pukRetries":null,"simToken":"target"}"#,
        ]
        for response in responses {
            let (state, _) = try await make(locked: true, statusResponse: response)
            XCTAssertNil(state.action)
            XCTAssertFalse(state.canSubmit)
            XCTAssertFalse(state.statusMessage.isEmpty)
        }
        let unknown = try JSONDecoder().decode(SIMPINStatus.self, from: Data(responses[1].utf8))
        XCTAssertEqual(unknown.lock, .unknown)
    }

    func testIncorrectPINRetainsActionAndRefreshesAttempts() async throws {
        let (state, server) = try await make()
        state.choose(.disable)
        state.pin = "0000"
        await state.submit()
        XCTAssertEqual(state.action, .disable)
        XCTAssertEqual(state.errorCode, "sim_pin_incorrect")
        XCTAssertEqual(state.status?.pinRetries, 2)
        XCTAssertEqual(state.pin, "")
        XCTAssertFalse(state.canSubmit)
        await state.submit()
        let writes = await server.allRequests().filter { $0.httpMethod == "PUT" }
        XCTAssertEqual(writes.count, 1)
    }

}

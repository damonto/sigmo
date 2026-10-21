import CallKit
import XCTest
@testable import Sigmo

@MainActor
final class CallDiagnosticsTests: XCTestCase {
    func testSnapshotsKeepStateAndDatesWithoutDisclosingBackendTextOrIdentities() throws {
        var record = CallsFixture.record("call-id-secret", state: "ended", direction: "incoming", owner: "client-secret")
        record.number = "phone-number-secret"
        record.profileId = "profile-secret"
        let fields = CallDiagnostics.snapshot(record, clientID: "this-client")
        XCTAssertEqual(fields["state"], "ended")
        XCTAssertEqual(fields["owner"], "other")
        XCTAssertEqual(fields["backendReason"], "normal")
        XCTAssertEqual(fields["recordCallUUID"], record.callUUID)
        XCTAssertNotNil(fields["endedAt"])

        record.state = "state-secret"
        record.direction = "direction-secret"
        record.route = "route-secret"
        record.hold = "hold-secret"
        record.reason = "reason-secret https://credential-secret@example.test"
        record.startedAt = "date-secret"
        record.callUUID = "uuid-secret"
        let redacted = CallDiagnostics.snapshot(record, clientID: "this-client")
        for key in ["state", "direction", "route", "hold", "backendReason"] { XCTAssertEqual(redacted[key], "other") }
        XCTAssertNil(redacted["startedAt"])
        XCTAssertNil(redacted["recordCallUUID"])
        let text = String(decoding: try JSONEncoder().encode([fields, redacted]), as: UTF8.self)
        XCTAssertFalse(text.contains("secret"))
        XCTAssertEqual(CallDiagnostics.snapshot(record, clientID: "client-secret")["owner"], "self")
        record.ownerClientId = nil
        XCTAssertEqual(CallDiagnostics.snapshot(record, clientID: "this-client")["owner"], "none")
    }

    func testPushValidationIdentifiesAllFailedChecks() throws {
        let id = UUID()
        let now = Date(timeIntervalSince1970: 1000)
        let connection = BackendConfiguration(name: "Home", address: try XCTUnwrap(URL(string: "https://fixture.test")))
        let valid = PushContent(kind: "call", backendId: "backend", backendName: "Home", modemId: "modem", profileId: "profile",
                                callId: "call", callUUID: id.uuidString, expiresAt: 1045)
        XCTAssertTrue(CallDiagnostics.validationIssues(content: valid, id: id, connection: connection, ended: false, now: now).isEmpty)
        XCTAssertEqual(CallDiagnostics.validationIssues(content: nil, id: id, connection: nil, ended: false, now: now), ["missing_content"])
        let invalid = PushContent(kind: "sms", backendId: "backend", backendName: "Home", modemId: "", profileId: "",
                                  callId: "", callUUID: UUID().uuidString, expiresAt: 1000)
        XCTAssertEqual(Set(CallDiagnostics.validationIssues(content: invalid, id: id, connection: nil, ended: true, now: now)),
                       ["uuid_mismatch", "wrong_kind", "already_ended", "expired", "missing_connection", "missing_call_id", "missing_modem_id", "missing_profile_id"])
    }

    func testCallKitLogsCorrelateActionErrorsAndTimeoutsWithoutDigitsOrErrorDetails() async throws {
        let directory = URL.temporaryDirectory.appending(path: "CallDiagnostics-\(UUID())")
        let store = DiagnosticLogStore(directory: directory, defaults: nil)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        await store.setEnabled(true)
        let bridge = CallKitBridge(enabled: false, diagnostics: store)
        bridge.perform = { _ in
            throw NSError(domain: CXErrorDomainRequestTransaction, code: 4, userInfo: [NSLocalizedDescriptionKey: "error-description-secret"])
        }
        let id = UUID()
        let action = CXPlayDTMFCallAction(call: id, digits: "1234567890#", type: .singleTone)
        await bridge.execute(action, .digits(id, "1234567890#")).value
        let gate = SettingsRequestGate()
        bridge.perform = { _ in await gate.wait() }
        let answer = CXAnswerCallAction(call: id)
        let pending = bridge.execute(answer, .answer(id))
        await gate.waitForCaller()
        bridge.timedOut(answer)
        await gate.release()
        await pending.value
        bridge.ended(id, reason: .remoteEnded)

        let data = try await store.snapshot()
        let events = try diagnosticEvents(data)
        XCTAssertTrue(events.allSatisfy { $0.fields["callUUID"] == id.uuidString })
        let error = try XCTUnwrap(events.first { $0.event == "callkit.action.error" })
        XCTAssertEqual(error.fields["errorDomain"], CXErrorDomainRequestTransaction)
        XCTAssertEqual(error.fields["errorCode"], "4")
        XCTAssertEqual(error.fields["actionID"], action.uuid.uuidString)
        XCTAssertEqual(events.first { $0.event == "callkit.action.completed" }?.fields["completion"], "failed")
        XCTAssertEqual(events.first { $0.event == "callkit.action.timed_out" }?.fields["actionID"], answer.uuid.uuidString)
        XCTAssertEqual(events.last?.fields["endReason"], "remote_ended")
        let text = String(decoding: data, as: UTF8.self)
        XCTAssertFalse(text.contains("1234567890#"))
        XCTAssertFalse(text.contains("error-description-secret"))
    }

    func testPushDecodeFailuresUseFixedErrorCodes() {
        XCTAssertEqual(DiagnosticError.fields(PushPayloadError.missingKey)["errorCode"], "missing_push_key")
        XCTAssertEqual(DiagnosticError.fields(PushPayloadError.invalidEnvelope)["errorCode"], "invalid_push_envelope")
    }
}

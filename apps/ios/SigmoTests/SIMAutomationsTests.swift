import XCTest
@testable import Sigmo

@MainActor
final class SIMAutomationsTests: XCTestCase {
    func testScheduleAndActionValidation() throws {
        var draft = AutomationDraft()
        draft.kind = .data
        draft.amount = "100"
        XCTAssertEqual(try XCTUnwrap(draft.schedule).action.bytes, 104_857_600)
        draft.amount = "101"
        XCTAssertNil(draft.schedule)
        draft.kind = .sms
        draft.to = "+12025550123"
        draft.text = " example "
        XCTAssertEqual(try XCTUnwrap(draft.schedule).action.text, " example ")
        draft.kind = .call
        draft.repeatDays = "7"
        draft.duration = "300"
        let schedule = try XCTUnwrap(draft.schedule)
        XCTAssertEqual(schedule.repeatDays, 7)
        XCTAssertEqual(schedule.action.durationSeconds, 300)
        XCTAssertNil(schedule.action.text)
        draft.duration = "301"
        XCTAssertNil(draft.schedule)
    }

    func testLineScopedAutomationRequestAndRestorationResult() async throws {
        let address = try BackendAddress.normalize("https://automation.example/prefix")
        let config = BackendConfiguration(name: "Automations", address: address)
        let api = BackendAPI(configuration: config, transport: { request in
            let collection = "/prefix/api/v1/sim-profiles/8986000000000000001/automations"
            switch request.httpMethod {
            case "GET":
                if request.url?.path == "/prefix/api/v1/modems/modem/automations" {
                    return .init(data: Data(#"{"items":[{"profileId":"8986000000000000001","seId":"se-a","actionType":"data","nextAt":"2026-11-21T12:00:00Z"},{"profileId":"8986000000000000002","seId":"se-a","actionType":"sms","nextAt":null}]}"#.utf8), status: 200)
                }
                XCTAssertEqual(request.url?.path, collection)
                return .init(data: Data(#"{"items":[{"id":"other","modemId":"different","seId":"se-a","nextAt":null,"repeatDays":null,"action":{"type":"data","bytes":2048},"running":false,"restoring":false},{"id":"task","modemId":"modem","seId":"se-a","nextAt":null,"repeatDays":null,"action":{"type":"data","bytes":1024},"running":false,"restoring":true,"lastRun":{"finishedAt":"2026-11-20T12:00:00.123Z","status":"succeeded","restoreCode":"restore_failed","bytesReceived":1024}}]}"#.utf8), status: 200)
            case "POST":
                XCTAssertEqual(request.url?.path, collection)
                let payload = try XCTUnwrap(JSONSerialization.jsonObject(with: request.httpBody ?? Data()) as? [String: Any])
                XCTAssertEqual(payload["modemId"] as? String, "modem")
                XCTAssertEqual(payload["seId"] as? String, "se-a")
                XCTAssertEqual(payload["repeatDays"] as? Int, 2)
                XCTAssertEqual((payload["action"] as? [String: Any])?["durationSeconds"] as? Int, 15)
                return .init(data: Data(), status: 201)
            case "PUT":
                XCTAssertEqual(request.url?.path, collection + "/task")
                let payload = try XCTUnwrap(JSONSerialization.jsonObject(with: request.httpBody ?? Data()) as? [String: Any])
                XCTAssertNil(payload["modemId"])
                XCTAssertEqual((payload["action"] as? [String: Any])?["durationSeconds"] as? Int, 15)
                return .init(data: Data(), status: 200)
            case "DELETE":
                XCTAssertEqual(request.url?.path, collection + "/task")
                return .init(data: Data(), status: 204)
            default:
                XCTFail("Unexpected request")
                return .init(data: Data(), status: 500)
            }
        })
        let line = SIMAutomationLine(profileID: "8986000000000000001", modemID: "modem", seID: "se-a", label: "Travel")
        let schedule = SIMAutomationSchedule(scheduledAt: Date().addingTimeInterval(3600), repeatDays: 2,
                                             action: SIMAutomationAction(type: .call, to: "+12025550123", durationSeconds: 15))
        try await api.saveAutomation(line: line, id: nil, schedule: schedule, token: nil)
        let loaded = try await api.automation(line: line, token: nil)
        let task = try XCTUnwrap(loaded)
        XCTAssertEqual(task.id, "task")
        XCTAssertEqual(AutomationDraft(task: task).amount, "1")
        try await api.saveAutomation(line: line, id: task.id, schedule: schedule, token: nil)
        let summaries = try await api.automationSummaries(modemID: line.modemID, token: nil)
        XCTAssertEqual(summaries.count, 2)
        XCTAssertEqual(summaries.first?.profileId, line.profileID)
        XCTAssertEqual(summaries.first?.actionType, .data)
        XCTAssertNotNil(summaries.first?.nextAt)
        XCTAssertEqual(summaries.last?.actionType, .sms)
        XCTAssertNil(summaries.last?.nextAt)
        XCTAssertTrue(task.restoring)
        XCTAssertEqual(task.lastRun?.restoreCode, "restore_failed")
        XCTAssertEqual(task.lastRun?.bytesReceived, 1024)
        try await api.deleteAutomation(profileID: line.profileID, id: task.id, token: nil)
    }
}

import Foundation

struct SIMAutomationLine: Sendable {
    let profileID: String
    let modemID: String
    var seID: String?
    let label: String

    static func key(profileID: String, seID: String?) -> String {
        "\(seID ?? ""):\(profileID)"
    }
}

enum SIMAutomationKind: String, Codable, CaseIterable, Identifiable, Sendable {
    case data, sms, call

    var id: Self { self }
    var label: String { "automation." + rawValue }
}

struct SIMAutomationAction: Codable, Sendable {
    let type: SIMAutomationKind
    var bytes: Int?
    var to: String?
    var text: String?
    var durationSeconds: Int?
}

struct SIMAutomationSchedule: Encodable, Sendable {
    let scheduledAt: Date
    let repeatDays: Int?
    let action: SIMAutomationAction
}

struct SIMAutomationTask: Decodable, Identifiable, Sendable {
    struct Run: Decodable, Sendable {
        let finishedAt: Date
        let status: String
        let code: String?
        let restoreCode: String?
        let bytesReceived: Int?
    }

    let id: String
    let modemId: String
    let seId: String?
    let repeatDays: Int?
    let action: SIMAutomationAction
    let nextAt: Date?
    let running: Bool
    let restoring: Bool
    let lastRun: Run?
}

struct SIMAutomationSummary: Decodable, Sendable {
    let profileId: String
    let seId: String?
    let actionType: SIMAutomationKind
    let nextAt: Date?
}

extension BackendAPI {
    func automation(line: SIMAutomationLine, token: String?) async throws -> SIMAutomationTask? {
        struct Reply: Decodable, Sendable { let items: [SIMAutomationTask] }
        let reply: Reply = try await decode(automationPath(line.profileID), token: token)
        return reply.items.first { $0.modemId == line.modemID && ($0.seId ?? "") == (line.seID ?? "") }
    }

    func automationSummaries(modemID: String, token: String?) async throws -> [SIMAutomationSummary] {
        struct Reply: Decodable, Sendable { let items: [SIMAutomationSummary] }
        let reply: Reply = try await decode(["modems", modemID, "automations"], token: token)
        return reply.items
    }

    func saveAutomation(line: SIMAutomationLine, id: String?, schedule: SIMAutomationSchedule, token: String?) async throws {
        struct Create: Encodable {
            let modemId: String
            let seId: String?
            let scheduledAt: Date
            let repeatDays: Int?
            let action: SIMAutomationAction
        }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let path = automationPath(line.profileID)
        if let id {
            _ = try await request(path + [id], method: "PUT", body: encoder.encode(schedule), token: token)
        } else {
            let value = Create(modemId: line.modemID, seId: line.seID, scheduledAt: schedule.scheduledAt,
                               repeatDays: schedule.repeatDays, action: schedule.action)
            _ = try await request(path, method: "POST", body: encoder.encode(value), token: token)
        }
    }

    func deleteAutomation(profileID: String, id: String, token: String?) async throws {
        _ = try await request(automationPath(profileID) + [id], method: "DELETE", token: token)
    }

    private func automationPath(_ profileID: String) -> [String] {
        ["sim-profiles", profileID, "automations"]
    }
}

import Foundation

extension BackendAPI {
    func contacts(token: String?) async throws -> [SigmoContact] {
        let result: CommunicationItems<SigmoContact> = try await decode(["contacts"], token: token)
        return result.items
    }
    func saveContact(_ contact: SigmoContact, token: String?) async throws -> SigmoContact {
        try await decode(contact.id.isEmpty ? ["contacts"] : ["contacts", contact.id], method: contact.id.isEmpty ? "POST" : "PUT", body: JSONEncoder().encode(contact), token: token)
    }
    func deleteContact(_ id: String, token: String?) async throws {
        _ = try await request(["contacts", id], method: "DELETE", token: token)
    }
    func contactAvatar(_ id: String, token: String?) async throws -> Data {
        try await request(["contacts", id, "avatar"], method: "GET", token: token, accept: "image/png").data
    }
    func saveContactAvatar(_ id: String, data: Data?, token: String?) async throws -> SigmoContact {
        let response = try await request(["contacts", id, "avatar"], method: data == nil ? "DELETE" : "PUT", body: data, token: token, contentType: "image/png")
        return try JSONDecoder().decode(SigmoContact.self, from: response.data)
    }
    func communicationLines(token: String?, cached: Bool = false) async throws -> [CommunicationLine] {
        let result: CommunicationItems<CommunicationLine> = try await decode(["sim-profiles"], token: token, query: cached ? [URLQueryItem(name: "cached", value: "true")] : [])
        return result.items
    }
    func communicationPreference(_ participant: String, token: String?) async throws -> CommunicationPreference {
        try await decode(["communication-preferences"], token: token, query: [.init(name: "participant", value: participant)])
    }
    func assignHistory(source: String, target: String, token: String?) async throws {
        struct Body: Encodable { let targetProfileId: String }
        _ = try await request(["sim-profiles", source, "history-assignment"], method: "PUT", body: JSONEncoder().encode(Body(targetProfileId: target)), token: token)
    }
    func messageThreads(search: String, cursor: String, profile: String = "", token: String?) async throws -> CommunicationPage<CommunicationThread> {
        try await decode(["message-threads"], token: token, query: [.init(name: "q", value: search), .init(name: "cursor", value: cursor), .init(name: "profileId", value: profile)])
    }
    func threadMessages(_ id: String, cursor: String, token: String?) async throws -> CommunicationPage<CommunicationMessage> {
        try await decode(["message-threads", id, "messages"], token: token, query: [.init(name: "cursor", value: cursor)])
    }
    func readThread(_ id: String, messageIDs: [Int64], token: String?) async throws -> MessageBadge {
        struct Body: Encodable { let messageIds: [Int64] }
        return try await decode(["message-threads", id], method: "PATCH", body: JSONEncoder().encode(Body(messageIds: messageIDs)), token: token)
    }
    func deleteThread(_ id: String, token: String?) async throws {
        _ = try await request(["message-threads", id], method: "DELETE", token: token)
    }
    func callHistory(search: String, cursor: String, profile: String = "", missed: Bool = false, modemID: String = "", token: String?) async throws -> CommunicationPage<CallRecord> {
        try await decode(["calls"], token: token, query: [.init(name: "q", value: search), .init(name: "cursor", value: cursor), .init(name: "profileId", value: profile), .init(name: "missed", value: String(missed)), .init(name: "modemId", value: modemID)])
    }
    func deleteCallHistory(_ id: String, token: String?) async throws { _ = try await request(["calls", id], method: "DELETE", token: token) }
}

import Foundation

extension BackendAPI {
    func conversations(modemID: String?, search: String = "", unassigned: Bool = false, token: String?) async throws -> MessageSnapshot {
        var query: [URLQueryItem] = []
        if let modemID { query.append(URLQueryItem(name: "modemId", value: modemID)) }
        if !search.isEmpty { query.append(URLQueryItem(name: "q", value: search)) }
        if unassigned { query.append(URLQueryItem(name: "unassigned", value: "true")) }
        return try await decode(["conversations"], token: token, query: query)
    }

    func conversationMessages(_ id: String, token: String?) async throws -> [SMSMessage] {
        try await decode(["conversations", id, "messages"], token: token)
    }

    func updateConversation(_ id: String, read: ConversationRead, token: String?) async throws -> MessageBadge {
        try await decode(["conversations", id], method: "PATCH", body: JSONEncoder().encode(read), token: token)
    }

    func deleteConversation(_ id: String, token: String?) async throws {
        _ = try await request(["conversations", id], method: "DELETE", token: token)
    }

    func sendMessage(modemID: String, submission: SMSSubmission, token: String?) async throws -> SMSSubmitted {
        try await decode(["modems", modemID, "messages"], method: "POST", body: JSONEncoder().encode(submission), token: token)
    }

    func legacyMessages(modemID: String, participant: String? = nil, search: String = "", token: String?) async throws -> [SMSMessage] {
        let path = ["modems", modemID, "messages"] + (participant.map { [$0] } ?? [])
        let query = search.isEmpty ? [] : [URLQueryItem(name: "q", value: search)]
        return try await decode(path, token: token, query: query)
    }

    func deleteLegacyMessages(modemID: String, participant: String, token: String?) async throws {
        _ = try await request(["modems", modemID, "messages", participant], method: "DELETE", token: token)
    }
}

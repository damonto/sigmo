import Foundation

extension BackendAPI {
    func calls(modemID: String, search: String = "", token: String?) async throws -> [CallRecord] {
        try await decode(["modems", modemID, "calls"], token: token,
            query: search.isEmpty ? [] : [URLQueryItem(name: "q", value: search)])
    }

    func call(modemID: String, id: String, token: String?) async throws -> CallRecord {
        try await decode(["modems", modemID, "calls", id], token: token)
    }

    func dial(modemID: String, submission: CallDial, token: String?) async throws -> CallRecord {
        try await decode(["modems", modemID, "calls"], method: "POST", body: JSONEncoder().encode(submission), token: token)
    }

    func updateCall(modemID: String, id: String, update: CallUpdate, token: String?) async throws -> CallRecord {
        try await decode(["modems", modemID, "calls", id], method: "PATCH", body: JSONEncoder().encode(update), token: token)
    }

    func deleteCall(modemID: String, id: String, token: String?) async throws {
        _ = try await request(["modems", modemID, "calls", id], method: "DELETE", token: token)
    }

    func callDTMF(modemID: String, id: String, clientID: String, digits: String, token: String?) async throws {
        struct Body: Encodable { let clientId, digits: String }
        _ = try await request(["modems", modemID, "calls", id, "dtmf-events"], method: "POST",
            body: JSONEncoder().encode(Body(clientId: clientID, digits: digits)), token: token)
    }

    func callICE(token: String?) async throws -> [CallICE] {
        struct Response: Decodable, Sendable { let iceServers: [CallICE] }
        let value: Response = try await decode(["call-media", "ice-servers"], token: token)
        return value.iceServers
    }

    func ussd(modemID: String, action: String, code: String, token: String?) async throws -> String {
        struct Body: Encodable { let action, code: String }
        struct Reply: Decodable, Sendable { let reply: String }
        let value: Reply = try await decode(["modems", modemID, "ussd"], method: "POST",
            body: JSONEncoder().encode(Body(action: action, code: code)), token: token, timeout: .hardware)
        return value.reply
    }
}

import Foundation

extension BackendAPI {
    func simPINStatus(_ modem: String, token: String?) async throws -> SIMPINStatus {
        try await decode(["modems", modem, "sim-pin"], token: token)
    }

    // One request per credential submission; callers refresh status after errors.
    func submitSIMPIN(
        _ modem: String,
        action: SIMPINAction,
        pin: String,
        newPIN: String,
        simToken: String?,
        token: String?
    ) async throws {
        struct Protection: Encodable {
            let enabled: Bool
            let pin: String
            let simToken: String?
        }
        struct Change: Encodable {
            let oldPin: String
            let newPin: String
            let simToken: String?
        }
        struct Unlock: Encodable {
            let pin: String?
            let puk: String?
            let newPin: String?
            let simToken: String?
        }
        let path: [String]
        let method: String
        let body: Data
        switch action {
        case .enable, .disable:
            path = ["modems", modem, "sim-pin", "protection"]
            method = "PUT"
            body = try JSONEncoder().encode(Protection(enabled: action == .enable, pin: pin, simToken: simToken))
        case .change:
            path = ["modems", modem, "sim-pin-changes"]
            method = "POST"
            body = try JSONEncoder().encode(Change(oldPin: pin, newPin: newPIN, simToken: simToken))
        case .pin, .puk:
            path = ["modems", modem, "sim-unlocks"]
            method = "POST"
            body = try JSONEncoder().encode(Unlock(
                pin: action == .pin ? pin : nil,
                puk: action == .puk ? pin : nil,
                newPin: action == .puk ? newPIN : nil,
                simToken: simToken
            ))
        }
        _ = try await request(path, method: method, body: body, token: token)
    }
}

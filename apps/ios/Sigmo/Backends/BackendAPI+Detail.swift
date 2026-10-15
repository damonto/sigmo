import Foundation

extension BackendAPI {
    func secureElements(_ modem: String, token: String?) async throws -> SecureElements {
        try await decode(["modems", modem, "ses"], token: token, timeout: .hardware)
    }
    func profiles(_ modem: String, token: String?) async throws -> ESIMProfiles {
        try await decode(["modems", modem, "esims"], token: token, timeout: .hardware)
    }
    func detailMutation(_ modem: String, path: [String], method: String, token: String?) async throws {
        _ = try await request(["modems", modem] + path, method: method, token: token, timeout: .hardware)
    }
    func detailMutation<Body: Encodable & Sendable>(_ modem: String, path: [String], method: String,
                                                   value: Body, token: String?) async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        _ = try await request(["modems", modem] + path, method: method, body: encoder.encode(value), token: token, timeout: .hardware)
    }
    func discoverProfiles(_ modem: String, seID: String, token: String?) async throws -> [ESIMDiscovery] {
        try await decode(["modems", modem, "ses", seID, "esim-discoveries"], method: "POST", token: token, timeout: .hardware)
    }
    func transferSources(_ modem: String, token: String?) async throws -> TransferSources {
        try await decode(["modems", modem, "esim-transfers", "sources"], token: token, timeout: .hardware)
    }
    func transferProfiles(_ modem: String, source: TransferSource, imei: String, token: String?) async throws -> [TransferProfile] {
        struct Submission: Encodable { let sourceType: String; let sourceId: String; let sourceImei: String }
        return try await decode(["modems", modem, "esim-transfers", "source-profiles"], method: "POST",
                                body: JSONEncoder().encode(Submission(sourceType: source.type, sourceId: source.id, sourceImei: imei)), token: token, timeout: .hardware)
    }
    func esimNotifications(_ modem: String, token: String?) async throws -> ESIMNotifications {
        try await decode(["modems", modem, "notifications"], token: token, timeout: .hardware)
    }
    func detailWiFi(_ modem: String, token: String?) async throws -> DetailWiFiSettings {
        try await decode(["modems", modem, "wifi-calling", "settings"], token: token, timeout: .hardware)
    }
    func carrierCallback(_ id: String, callback: CarrierCallback, token: String?) async throws {
        _ = try await request(["websheets", id, "callback"], method: "POST", body: JSONEncoder().encode(callback), token: token, timeout: .hardware)
    }
}

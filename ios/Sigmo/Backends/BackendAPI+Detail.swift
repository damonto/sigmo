import Foundation

extension BackendAPI {
    func secureElements(_ modem: String, token: String?) async throws -> SecureElements {
        try await decode(["modems", modem, "ses"], token: token)
    }
    func profiles(_ modem: String, token: String?) async throws -> ESIMProfiles {
        try await decode(["modems", modem, "esims"], token: token)
    }
    func detailMutation(_ modem: String, path: [String], method: String, token: String?) async throws {
        _ = try await request(["modems", modem] + path, method: method, token: token)
    }
    func detailMutation<Body: Encodable & Sendable>(_ modem: String, path: [String], method: String,
                                                   value: Body, token: String?) async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        _ = try await request(["modems", modem] + path, method: method, body: encoder.encode(value), token: token)
    }
    func discoverProfiles(_ modem: String, seID: String, token: String?) async throws -> [ESIMDiscovery] {
        try await decode(["modems", modem, "ses", seID, "esim-discoveries"], method: "POST", token: token)
    }
    func transferSources(_ modem: String, token: String?) async throws -> TransferSources {
        try await decode(["modems", modem, "esim-transfers", "sources"], token: token)
    }
    func transferProfiles(_ modem: String, source: TransferSource, imei: String, token: String?) async throws -> [TransferProfile] {
        struct Submission: Encodable { let sourceType: String; let sourceId: String; let sourceImei: String }
        return try await decode(["modems", modem, "esim-transfers", "source-profiles"], method: "POST",
                                body: JSONEncoder().encode(Submission(sourceType: source.type, sourceId: source.id, sourceImei: imei)), token: token)
    }
    func esimNotifications(_ modem: String, token: String?) async throws -> ESIMNotifications {
        try await decode(["modems", modem, "notifications"], token: token)
    }
    func detailInternet(_ modem: String, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections", "current"], token: token)
    }
    func detailWiFi(_ modem: String, token: String?) async throws -> DetailWiFiSettings {
        try await decode(["modems", modem, "wifi-calling", "settings"], token: token)
    }
    func carrierCallback(_ id: String, callback: CarrierCallback, token: String?) async throws {
        _ = try await request(["websheets", id, "callback"], method: "POST", body: JSONEncoder().encode(callback), token: token)
    }
}

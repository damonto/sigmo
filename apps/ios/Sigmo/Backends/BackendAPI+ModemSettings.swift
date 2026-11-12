import Foundation

extension BackendAPI {
    func modemAirplaneMode(_ modem: String, token: String?) async throws -> ModemAirplaneMode {
        try await decode(["modems", modem, "network-settings", "airplane-mode"], token: token, timeout: .hardware)
    }

    func setModemAirplaneMode(_ modem: String, enabled: Bool, token: String?) async throws {
        struct Submission: Encodable { let enabled: Bool }
        _ = try await request(["modems", modem, "network-settings", "airplane-mode"], method: "PUT",
                              body: JSONEncoder().encode(Submission(enabled: enabled)), token: token, timeout: .hardware)
    }

    func modemModes(_ modem: String, token: String?) async throws -> ModemNetworkModes {
        try await decode(["modems", modem, "network-settings", "modes"], token: token, timeout: .hardware)
    }

    func setModemMode(_ modem: String, mode: ModemModeSelection, token: String?) async throws {
        _ = try await request(["modems", modem, "network-settings", "modes"], method: "PUT",
                              body: JSONEncoder().encode(["current": mode]), token: token, timeout: .hardware)
    }

    func modemBands(_ modem: String, token: String?) async throws -> ModemNetworkBands {
        try await decode(["modems", modem, "network-settings", "bands"], token: token, timeout: .hardware)
    }

    func setModemBands(_ modem: String, bands: [ModemBandValue], token: String?) async throws {
        struct Submission: Encodable { let current: [ModemBandValue] }
        _ = try await request(["modems", modem, "network-settings", "bands"], method: "PUT",
                              body: JSONEncoder().encode(Submission(current: bands)), token: token, timeout: .hardware)
    }

    func modemDeviceSettings(_ modem: String, token: String?) async throws -> ModemDeviceSettings {
        try await decode(["modems", modem, "settings"], token: token, timeout: .hardware)
    }

    func setModemDeviceSettings(_ modem: String, settings: ModemDeviceSettings, token: String?) async throws {
        _ = try await request(["modems", modem, "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token, timeout: .hardware)
    }
}

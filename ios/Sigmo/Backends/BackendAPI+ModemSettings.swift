import Foundation

extension BackendAPI {
    func modemAirplaneMode(_ modem: String, token: String?) async throws -> ModemAirplaneMode {
        try await decode(["modems", modem, "networks", "airplane-mode"], token: token)
    }

    func setModemAirplaneMode(_ modem: String, enabled: Bool, token: String?) async throws {
        struct Submission: Encodable { let enabled: Bool }
        _ = try await request(["modems", modem, "networks", "airplane-mode"], method: "PUT",
                              body: JSONEncoder().encode(Submission(enabled: enabled)), token: token)
    }

    func modemModes(_ modem: String, token: String?) async throws -> ModemNetworkModes {
        try await decode(["modems", modem, "networks", "modes"], token: token)
    }

    func setModemMode(_ modem: String, mode: ModemModeSelection, token: String?) async throws {
        _ = try await request(["modems", modem, "networks", "current-modes"], method: "PUT",
                              body: JSONEncoder().encode(mode), token: token)
    }

    func modemBands(_ modem: String, token: String?) async throws -> ModemNetworkBands {
        try await decode(["modems", modem, "networks", "bands"], token: token)
    }

    func setModemBands(_ modem: String, bands: [ModemBandValue], token: String?) async throws {
        struct Submission: Encodable { let bands: [ModemBandValue] }
        _ = try await request(["modems", modem, "networks", "current-bands"], method: "PUT",
                              body: JSONEncoder().encode(Submission(bands: bands)), token: token)
    }

    func modemDeviceSettings(_ modem: String, token: String?) async throws -> ModemDeviceSettings {
        try await decode(["modems", modem, "settings"], token: token)
    }

    func setModemDeviceSettings(_ modem: String, settings: ModemDeviceSettings, token: String?) async throws {
        _ = try await request(["modems", modem, "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token)
    }
}

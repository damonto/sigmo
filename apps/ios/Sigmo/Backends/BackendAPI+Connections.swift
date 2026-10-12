import Foundation

extension BackendAPI {
    func networkRegistration(_ modem: String, token: String?) async throws -> NetworkRegistration {
        try await decode(["modems", modem, "networks", "registration"], token: token)
    }

    func setNetworkRegistration(_ modem: String, value: NetworkRegistration, token: String?) async throws {
        _ = try await request(["modems", modem, "networks", "registration"], method: "PUT",
                              body: JSONEncoder().encode(value), token: token, timeout: .connection)
    }

    func startNetworkScan(_ modem: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans"], method: "POST", token: token)
    }

    func networkScan(_ modem: String, id: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans", id], token: token)
    }

    func connectInternet(_ modem: String, preferences: InternetPreferences, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections"], method: "POST",
                         body: JSONEncoder().encode(preferences.submission), token: token, timeout: .connection)
    }

    func disconnectInternet(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "internet-connections", "current"], method: "DELETE",
                              token: token, timeout: .connection)
    }

    func internetOptions(_ modem: String, value: InternetConnectionOptions, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections", "current", "preferences"], method: "PUT",
                         body: JSONEncoder().encode(value), token: token)
    }

    func internetPublicInfo(_ modem: String, token: String?) async throws -> InternetPublicInfo {
        try await decode(["modems", modem, "internet-connections", "public"], token: token)
    }

    func updateWiFiCalling(_ modem: String, settings: WiFiSubmission, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token)
    }

    func reconnectWiFiCalling(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "sessions"], method: "POST", token: token)
    }

    func disconnectWiFiCalling(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "sessions", "current"], method: "DELETE", token: token)
    }

    func wifiCallingWebsheet(_ modem: String, emergencyAddress: Bool, token: String?) async throws -> CarrierWebsheet {
        try await decode(["modems", modem, "wifi-calling", emergencyAddress ? "emergency-address-websheets" : "websheets"],
                         method: "POST", token: token, timeout: .connection)
    }

    func volteSettings(_ modem: String, token: String?) async throws -> VoLTESettings {
        try await decode(["modems", modem, "volte", "settings"], token: token)
    }

    func updateVoLTE(_ modem: String, settings: VoLTESubmission, token: String?) async throws {
        _ = try await request(["modems", modem, "volte", "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token, timeout: .connection)
    }
}

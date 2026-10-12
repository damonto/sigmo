import Foundation

extension BackendAPI {
    func networkRegistration(_ modem: String, token: String?) async throws -> NetworkRegistration {
        try await decode(["modems", modem, "networks", "registration"], token: token)
    }

    func setNetworkRegistration(_ modem: String, value: NetworkRegistration, token: String?) async throws {
        _ = try await request(["modems", modem, "networks", "registration"], method: "PUT",
                              body: JSONEncoder().encode(value), token: token)
    }

    func startNetworkScan(_ modem: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans"], method: "POST", token: token)
    }

    func networkScan(_ modem: String, id: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans", id], token: token)
    }

    func detailInternet(_ modem: String, token: String?) async throws -> DetailInternet {
        try await readInternet(["modems", modem, "internet-connections", "current"], token: token)
    }

    func connectInternet(_ modem: String, preferences: InternetPreferences, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections"], method: "POST",
                         body: JSONEncoder().encode(preferences.submission), token: token)
    }

    func disconnectInternet(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "internet-connections", "current"], method: "DELETE",
                              token: token)
    }

    func internetOptions(_ modem: String, value: InternetConnectionOptions, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections", "current", "preferences"], method: "PUT",
                         body: JSONEncoder().encode(value), token: token)
    }

    func internetPublicInfo(_ modem: String, token: String?) async throws -> InternetPublicInfo {
        try await readInternet(["modems", modem, "internet-connections", "public"], token: token)
    }

    private func readInternet<Value: Decodable & Sendable>(_ path: [String], token: String?) async throws -> Value {
        // Status reads share the modem lock with background connection work.
        // Retry only this temporary conflict, with a bounded, cancellable wait; never replay writes.
        for delay in [250, 500, 1_000, 2_000] {
            do { return try await decode(path, token: token) }
            catch let failure as BackendFailure where failure.source == .backend
                && failure.status == 409 && failure.code == "internet_operation_in_progress" {
                try await Task.sleep(for: .milliseconds(delay))
            }
        }
        return try await decode(path, token: token)
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
                         method: "POST", token: token)
    }

    func volteSettings(_ modem: String, token: String?) async throws -> VoLTESettings {
        try await decode(["modems", modem, "volte", "settings"], token: token)
    }

    func updateVoLTE(_ modem: String, settings: VoLTESubmission, token: String?) async throws {
        _ = try await request(["modems", modem, "volte", "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token)
    }
}

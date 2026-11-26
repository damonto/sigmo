import Foundation

extension BackendAPI {
    func networkRegistration(_ modem: String, token: String?) async throws -> NetworkRegistration {
        try await decode(["modems", modem, "network-settings", "registration"], token: token, timeout: .hardware)
    }

    func setNetworkRegistration(_ modem: String, value: NetworkRegistration, token: String?) async throws {
        _ = try await request(["modems", modem, "network-settings", "registration"], method: "PUT",
                              body: JSONEncoder().encode(value), token: token, timeout: .hardware)
    }

    func startNetworkScan(_ modem: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans"], method: "POST", token: token, timeout: .hardware)
    }

    func networkScan(_ modem: String, id: String, token: String?) async throws -> NetworkScan {
        try await decode(["modems", modem, "network-scans", id], token: token, timeout: .hardware)
    }

    func detailInternet(_ modem: String, token: String?) async throws -> DetailInternet {
        try await readInternet(["modems", modem, "internet-connections", "current"], token: token, timeout: .hardware)
    }

    func internetTraffic(_ modem: String, token: String?) async throws -> InternetTrafficResponse {
        try await decode(["modems", modem, "internet-connections", "current", "traffic"], token: token, timeout: .traffic)
    }

    func connectInternet(_ modem: String, preferences: InternetPreferences, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections"], method: "POST",
                         body: JSONEncoder().encode(preferences.submission), token: token, timeout: .hardware)
    }

    func disconnectInternet(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "internet-connections", "current"], method: "DELETE",
                              token: token, timeout: .hardware)
    }

    func internetOptions(_ modem: String, value: InternetConnectionOptions, token: String?) async throws -> DetailInternet {
        try await decode(["modems", modem, "internet-connections", "current", "preferences"], method: "PUT",
                         body: JSONEncoder().encode(value), token: token, timeout: .hardware)
    }

    func internetPublicInfo(_ modem: String, token: String?) async throws -> InternetPublicInfo {
        try await readInternet(["modems", modem, "internet-connections", "public"], token: token, timeout: .hardware)
    }

    func latestInternetSpeedTest(_ modem: String, token: String?) async throws -> InternetSpeedTestStatus {
        try await decode(["modems", modem, "internet-connections", "speed-tests", "latest"], token: token)
    }

    func runInternetSpeedTest(_ modem: String, iccid: String, size: InternetSpeedTestSize,
                              token: String?) async throws -> InternetSpeedTestStatus {
        try await decode(["modems", modem, "internet-connections", "speed-tests"], method: "POST",
                         body: JSONEncoder().encode(InternetSpeedTestRequest(iccid: iccid, sizeMB: size.rawValue)),
                         token: token, timeout: .speedTest)
    }

    private func readInternet<Value: Decodable & Sendable>(_ path: [String], token: String?, timeout: BackendRequestTimeout) async throws -> Value {
        // Status reads share the modem lock with background connection work.
        // Retry only this temporary conflict, with a bounded, cancellable wait; never replay writes.
        for delay in [250, 500, 1_000, 2_000] {
            do { return try await decode(path, token: token, timeout: timeout) }
            catch let failure as BackendFailure where failure.source == .backend
                && failure.status == 409 && failure.code == "internet_operation_in_progress" {
                try await Task.sleep(for: .milliseconds(delay))
            }
        }
        return try await decode(path, token: token, timeout: timeout)
    }

    func wifiCallingProxies(token: String?) async throws -> [WiFiProxy] {
        try await decode(["wifi-calling", "proxies"], token: token)
    }

    func saveWiFiProxy(_ id: String?, settings: WiFiProxySettings, token: String?) async throws -> WiFiProxy {
        let collection = ["wifi-calling", "proxies"]
        return try await decode(id.map { collection + [$0] } ?? collection,
                                method: id == nil ? "POST" : "PUT",
                                body: JSONEncoder().encode(settings), token: token)
    }

    func checkWiFiProxy(_ modem: String, proxyId: String, token: String?) async throws -> WiFiProxyCheck {
        try await decode(["modems", modem, "wifi-calling", "connectivity-checks"], method: "POST",
                         body: JSONEncoder().encode(["proxyId": proxyId]), token: token, timeout: .hardware)
    }

    func updateWiFiCalling(_ modem: String, settings: WiFiSubmission, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token, timeout: .hardware)
    }

    func reconnectWiFiCalling(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "sessions"], method: "POST", token: token, timeout: .hardware)
    }

    func disconnectWiFiCalling(_ modem: String, token: String?) async throws {
        _ = try await request(["modems", modem, "wifi-calling", "sessions", "current"], method: "DELETE", token: token, timeout: .hardware)
    }

    func wifiCallingWebsheet(_ modem: String, emergencyAddress: Bool, token: String?) async throws -> CarrierWebsheet {
        try await decode(["modems", modem, "wifi-calling", emergencyAddress ? "emergency-address-websheets" : "websheets"],
                         method: "POST", token: token, timeout: .hardware)
    }

    func volteSettings(_ modem: String, token: String?) async throws -> VoLTESettings {
        try await decode(["modems", modem, "volte", "settings"], token: token, timeout: .hardware)
    }

    func updateVoLTE(_ modem: String, settings: VoLTESubmission, token: String?) async throws {
        _ = try await request(["modems", modem, "volte", "settings"], method: "PUT",
                              body: JSONEncoder().encode(settings), token: token, timeout: .hardware)
    }
}

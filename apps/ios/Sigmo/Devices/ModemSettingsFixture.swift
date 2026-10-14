import Foundation

// A value per modem keeps fixture writes isolated just like the backend routes.
struct ModemSettingsFixture: Sendable {
    var airplaneSupported = true
    var airplaneEnabled = false
    var device = ModemDeviceSettings(alias: "", mss: 240)
    var mode = ModemModeSelection(allowed: 68, preferred: 0)
    var supportedModes = [
        ModemNetworkMode(allowed: 4, preferred: 0, allowedLabel: "LTE", preferredLabel: "None", current: false),
        ModemNetworkMode(allowed: 68, preferred: 0, allowedLabel: "LTE + 5G SA", preferredLabel: "None", current: true),
        ModemNetworkMode(allowed: 68, preferred: 64, allowedLabel: "LTE + 5G SA", preferredLabel: "5G SA", current: false),
    ]
    var supportedBands = [
        ModemNetworkBand(value: .init(technology: 4, number: 1), label: "LTE B1", current: true),
        ModemNetworkBand(value: .init(technology: 4, number: 3), label: "LTE B3", current: true),
        ModemNetworkBand(value: .init(technology: 64, number: 1), label: "NR n1", current: true),
        ModemNetworkBand(value: .init(technology: 64, number: 78), label: "NR n78", current: true),
    ]
    var selectedBands = [ModemBandValue(technology: 4, number: 1), .init(technology: 4, number: 3),
                         .init(technology: 64, number: 1), .init(technology: 64, number: 78)]
    var statuses: [String: Int] = [:]
    var failNextWrite = false
    var disconnectAfterWrite = false

    mutating func respond(_ request: URLRequest, resource: String) throws -> BackendHTTPResponse? {
        let paths = ["/networks/airplane-mode", "/networks/modes", "/networks/current-modes",
                     "/networks/bands", "/networks/current-bands", "/settings"]
        guard paths.contains(resource) else { return nil }
        let method = request.httpMethod ?? "GET"
        if let status = statuses[method + " " + resource], status >= 400 {
            return error(status == 401 ? "invalid_session" : "remote_error", status: status)
        }
        if method == "PUT", failNextWrite {
            failNextWrite = false
            return error("remote_error", status: 503)
        }
        let data = request.httpBody ?? Data()
        switch (method, resource) {
        case ("GET", "/networks/airplane-mode"):
            return try reply(ModemAirplaneMode(supported: airplaneSupported, enabled: airplaneEnabled))
        case ("PUT", "/networks/airplane-mode"):
            guard airplaneSupported else { return error("airplane_mode_unsupported", status: 400) }
            struct Submission: Decodable { let enabled: Bool }
            airplaneEnabled = try JSONDecoder().decode(Submission.self, from: data).enabled
        case ("GET", "/networks/modes"):
            let options = supportedModes.map {
                ModemNetworkMode(allowed: $0.allowed, preferred: $0.preferred, allowedLabel: $0.allowedLabel,
                                 preferredLabel: $0.preferredLabel, current: $0.id == mode)
            }
            guard let current = options.first(where: { $0.current }) else { return error("get_modes_failed", status: 500) }
            return try reply(ModemNetworkModes(supported: options, current: current))
        case ("PUT", "/networks/current-modes"):
            let selection = try JSONDecoder().decode(ModemModeSelection.self, from: data)
            guard supportedModes.contains(where: { $0.id == selection }) else { return error("unsupported_mode", status: 400) }
            mode = selection
        case ("GET", "/networks/bands"):
            let options = supportedBands.map {
                ModemNetworkBand(value: $0.value, label: $0.label, current: selectedBands.contains($0.value))
            }
            return try reply(ModemNetworkBands(supported: options, current: selectedBands))
        case ("PUT", "/networks/current-bands"):
            struct Submission: Decodable { let bands: [ModemBandValue] }
            let bands = try JSONDecoder().decode(Submission.self, from: data).bands
            guard !bands.isEmpty else { return error("bands_required", status: 400) }
            guard Set(bands).count == bands.count else { return error("duplicate_band", status: 400) }
            guard Set(bands).isSubset(of: Set(supportedBands.map(\.value))) else { return error("unsupported_band", status: 400) }
            selectedBands = bands
        case ("GET", "/settings"):
            return try reply(device)
        case ("PUT", "/settings"):
            let value = try JSONDecoder().decode(ModemDeviceSettings.self, from: data)
            guard (64...254).contains(value.mss) else { return error("update_settings_invalid_request", status: 400) }
            device = ModemDeviceSettings(alias: value.alias.trimmingCharacters(in: .whitespacesAndNewlines), mss: value.mss)
        default:
            return error("method_not_allowed", status: 405)
        }
        if disconnectAfterWrite {
            disconnectAfterWrite = false
            throw URLError(.networkConnectionLost)
        }
        return BackendHTTPResponse(data: Data(), status: 204)
    }

    private func reply<Value: Encodable>(_ value: Value) throws -> BackendHTTPResponse {
        BackendHTTPResponse(data: try JSONEncoder().encode(value), status: 200)
    }

    private func error(_ code: String, status: Int) -> BackendHTTPResponse {
        HTTPProblem.response(status: status, code: code, detail: "Fixture settings error")
    }
}

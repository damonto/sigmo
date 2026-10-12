#if DEBUG
import Foundation

struct ConnectionFixture: Sendable {
    static let carrierHTML = """
    <!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1">
    <style>body { font: -apple-system-body; padding: 20px; } button { font: inherit; min-height: 48px; margin: 12px 0; width: 100%; }</style></head>
    <body><h1>Carrier Setup</h1><p>Local carrier fixture</p>
    <button onclick="window.postMessage({type:'sigmo-websheet-callback',callback:{event:'progress'}},location.origin)">Continue Setup</button>
    <button onclick="window.postMessage({type:'sigmo-websheet-callback',callback:{event:'progress'}},location.origin); window.postMessage({type:'sigmo-websheet-callback',callback:{event:'entitlementChanged'}},location.origin)">Complete Setup</button>
    </body></html>
    """
    var registration = NetworkRegistration(mode: "automatic", operatorCode: nil)
    var registeredOperator = RegisteredOperator(name: "Example Mobile", code: "46000")
    var registrationState = "Home"
    var networks = [
        NetworkOperator(status: "Current", operatorName: "Example Mobile", operatorShortName: "Example", operatorCode: "46000", accessTechnologies: ["LTE"]),
        NetworkOperator(status: "Available", operatorName: "Travel Mobile", operatorShortName: "Travel", operatorCode: "23410", accessTechnologies: ["LTE"]),
        NetworkOperator(status: "Available", operatorName: "Travel Mobile", operatorShortName: "Travel", operatorCode: "23410", accessTechnologies: ["5G SA"])
    ]
    var scanPolls = 0
    var scanOutcome = "completed"
    var scanErrorCode: String?
    var scanStarted = false
    var internet = ConnectionFixtureInternet()
    var wifi = DetailWiFiSettings(enabled: false, underlay: .init(mode: "modem", modemId: "device-2"),
                                  connected: false, state: "disconnected", lastError: nil)
    var volte = VoLTESettings(enabled: true, connected: true, state: "connected", durationSeconds: 120,
                              modemRegistered: false, dataPath: "qmap")
    var wifiRequiresSetup = false
    var statuses: [String: Int] = [:]
    var busyInternetReads: [String: Int] = [:]
    var disconnectAfterWrite = false
    private var sheets: Set<String> = []

    mutating func respond(_ request: URLRequest, resource: String, modemID: String, airplane: Bool) throws -> BackendHTTPResponse? {
        guard resource.hasPrefix("/internet-connections") || resource.hasPrefix("/wifi-calling/")
                || resource == "/volte/settings" || resource == "/networks/registration"
                || resource.hasPrefix("/network-scans") else { return nil }
        let method = request.httpMethod ?? "GET"
        if let status = statuses[method + " " + resource], status >= 400 { return error("remote_error", status: status) }
        if method == "GET", let remaining = busyInternetReads[resource], remaining > 0 {
            busyInternetReads[resource] = remaining - 1
            return error("internet_operation_in_progress", status: 409)
        }
        let data = request.httpBody ?? Data()
        switch (method, resource) {
        case ("GET", "/networks/registration"):
            return try reply(registration)
        case ("PUT", "/networks/registration"):
            if airplane { return error("conflict", status: 409) }
            let value = try JSONDecoder().decode(NetworkRegistration.self, from: data)
            guard value.automatic != nil else { return error("registration_mode_invalid", status: 400) }
            if value.mode == "manual" {
                guard let code = value.operatorCode, !code.isEmpty else { return error("operator_code_required", status: 400) }
                registeredOperator = .init(name: networks.first { $0.id == code }?.name ?? code, code: code)
            } else { registeredOperator = .init(name: "Example Mobile", code: "46000") }
            registration = value
        case ("POST", "/network-scans"):
            if airplane { return error("conflict", status: 409) }
            let created = !scanStarted
            scanStarted = true
            return try reply(scanResponse, status: created ? 201 : 200)
        case ("GET", let resource) where resource.hasPrefix("/network-scans/"):
            guard scanStarted, resource == "/network-scans/scan-1" else { return error("network_scan_not_found", status: 404) }
            if scanPolls > 0 { scanPolls -= 1 }
            return try reply(scanResponse)
        case ("GET", "/internet-connections/current"):
            var current = internet
            if airplane { current.connected = false }
            return try reply(current)
        case ("POST", "/internet-connections"):
            if airplane { return error("internet_airplane_mode", status: 409) }
            internet.preferences = try JSONDecoder().decode(InternetPreferences.self, from: data)
            internet.connected = true
            internet.bearer += 1
            try interruptWriteIfNeeded()
            return try reply(internet, status: 201)
        case ("DELETE", "/internet-connections/current"):
            internet.connected = false
        case ("PUT", "/internet-connections/current/preferences"):
            guard internet.connected else { return error("internet_connection_not_connected", status: 409) }
            let options = try JSONDecoder().decode(InternetConnectionOptions.self, from: data)
            internet.preferences.defaultRoute = options.defaultRoute
            internet.preferences.proxyEnabled = options.proxyEnabled
            internet.preferences.alwaysOn = options.alwaysOn
            try interruptWriteIfNeeded()
            return try reply(internet)
        case ("GET", "/internet-connections/public"):
            guard internet.connected else { return error("internet_connection_not_connected", status: 409) }
            return try reply(InternetPublicInfo(ip: "203.0.113.\(internet.bearer)", country: "GB", organization: "Fixture Mobile Network"))
        case ("GET", "/wifi-calling/settings"):
            return try reply(wifi)
        case ("PUT", "/wifi-calling/settings"):
            struct Submission: Decodable { let enabled: Bool; let underlay: WiFiUnderlay }
            let settings = try JSONDecoder().decode(Submission.self, from: data)
            wifi.enabled = settings.enabled
            wifi.underlay = settings.underlay
            updateWiFi(modemID: modemID)
        case ("POST", "/wifi-calling/sessions"):
            guard wifi.enabled else { return error("ims_session_unavailable", status: 400) }
            updateWiFi(modemID: modemID)
            try interruptWriteIfNeeded()
            return BackendHTTPResponse(data: Data(), status: 202)
        case ("DELETE", "/wifi-calling/sessions/current"):
            wifi.connected = false
            wifi.state = "disconnected"
            wifi.durationSeconds = 0
        case ("POST", "/wifi-calling/websheets"):
            guard let sheet = wifi.websheet else { return error("ims_websheet_not_pending", status: 400) }
            return try reply(sheet, status: 201)
        case ("POST", "/wifi-calling/emergency-address-websheets"):
            guard wifi.emergencyAddressUpdateAvailable else { return error("ims_websheet_unavailable", status: 400) }
            let sheet = sheet(id: "e911-" + modemID, title: "Emergency Address")
            sheets.insert(sheet.id)
            return try reply(sheet, status: 201)
        case ("GET", "/volte/settings"):
            var current = volte
            if airplane { current.connected = false; current.modemRegistered = false; current.state = "disconnected" }
            return try reply(current)
        case ("PUT", "/volte/settings"):
            if airplane { return error("volte_airplane_mode", status: 409) }
            struct Submission: Decodable { let enabled: Bool; let dataPath: String? }
            let value = try JSONDecoder().decode(Submission.self, from: data)
            if volte.dataPath != "mbim" {
                guard let path = value.dataPath, VoLTEDataPath(rawValue: path) != nil else {
                    return error("update_volte_settings_invalid_request", status: 422)
                }
                volte.dataPath = path
            }
            volte.enabled = value.enabled
            volte.connected = value.enabled
            volte.state = value.enabled ? "connected" : "disconnected"
        default:
            return error("method_not_allowed", status: 405)
        }
        try interruptWriteIfNeeded()
        return BackendHTTPResponse(data: Data(), status: 204)
    }

    mutating func callback(_ callback: CarrierCallback, id: String) -> BackendHTTPResponse? {
        guard sheets.contains(id) else { return nil }
        if callback.event == "entitlementChanged" || callback.event == "finishFlow" || callback.resultCode == "success" {
            if id.hasPrefix("wfc-") {
                wifiRequiresSetup = false
                wifi.connected = wifi.enabled
                wifi.state = wifi.enabled ? "connected" : "disconnected"
                wifi.websheet = nil
            }
            sheets.remove(id)
        }
        return BackendHTTPResponse(data: Data(), status: 204)
    }

    private var scanResponse: NetworkScan {
        let running = scanPolls != 0
        return NetworkScan(id: "scan-1", status: running ? "running" : scanOutcome,
                           networks: running || scanOutcome != "completed" ? nil : networks, errorCode: scanErrorCode)
    }

    private mutating func updateWiFi(modemID: String) {
        wifi.connected = wifi.enabled && !wifiRequiresSetup
        wifi.state = !wifi.enabled ? "disconnected" : wifiRequiresSetup ? "websheet_required" : "connected"
        wifi.durationSeconds = 0
        if wifi.enabled, wifiRequiresSetup {
            let sheet = sheet(id: "wfc-" + modemID, title: "Carrier Setup")
            wifi.websheet = sheet
            sheets.insert(sheet.id)
        } else { wifi.websheet = nil }
    }

    private func sheet(id: String, title: String) -> CarrierWebsheet {
        .init(id: id, embedUrl: "/api/v1/websheets/" + id, title: title, url: "https://carrier.example/setup", method: "GET")
    }

    private mutating func interruptWriteIfNeeded() throws {
        if disconnectAfterWrite { disconnectAfterWrite = false; throw URLError(.timedOut) }
    }

    private func reply<Value: Encodable>(_ value: Value, status: Int = 200) throws -> BackendHTTPResponse {
        BackendHTTPResponse(data: try JSONEncoder().encode(value), status: status)
    }
    private func error(_ code: String, status: Int) -> BackendHTTPResponse {
        return HTTPProblem.response(status: status, code: code, detail: "Fixture connection error")
    }
}

struct ConnectionFixtureInternet: Encodable, Sendable {
    var preferences = InternetPreferences(apn: "example.apn", ipType: "ipv4v6", apnUsername: "user", apnPassword: "secret", apnAuth: "pap",
                                          defaultRoute: false, proxyEnabled: true, alwaysOn: true)
    var connected = true
    var bearer: UInt64 = 1

    private enum CodingKeys: String, CodingKey {
        case status, proxy, interfaceName, bearer, ipv4Addresses, ipv6Addresses, dns, durationSeconds, txBytes, rxBytes, routeMetric
    }
    func encode(to encoder: Encoder) throws {
        try preferences.encode(to: encoder)
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(connected ? "connected" : "disconnected", forKey: .status)
        try values.encode(connected ? "wwan0" : "", forKey: .interfaceName)
        try values.encode(connected ? bearer : 0, forKey: .bearer)
        try values.encode(connected ? ["10.0.0.2/24"] : [], forKey: .ipv4Addresses)
        try values.encode(connected ? ["2001:db8::2/64"] : [], forKey: .ipv6Addresses)
        try values.encode(connected ? ["1.1.1.1"] : [], forKey: .dns)
        try values.encode(connected ? 65 : 0, forKey: .durationSeconds)
        try values.encode(connected ? 4096 : 0, forKey: .txBytes)
        try values.encode(connected ? 8192 : 0, forKey: .rxBytes)
        try values.encode(connected ? 100 : 0, forKey: .routeMetric)
        try values.encode(InternetProxy(enabled: connected && preferences.proxyEnabled, username: "proxy", password: "p@ss:word/测试",
                                        httpAddress: "192.0.2.1:8080", socks5Address: "[2001:db8::1]:1080"), forKey: .proxy)
    }
}
#endif

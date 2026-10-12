import Foundation

struct NetworkRegistration: Codable, Sendable, Equatable {
    let mode: String
    let operatorCode: String?

    var automatic: Bool? {
        switch mode {
        case "automatic": true
        case "manual": false
        default: nil
        }
    }
}

struct NetworkOperator: Codable, Sendable, Equatable, Identifiable {
    var status: String
    var operatorName: String
    var operatorShortName: String
    let operatorCode: String
    var accessTechnologies: [String]
    var id: String { operatorCode }
    var name: String {
        [operatorName, operatorShortName, operatorCode].first { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty } ?? operatorCode
    }

    // A PLMN may appear once per radio technology, but registration takes only its code.
    static func merged(_ operators: [Self]) -> [Self] {
        var result: [Self] = []
        for network in operators where !network.operatorCode.isEmpty {
            guard let index = result.firstIndex(where: { $0.id == network.id }) else {
                result.append(network)
                continue
            }
            if result[index].operatorName.isEmpty { result[index].operatorName = network.operatorName }
            if result[index].operatorShortName.isEmpty { result[index].operatorShortName = network.operatorShortName }
            for technology in network.accessTechnologies where !result[index].accessTechnologies.contains(technology) {
                result[index].accessTechnologies.append(technology)
            }
            let priority = ["Unknown", "Forbidden", "Available", "Current"]
            if (priority.firstIndex(of: network.status) ?? 0) > (priority.firstIndex(of: result[index].status) ?? 0) {
                result[index].status = network.status
            }
        }
        return result
    }
}

struct NetworkScan: Codable, Sendable {
    let id: String
    let status: String
    let networks: [NetworkOperator]?
    let errorCode: String?
}

struct InternetPreferences: Codable, Sendable, Equatable {
    var apn: String
    var ipType: String
    var apnUsername: String
    var apnPassword: String
    var apnAuth: String
    var defaultRoute: Bool
    var proxyEnabled: Bool
    var alwaysOn: Bool

    static var automatic: Self {
        Self(apn: "", ipType: "ipv4v6", apnUsername: "", apnPassword: "", apnAuth: "",
             defaultRoute: false, proxyEnabled: false, alwaysOn: false)
    }
    var submission: Self {
        var value = self
        value.apn = apn.trimmingCharacters(in: .whitespacesAndNewlines)
        value.apnUsername = apnUsername.trimmingCharacters(in: .whitespacesAndNewlines)
        value.ipType = ipType.isEmpty ? "ipv4v6" : ipType
        value.apnAuth = apnAuth == "default" ? "" : apnAuth
        return value
    }
    var valid: Bool {
        let value = submission
        return [value.apn, value.apnUsername, value.apnPassword].allSatisfy { $0.unicodeScalars.count <= 100 }
            && ["ipv4", "ipv6", "ipv4v6"].contains(value.ipType)
            && ["", "none", "pap", "chap", "pap|chap", "mschapv2"].contains(value.apnAuth)
    }
    var connectionOptions: InternetConnectionOptions {
        .init(defaultRoute: defaultRoute, proxyEnabled: proxyEnabled, alwaysOn: alwaysOn)
    }
}

struct InternetConnectionOptions: Codable, Sendable, Equatable {
    var defaultRoute: Bool
    var proxyEnabled: Bool
    var alwaysOn: Bool
}

struct InternetProxy: Codable, Sendable, Equatable {
    let enabled: Bool
    let username: String?
    let password: String?
    let httpAddress: String?
    let socks5Address: String?

    func url(socks: Bool) -> String? {
        guard enabled, let username, !username.isEmpty, let password, !password.isEmpty,
              let address = socks ? socks5Address : httpAddress, !address.isEmpty,
              var components = URLComponents(string: (socks ? "socks5h://" : "http://") + address),
              components.host?.isEmpty == false else { return nil }
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-._~"))
        components.percentEncodedUser = username.addingPercentEncoding(withAllowedCharacters: allowed)
        components.percentEncodedPassword = password.addingPercentEncoding(withAllowedCharacters: allowed)
        return components.string
    }
}

struct DetailInternet: Decodable, Sendable, Equatable {
    let status: String
    let preferences: InternetPreferences
    let proxy: InternetProxy?
    let interfaceName: String?
    let bearer: UInt64?
    let ipv4Addresses: [String]
    let ipv6Addresses: [String]
    let dns: [String]
    let durationSeconds: Int64?
    let txBytes: UInt64?
    let rxBytes: UInt64?
    let routeMetric: Int?

    var connected: Bool { status == "connected" }
    var knownStatus: Bool { ["connected", "disconnected"].contains(status) }
    var connectionID: String? {
        guard connected else { return nil }
        if let bearer, bearer != 0 { return "bearer:\(bearer)" }
        return interfaceName.flatMap { $0.isEmpty ? nil : "interface:" + $0 }
    }

    private enum CodingKeys: String, CodingKey {
        case status, proxy, interfaceName, bearer, ipv4Addresses, ipv6Addresses, dns
        case durationSeconds, txBytes, rxBytes, routeMetric
    }
    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        status = try values.decode(String.self, forKey: .status)
        preferences = try InternetPreferences(from: decoder)
        proxy = try values.decodeIfPresent(InternetProxy.self, forKey: .proxy)
        interfaceName = try values.decodeIfPresent(String.self, forKey: .interfaceName)
        bearer = try values.decodeIfPresent(UInt64.self, forKey: .bearer)
        ipv4Addresses = try values.decodeIfPresent([String].self, forKey: .ipv4Addresses) ?? []
        ipv6Addresses = try values.decodeIfPresent([String].self, forKey: .ipv6Addresses) ?? []
        dns = try values.decodeIfPresent([String].self, forKey: .dns) ?? []
        durationSeconds = try values.decodeIfPresent(Int64.self, forKey: .durationSeconds)
        txBytes = try values.decodeIfPresent(UInt64.self, forKey: .txBytes)
        rxBytes = try values.decodeIfPresent(UInt64.self, forKey: .rxBytes)
        routeMetric = try values.decodeIfPresent(Int.self, forKey: .routeMetric)
    }
}

struct InternetPublicInfo: Codable, Sendable, Equatable {
    let ip: String?
    let country: String?
    let organization: String?
}

struct WiFiUnderlay: Codable, Sendable, Equatable, Hashable {
    let mode: String
    var modemId: String? = nil
    static let system = Self(mode: "system")
    static let current = Self(mode: "self")
}

struct DetailWiFiSettings: Codable, Sendable, Equatable {
    var enabled: Bool
    var underlay: WiFiUnderlay
    var connected: Bool
    var state: String
    var lastError: String?
    var durationSeconds: Int64 = 0
    var emergencyAddressUpdateAvailable = false
    var websheet: CarrierWebsheet? = nil

    var statusKey: String {
        if !enabled { return "modem.value.disabled" }
        if connected { return "common.connected" }
        switch state {
        case "connected": return "common.connected"
        case "connecting": return "modem.value.connecting"
        case "waiting_for_uplink": return "connection.waitingForUplink"
        case "websheet_required": return "connection.setupRequired"
        case "idle", "disconnected": return "common.disconnected"
        default: return "common.unknown"
        }
    }
}

struct WiFiSubmission: Encodable, Sendable {
    let enabled: Bool
    let underlay: WiFiUnderlay
}

enum VoLTEDataPath: String, CaseIterable, Sendable {
    case qmap
    case legacy = "legacy_bam_dmux"
    case qualcomm410 = "qualcomm_410"

    var title: String {
        switch self {
        case .qmap: "QMAP"
        case .legacy: "Legacy BAM-DMUX"
        case .qualcomm410: "Qualcomm 410"
        }
    }
    var explanation: String { "connection.dataPath." + rawValue }
}

struct VoLTESettings: Codable, Sendable, Equatable {
    var enabled: Bool
    var connected: Bool
    var state: String
    var durationSeconds: Int64
    var modemRegistered: Bool
    var dataPath: String

    var modemManaged: Bool { !enabled && modemRegistered }
    var knownDataPath: Bool { dataPath == "mbim" || VoLTEDataPath(rawValue: dataPath) != nil }
    var statusKey: String {
        if modemManaged { return "connection.modemManaged" }
        if !enabled { return "modem.value.disabled" }
        if connected { return "common.connected" }
        switch state {
        case "connected": return "common.connected"
        case "connecting": return "modem.value.connecting"
        case "idle", "disconnected": return "common.disconnected"
        default: return "common.unknown"
        }
    }
}

struct VoLTESubmission: Encodable, Sendable {
    let enabled: Bool
    let dataPath: String?
}

extension Modem {
    var networkSelectionName: String {
        if airplaneMode { return String(localized: "modem.airplane") }
        // This field describes the registered network, independently of the SIM brand.
        let name = registeredOperator.name.trimmingCharacters(in: .whitespacesAndNewlines)
        if !name.isEmpty { return name }
        let code = registeredOperator.code.trimmingCharacters(in: .whitespacesAndNewlines)
        if !code.isEmpty { return code }
        // The backend emits title-cased 3GPP states (Home, Roaming, Searching).
        switch registrationState.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "searching": return String(localized: "modem.value.searching")
        case "idle", "denied": return String(localized: "connection.noService")
        default: return String(localized: "common.unknown")
        }
    }
}

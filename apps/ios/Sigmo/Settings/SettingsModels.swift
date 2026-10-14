import Foundation

// The backend schema has a finite set of JSON value shapes. Keep them explicit
// instead of passing Any through form controls and request encoding.
enum SettingValue: Codable, Equatable, Sendable {
    case text(String), number(Int), toggle(Bool), list([String]), pairs([String: String]), null

    init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() { self = .null }
        else if let result = try? value.decode(Bool.self) { self = .toggle(result) }
        else if let result = try? value.decode(Int.self) { self = .number(result) }
        else if let result = try? value.decode(String.self) { self = .text(result) }
        else if let result = try? value.decode([String].self) { self = .list(result) }
        else { self = .pairs(try value.decode([String: String].self)) }
    }

    func encode(to encoder: Encoder) throws {
        var value = encoder.singleValueContainer()
        switch self {
        case .text(let text): try value.encode(text)
        case .number(let number): try value.encode(number)
        case .toggle(let toggle): try value.encode(toggle)
        case .list(let list): try value.encode(list)
        case .pairs(let pairs): try value.encode(pairs)
        case .null: try value.encodeNil()
        }
    }

    var text: String {
        switch self {
        case .text(let value): value
        case .number(let value): String(value)
        default: ""
        }
    }
    var bool: Bool { if case .toggle(let value) = self { value } else { false } }
    var strings: [String] { if case .list(let value) = self { value } else { [] } }
    var dictionary: [String: String] { if case .pairs(let value) = self { value } else { [:] } }
}

typealias SettingsFields = [String: SettingValue]

enum SettingsSection: Equatable, Sendable {
    case authentication
    case proxy
    case notifications
    case notification(String)

    var path: [String] {
        switch self {
        case .authentication: ["auth"]
        case .proxy: ["proxy"]
        case .notifications: ["notifications"]
        case .notification(let channel): ["channels", channel]
        }
    }
}

enum SettingsControl: String, Codable, Sendable {
    case text, password, number, select, list, keyValue, channelList
    case toggle = "switch"
    case unsupported

    init(from decoder: Decoder) throws {
        let rawValue = try decoder.singleValueContainer().decode(String.self)
        self = Self(rawValue: rawValue) ?? .unsupported
    }
}

struct SettingsField: Codable, Sendable, Identifiable {
    let key: String
    let label: String
    let description: String?
    let control: SettingsControl
    let required: Bool?
    let secret: Bool?
    let placeholder: String?
    let min: Int?
    let max: Int?
    let options: [SettingsOption]?
    var id: String { key }
}

struct SettingsOption: Codable, Sendable, Identifiable {
    let label: String
    let value: String
    var id: String { value }
}

struct NotificationSchema: Codable, Sendable, Identifiable {
    let key: String
    let label: String
    let description: String?
    let fields: [SettingsField]
    var id: String { key }
}

struct SettingsSchema: Codable, Sendable {
    let auth: [SettingsField]
    let proxy: [SettingsField]
    let channels: [NotificationSchema]
}

struct SettingsValues: Codable, Sendable {
    var auth: SettingsFields
    var notifications: SettingsFields
    var proxy: SettingsFields
    var channels: [String: SettingsFields]?
}

struct SettingsResponse: Codable, Sendable {
    let schema: SettingsSchema
    var values: SettingsValues
}

struct MCPSettings: Codable, Sendable {
    var enabled: Bool
    let endpointPath: String
    let auditRetentionDays: Int
    let permissions: [MCPPermission]?
}

struct MCPPermission: Codable, Sendable, Identifiable {
    let name: String
    let module: String
    var id: String { name }
}

struct MCPAPIKey: Codable, Sendable, Identifiable {
    let id: String
    let name: String
    let tokenHint: String
    var status: String
    let allModems: Bool
    let modemIds: [String]?
    let permissions: [String]?
    let createdAt: Date
    let expiresAt: Date
    let revokedAt: Date?
}

struct MCPKeyRequest: Encodable, Sendable {
    var name = ""
    var validityDays = 30
    var allModems = true
    var modemIds: [String] = []
    var permissions: [String] = []
    var isValid: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && name.trimmingCharacters(in: .whitespacesAndNewlines).utf8.count <= 64
            && (1...180).contains(validityDays) && !permissions.isEmpty && (allModems || !modemIds.isEmpty)
    }
}

struct MCPCreatedKey: Codable, Sendable {
    let apiKey: MCPAPIKey
    let token: String
}

// Keep the one-time token and its display configuration in the same lifetime.
struct MCPSecret: Sendable {
    let apiKey: MCPAPIKey
    let token: String
    let clientConfiguration: String
}
struct MCPKeys: Codable, Sendable { let apiKeys: [MCPAPIKey]? }
struct MCPAudit: Codable, Sendable { let events: [MCPAuditEvent]? }
struct MCPAuditEvent: Codable, Sendable, Identifiable {
    let id: Int64
    let keyId: String
    let keyName: String
    let tool: String
    let modemIds: [String]?
    let outcome: String
    let errorCode: String?
    let durationMs: Int
    let createdAt: Date
}

struct UpdatePreferences: Codable, Sendable, Equatable {
    var automatic: Bool
    var channel: String
}
struct UpdateManifest: Codable, Sendable {
    let version: String
    let notes: String
    let publishedAt: Date
}
struct UpdateLicense: Codable, Sendable {
    let status: String
    let deviceId: String
    let telegramId: Int64
    let displayName: String
    let username: String?
    let expiresAt: Date?
    let offlineUntil: Date?

    var statusKey: String {
        switch status {
        case "active": "settings.updates.licenseActive"
        case "revoked": "settings.updates.licenseRevoked"
        case "expired": "settings.updates.licenseExpired"
        default: "common.unknown"
        }
    }
}
struct UpdateSnapshot: Codable, Sendable {
    var settings: UpdatePreferences
    let current: AppInformation
    let latest: UpdateManifest?
    let license: UpdateLicense?
    var state: String
    let checkedAt: Date?
    let updateAvailable: Bool
    let selfUpdateSupported: Bool
    let unsupportedReason: String?
    let errorCode: String?
    let error: String?
    var busy: Bool { ["checking", "downloading", "verifying", "restarting"].contains(state) }
}

func settingsText(_ key: String) -> String {
    NSLocalizedString(key, comment: "")
}

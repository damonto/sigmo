import Foundation

struct ModemAirplaneMode: Codable, Equatable, Sendable {
    let supported: Bool
    let enabled: Bool
}

struct ModemModeSelection: Codable, Hashable, Sendable {
    let allowed: UInt64
    let preferred: UInt64

    var identifier: String { "\(allowed)-\(preferred)" }
}

struct ModemNetworkMode: Codable, Equatable, Identifiable, Sendable {
    let allowed: UInt64
    let preferred: UInt64
    let allowedLabel: String
    let preferredLabel: String
    let current: Bool

    var id: ModemModeSelection { ModemModeSelection(allowed: allowed, preferred: preferred) }
    var label: String { preferred == 0 ? allowedLabel : "\(allowedLabel) / \(preferredLabel)" }
}

struct ModemNetworkModes: Codable, Equatable, Sendable {
    let supported: [ModemNetworkMode]
    let current: ModemNetworkMode
}

struct ModemBandValue: Codable, Hashable, Sendable {
    let technology: UInt64
    let number: UInt16

    var identifier: String { "\(technology)-\(number)" }
}

struct ModemNetworkBand: Codable, Equatable, Identifiable, Sendable {
    let value: ModemBandValue
    let label: String
    let current: Bool

    var id: ModemBandValue { value }
}

struct ModemNetworkBands: Codable, Equatable, Sendable {
    let supported: [ModemNetworkBand]
    let current: [ModemBandValue]
}

struct ModemDeviceSettings: Codable, Equatable, Sendable {
    let alias: String
    let mss: Int
}

struct ModemDeviceDraft: Equatable {
    var alias: String
    var mss: String

    init(_ settings: ModemDeviceSettings) {
        alias = settings.alias
        mss = String(settings.mss)
    }

    var submission: ModemDeviceSettings? {
        let value = mss.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value.allSatisfy({ $0 >= "0" && $0 <= "9" }),
              let number = Int(value), (64...254).contains(number) else { return nil }
        return ModemDeviceSettings(alias: alias.trimmingCharacters(in: .whitespacesAndNewlines), mss: number)
    }
}

enum ModemSettingsSection: String, CaseIterable, Sendable {
    case airplane, mode, bands, device
}

enum ModemSettingsChange: Sendable {
    case airplane(Bool)
    case mode(ModemModeSelection)
    case bands([ModemBandValue])
    case device(ModemDeviceSettings)

    var section: ModemSettingsSection {
        switch self {
        case .airplane: .airplane
        case .mode: .mode
        case .bands: .bands
        case .device: .device
        }
    }

    var refreshSections: Set<ModemSettingsSection> {
        section == .device ? [.device] : [.airplane, .mode, .bands]
    }
}

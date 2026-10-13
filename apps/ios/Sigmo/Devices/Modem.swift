import Foundation

struct SIMSlot: Codable, Identifiable, Sendable, Equatable {
    let slot: Int
    let active: Bool
    let operatorName: String
    let operatorIdentifier: String
    let regionCode: String
    let identifier: String
    var icon: String?
    var reminder: SIMReminder?
    var id: Int { slot }
}

struct RegisteredOperator: Codable, Sendable, Equatable {
    let name: String
    let code: String
}

struct Modem: Codable, Identifiable, Sendable, Equatable {
    let id: String
    let name: String
    let manufacturer: String
    let primaryPort: String
    let firmwareRevision: String
    let hardwareRevision: String
    let number: String
    let state: String
    let unlockRequired: String
    let unlockSupported: Bool
    let sim: SIMSlot
    let slots: [SIMSlot]?
    let accessTechnology: String?
    let registrationState: String
    let registeredOperator: RegisteredOperator
    let signalQuality: Int
    let airplaneMode: Bool
    let simKind: String
    let internetConnected: Bool?
    let wifiCallingEnabled: Bool?
    let wifiCallingConnected: Bool?
    let volteEnabled: Bool?
    let volteConnected: Bool?

    var wifiCallingStatus: IMSConnectionState { .init(enabled: wifiCallingEnabled, connected: wifiCallingConnected) }
    var volteStatus: IMSConnectionState { .init(enabled: volteEnabled, connected: volteConnected) }

    var displayName: String { name.isEmpty ? id : name }
    var carrier: String {
        registeredOperator.name.isEmpty ? sim.operatorName : registeredOperator.name
    }
    var signalPercent: Int { min(100, max(0, signalQuality)) }
}

// Both IMS services use the same presentation without guessing omitted API values.
enum IMSConnectionState: Equatable {
    case disabled, connected, disconnected, unknown

    init(enabled: Bool?, connected: Bool?) {
        if enabled == false { self = .disabled }
        else if connected == true { self = .connected }
        else if enabled == true && connected == false { self = .disconnected }
        else { self = .unknown }
    }

    var displayName: String {
        switch self {
        case .disabled: String(localized: "modem.value.disabled")
        case .connected: String(localized: "common.connected")
        case .disconnected: String(localized: "common.disconnected")
        case .unknown: String(localized: "common.unknown")
        }
    }
}

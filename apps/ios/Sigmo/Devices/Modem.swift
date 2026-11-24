import Foundation

struct SIMSlot: Codable, Identifiable, Sendable, Equatable {
    let slot: Int
    let active: Bool
    let operatorName: String
    let operatorIdentifier: String
    let regionCode: String
    let identifier: String
    var label: String?
    var icon: String?
    var reminder: SIMReminder?
    var id: Int { slot }
}

struct RegisteredOperator: Codable, Sendable, Equatable {
    let name: String
    let code: String

    var displayName: String? {
        [name, code].lazy
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .first { !$0.isEmpty }
    }
}

struct Modem: Codable, Identifiable, Sendable, Equatable {
    enum CellularRegistration: String {
        case home, roaming, searching, denied, idle, unknown
    }

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

    var communicationLabel: String { sim.label.flatMap { $0.isEmpty ? nil : $0 } ?? (sim.operatorName.isEmpty ? displayName : sim.operatorName) }
    var displayName: String { name.isEmpty ? id : name }
    var selectionLabel: String {
        let profileLabel = sim.label?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return profileLabel.isEmpty ? displayName : "\(displayName) · \(profileLabel)"
    }
    var carrier: String {
        registeredOperator.name.isEmpty ? sim.operatorName : registeredOperator.name
    }

    var cellularRegistration: CellularRegistration {
        let state = registrationState.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return CellularRegistration(rawValue: state) ?? .unknown
    }

    var networkOverviewCarrier: String? {
        let registration = cellularRegistration
        if !airplaneMode, registration == .home || registration == .roaming,
           let name = registeredOperator.displayName { return name }
        return [sim.operatorName, sim.operatorIdentifier].lazy
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .first { !$0.isEmpty }
    }

    var signalPercent: Int { min(100, max(0, signalQuality)) }

    var cellularSignalFraction: Double? {
        guard !airplaneMode, signalPercent > 0 else { return nil }
        return Double(signalPercent) / 100
    }

    var cellularTechnology: String? {
        guard !airplaneMode, let technology = accessTechnology?.trimmingCharacters(in: .whitespacesAndNewlines),
              !technology.isEmpty else { return nil }
        return technology
    }

    var hasVoLTEConnection: Bool { !airplaneMode && volteStatus == .connected }
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

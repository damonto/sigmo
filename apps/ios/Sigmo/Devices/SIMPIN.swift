import Foundation

struct SIMPINStatus: Codable, Equatable, Sendable {
    let supported: Bool
    let enabled: Bool?
    let lock: SIMPINLock
    let pinRetries: UInt32?
    let pukRetries: UInt32?
    let simToken: String
}

enum SIMPINLock: String, Codable, Sendable {
    case unlocked = "none"
    case pin
    case puk
    case permanentlyBlocked = "permanently_blocked"
    case other
    case unknown

    init(from decoder: any Decoder) throws {
        let value = try decoder.singleValueContainer().decode(String.self)
        self = Self(rawValue: value) ?? .unknown
    }

    var unlockAction: SIMPINAction? {
        switch self {
        case .pin: .pin
        case .puk: .puk
        default: nil
        }
    }
}

enum SIMPINAction: String, CaseIterable, Sendable {
    case enable, disable, change, pin, puk

    var needsNewPIN: Bool { self == .change || self == .puk }
    var title: String { String(localized: String.LocalizationValue("simPin.actions." + rawValue)) }
}

struct SIMPINPageIdentity: Equatable {
    let accessRevision: Int
    let modem: ModemSIMIdentity?
}

func validSIMPIN(_ value: String) -> Bool {
    (4...8).contains(value.utf8.count) && value.utf8.allSatisfy { (48...57).contains($0) }
}

func validSIMPUK(_ value: String) -> Bool {
    value.utf8.count == 8 && value.utf8.allSatisfy { (48...57).contains($0) }
}

import Foundation

struct SIMReminder: Codable, Sendable, Equatable {
    let nextAt: Date
    let repeatDays: Int?
    let content: String
}

struct ReminderSubmission: Codable, Sendable {
    let scheduledAt: Date
    let repeatDays: Int?
    let content: String

    func isValid(now: Date = Date()) -> Bool {
        scheduledAt > now && !content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            (repeatDays.map { (1...3650).contains($0) } ?? true)
    }
}

struct SecureElements: Codable, Sendable {
    let ses: [SecureElement]
}

struct SecureElement: Codable, Identifiable, Sendable, Equatable {
    let id: String
    let label: String
    let aid: String?
    let eid: String?
    let freeSpace: Int64?
    let sasUp: Accreditation?
    let certificates: [String]?

    struct Accreditation: Codable, Sendable, Equatable {
        let name: String
        let region: String?
    }
}

struct ESIMProfiles: Codable, Sendable {
    var ses: [ESIMProfileGroup]
}

struct ESIMProfileGroup: Codable, Identifiable, Sendable {
    let id: String
    let label: String
    let aid: String?
    let eid: String?
    var profiles: [ESIMProfile]
}

struct ESIMProfile: Codable, Identifiable, Sendable, Equatable {
    let seId: String
    let seLabel: String
    let seEid: String?
    var name: String
    var number: String?
    let serviceProviderName: String
    let iccid: String
    let isdPAID: String?
    let icon: String
    let profileName: String
    let profileNickname: String?
    var profileState: UInt8
    let profileStateName: String
    let profileClass: String
    let profileOwner: ESIMOwner
    let regionCode: String?
    var reminder: SIMReminder?
    var id: String { seId + ":" + iccid }
    var enabled: Bool { profileState == 1 }
    @MainActor var numberLabel: String {
        let value = PhoneNumberDisplay.format(number ?? "", regionCode: regionCode)
        return value.isEmpty ? String(localized: "detail.noNumber") : value
    }
}

struct ESIMOwner: Codable, Sendable, Equatable {
    let mcc: String
    let mnc: String
    let gid1: String?
    let gid2: String?

    var plmn: String? {
        guard mcc.utf8.count == 3, (2...3).contains(mnc.utf8.count) else { return nil }
        let value = mcc + mnc
        return value.utf8.allSatisfy({ (48...57).contains($0) }) ? value : nil
    }
}

struct ESIMPreview: Codable, Sendable {
    let iccid: String
    let serviceProviderName: String
    let profileName: String
    let profileNickname: String?
    let profileState: String
    let profileOwner: ESIMOwner
    let icon: String?
    let regionCode: String?
}

struct ESIMDiscovery: Codable, Identifiable, Sendable {
    let eventId: String
    let address: String
    var id: String { eventId + ":" + address }
}

struct TransferSources: Codable, Sendable {
    let sources: [TransferSource]
    let ccidError: String?
}

struct TransferSource: Codable, Identifiable, Sendable, Equatable {
    let type: String
    let id: String
    let name: String
    let detail: String?
    let requiresSourceImei: Bool
    var cardPresent: Bool? = nil
    var key: String { type + ":" + id }
}

struct TransferProfile: Codable, Identifiable, Sendable {
    let id: String
    let seId: String?
    let type: String
    let name: String
    let serviceProviderName: String?
    let iccid: String
    let icon: String?
    let regionCode: String?
    let enabled: Bool
    let supported: Bool
    let unsupportedReason: String?
    let carrierName: String?
}

struct ESIMNotifications: Codable, Sendable {
    let ses: [ESIMNotificationGroup]
}

struct ESIMNotificationGroup: Codable, Identifiable, Sendable {
    let id: String
    let label: String
    let eid: String?
    let notifications: [ESIMNotification]
}

struct ESIMNotification: Codable, Identifiable, Sendable {
    let seId: String
    let seLabel: String
    let seEid: String?
    let sequenceNumber: String
    let iccid: String
    let smdp: String
    let operation: String
    var id: String { seId + ":" + sequenceNumber }
}

struct CarrierWebsheet: Codable, Identifiable, Sendable, Equatable {
    let id: String
    let embedUrl: String
    let title: String?
    let url: String
    let method: String
}

struct CarrierCallback: Codable, Sendable, Hashable {
    var event: String?
    var method: String?
    var resultCode: String?
    var activationCode: String?
    var iccid: String?
    var imei: String?
    var defaultSmdpAddress: String?
    var smdpFqdn: String?
    var nextAction: String?
    var source: String?
    var controller: String?
}

struct CarrierCallbackDelivery: Decodable, Sendable {
    enum Status: String, Decodable, Sendable {
        case native, submitted, failed
    }

    let callback: CarrierCallback
    let status: Status
}

struct LPAInput: Equatable {
    var smdp = ""
    var activationCode = ""
    var confirmationCode = ""
    var confirmationRequired = false

    var valid: Bool { !Self.compact(smdp).isEmpty && (!confirmationRequired || !confirmationCode.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) }
    static func compact(_ text: String) -> String { text.filter { !$0.isWhitespace } }
    static func parse(_ raw: String) -> LPAInput? {
        let parts = compact(raw).components(separatedBy: "$")
        guard parts.count >= 3, parts[0].uppercased().hasPrefix("LPA:"), !parts[1].isEmpty else { return nil }
        return LPAInput(smdp: parts[1], activationCode: parts[2].isEmpty && parts.count > 3 ? parts[3] : parts[2],
                        confirmationRequired: parts.count > 4 && parts[4] == "1")
    }
}

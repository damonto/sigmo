import SwiftUI

struct SigmoContact: Codable, Hashable, Identifiable, Sendable {
    var id = ""
    var name = ""
    var notes = ""
    var numbers: [ContactPhone] = []
    var hasAvatar = false
    var avatarColor = ""
    var updatedAt = ""
    var displayName: String { name.isEmpty ? numbers.first?.number ?? "" : name }
}

struct ContactPhone: Codable, Hashable, Sendable {
    var label = ""
    var number = ""
    var region = ""
    var key = ""
}

struct CommunicationLine: Codable, Hashable, Identifiable, Sendable {
    let profileId, modemId, label, number, region, kind, seId, deletionState, successorId: String
    let messageCount, callCount: Int
    let present, active, canMessage, canCall: Bool
    var id: String { profileId }
    var displayLabel: String { label.isEmpty ? (number.isEmpty ? profileId : number) : label }
    var deleted: Bool { deletionState == "deleted" || deletionState == "assigned" }
    var title: String {
        let source = "\(number.isEmpty ? (label.isEmpty ? profileId : label) : number) · \(modemId)"
        return deleted ? "\(source) · \(detailText("communication.deletedNumber"))" : source
    }
}

struct CommunicationPreference: Decodable, Sendable {
    let participant, profileId, updatedAt: String
}

struct CommunicationMessage: Decodable, Identifiable, Hashable, Sendable {
    let id: Int64
    let conversationId, profileId, historyProfileId, modemId: String
    let sender, recipient, text, timestamp, status, lineLabel, lineNumber: String
    let incoming: Bool
    var read: Bool
    var participant: String { incoming ? sender : recipient }
    var lineTitle: String { lineLabel.isEmpty ? (lineNumber.isEmpty ? profileId : lineNumber) : lineLabel }
}

struct CommunicationThread: Decodable, Identifiable, Hashable, Sendable {
    let id, participant: String
    let latestMessage: CommunicationMessage
    let unreadCount: Int
    let markedUnread: Bool
}

struct CommunicationItems<Item: Decodable & Sendable>: Decodable, Sendable { let items: [Item] }
struct CommunicationPage<Item: Decodable & Sendable>: Decodable, Sendable {
    let items: [Item]
    let nextCursor: String
}

enum CommunicationSection: String, Hashable {
    case messages, calls, contacts
    // Literal keys avoid turning a runtime key into a localized interpolation format.
    var title: LocalizedStringKey {
        switch self {
        case .messages: "communication.messages"
        case .calls: "communication.calls"
        case .contacts: "communication.contacts"
        }
    }
    var emptyTitle: LocalizedStringKey {
        switch self {
        case .messages: "communication.emptyMessages"
        case .calls: "communication.emptyCalls"
        case .contacts: "communication.emptyContacts"
        }
    }
    var searchPrompt: LocalizedStringKey {
        switch self {
        case .messages: "communication.searchMessages"
        case .calls: "communication.searchCalls"
        case .contacts: "communication.searchContacts"
        }
    }
    var icon: String {
        switch self { case .messages: "message"; case .calls: "phone"; case .contacts: "person.crop.circle" }
    }
}

struct CommunicationAction: Identifiable {
    let id = UUID()
    var number: String
    var calling = false
    var modemID: String?
    var preferredModemID: String?
}

struct BackendCommunicationAction: Identifiable {
    let session: BackendSession
    var action: CommunicationAction
    var id: UUID { action.id }
}

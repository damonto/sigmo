import Foundation

struct SMSMessage: Codable, Identifiable, Hashable, Sendable {
    let id: Int64
    var conversationId: String?
    var profileId: String?
    let sender: String
    let recipient: String
    let text: String
    let timestamp: Date
    let status: String
    let incoming: Bool
    var routed: Bool?
}

struct SMSConversation: Codable, Identifiable, Hashable, Sendable {
    let id: String
    let profileId: String
    let participant: String
    let modemId: String
    let lineLabel: String
    let lineNumber: String
    var readThroughMessageId: Int64
    var markedUnread: Bool
    var unreadCount: Int
    let latestMessage: SMSMessage
    let canSend: Bool
    var lastMessageId: Int64?

    @MainActor func participantLabel(regionCode: String?) -> String {
        let value = PhoneNumberDisplay.format(participant, regionCode: regionCode)
        return value.isEmpty ? String(localized: "messages.unknownSender") : value
    }

    @MainActor func lineDescription(regionCode: String?) -> String {
        if !lineLabel.isEmpty { return lineLabel }
        let number = PhoneNumberDisplay.format(lineNumber, regionCode: regionCode)
        if !number.isEmpty { return number }
        return String(format: String(localized: "messages.lineSuffix"), String(profileId.suffix(6)))
    }

    static func legacy(_ message: SMSMessage, modem: Modem) -> SMSConversation {
        let participant = message.incoming ? message.sender : message.recipient
        return SMSConversation(id: message.conversationId ?? "legacy:\(modem.sim.identifier):\(participant)",
            profileId: message.profileId ?? modem.sim.identifier, participant: participant,
            modemId: modem.id, lineLabel: modem.carrier, lineNumber: modem.number,
            readThroughMessageId: 0, markedUnread: false, unreadCount: 0,
            latestMessage: message, canSend: true)
    }
}

struct MessageSnapshot: Decodable, Sendable {
    let backendId: String
    let revision: Int64
    let unreadCount: Int
    let items: [SMSConversation]
}

struct MessageBadge: Codable, Equatable, Sendable {
    let backendId: String
    let revision: Int64
    let unreadCount: Int
}

struct SMSSubmission: Encodable, Sendable {
    let to: String
    let text: String
    let expectedProfileId: String
}

struct SMSSubmitted: Decodable, Sendable {
    let to: String
    let conversationId: String?
}

struct ConversationRead: Encodable, Sendable {
    var readThroughMessageId: Int64?
    var markedUnread: Bool?
}

extension BackendFailure {
    var unconfirmedMessageSend: Bool {
        // A proxy/server timeout or an invalid/lost response can follow an accepted SMS.
        guard let status else { return true }
        return isNetwork || status == 408 || status >= 500
    }
}

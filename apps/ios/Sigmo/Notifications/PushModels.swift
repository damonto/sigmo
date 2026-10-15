import CryptoKit
import Foundation

struct PushBinding: Codable, Identifiable, Equatable, Sendable {
    let id: String
    let backendID: String
    let sendToken: String
    var enabled = true
    var sms = true
    var reminders = true
    var calls: Bool?
    var allowsCalls: Bool { calls ?? true }
}

struct PushConnection: Codable, Equatable, Sendable {
    let configurationID: UUID
    let backendID: String
    let address: URL
}

struct PushIdentity: Codable, Equatable, Sendable {
    let id: String
    let managementToken: String
    var bindings: [PushBinding] = []
    var connections: [PushConnection] = []
    var revocations: [String] = []
    // Revoked grants can still identify delivered notifications through login.
    // The keys are removed when the owning connection is removed or changed.
    var retiredKeys: [String]?

    static func credential() -> String {
        SymmetricKey(size: .bits256).withUnsafeBytes { Data($0).base64EncodedString() }
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}

struct NativeSubscription: Codable, Sendable {
    let id: String
    let installationId: String
    let label: String
    let backendName: String
    let sms: Bool
    let reminders: Bool
    let enabled: Bool
    var calls: Bool?
}

struct NativePushOverview: Decodable, Sendable {
    let backendId: String
    let revision: Int64
    let unreadCount: Int
    let subscriptions: [NativeSubscription]
}

struct NativePushRegistration: Encodable, Sendable {
    let id: String
    let installationId: String
    let label: String
    let backendName: String
    let sendToken: String
    let encryptionKey: String
    let sms: Bool
    let reminders: Bool
    let enabled: Bool
    var calls = false
}

struct GatewayRegistration: Decodable, Sendable {
    let id: String
    let count: Int
    let revision: Int64
    var backends: [MessageBadge]?
}

struct PushNavigation: Identifiable, Sendable {
    let id = UUID()
    let content: PushContent
}

struct PushConversation: Equatable, Sendable {
    let backendID: String
    let conversationID: String
}

struct PushThread: Equatable, Sendable {
    let backendID: String
    let participant: String
    let conversationIDs: Set<String>
}

import CryptoKit
import Foundation

struct PushEnvelope: Codable, Sendable {
    let version: Int
    let subscriptionId: String
    let eventId: String
    let nonce: String
    let ciphertext: String
}

struct PushContent: Codable, Equatable, Sendable {
    let kind: String
    let backendId: String
    let backendName: String
    var conversationId: String?
    var messageId: Int64?
    var modemId: String?
    var modemName: String?
    var profileId: String?
    var profileType: String?
    var profileName: String?
    var seId: String?
    var sender: String?
    var text: String?
    var callId: String?
    var callUUID: String?
    var expiresAt: Int64?
}

enum PushPayloadError: Error { case invalidEnvelope, missingKey }

struct PushDecryptionKey: Codable, Sendable {
    let backendID: String
    let key: Data
    var allowsPreview: Bool?
}

enum PushPayload {
    static func decrypt(_ envelope: PushEnvelope, key: Data) throws -> PushContent {
        guard envelope.version == 1, key.count == 32,
              !envelope.subscriptionId.isEmpty, !envelope.eventId.isEmpty,
              let nonce = Data(base64Encoded: envelope.nonce), nonce.count == 12,
              let sealed = Data(base64Encoded: envelope.ciphertext), sealed.count >= 16,
              sealed.count <= 1916 else { throw PushPayloadError.invalidEnvelope }
        let aad = Data("sigmo-push:v1:\(envelope.subscriptionId):\(envelope.eventId)".utf8)
        let box = try AES.GCM.SealedBox(nonce: AES.GCM.Nonce(data: nonce), ciphertext: sealed.dropLast(16), tag: sealed.suffix(16))
        let plain = try AES.GCM.open(box, using: SymmetricKey(data: key), authenticating: aad)
        let content = try JSONDecoder().decode(PushContent.self, from: plain)
        guard ["sms", "reminder", "call"].contains(content.kind), !content.backendId.isEmpty else { throw PushPayloadError.invalidEnvelope }
        if content.kind == "call" {
            guard content.callUUID.flatMap(UUID.init(uuidString:)) != nil,
                  content.callId?.isEmpty == false, content.modemId?.isEmpty == false,
                  content.profileId?.isEmpty == false, content.expiresAt != nil else { throw PushPayloadError.invalidEnvelope }
        }
        return content
    }

    static func decode(_ data: Data, forPreview: Bool = false) throws -> PushContent {
        let envelope = try JSONDecoder().decode(PushEnvelope.self, from: data)
        guard let data = try PushKeychain.read("key-" + envelope.subscriptionId, shared: true) else { throw PushPayloadError.missingKey }
        let record = try JSONDecoder().decode(PushDecryptionKey.self, from: data)
        return try decrypt(envelope, record: record, forPreview: forPreview)
    }

    static func decrypt(_ envelope: PushEnvelope, record: PushDecryptionKey, forPreview: Bool = false) throws -> PushContent {
        guard !forPreview || record.allowsPreview != false else { throw PushPayloadError.missingKey }
        let content = try decrypt(envelope, key: record.key)
        guard content.backendId == record.backendID else { throw PushPayloadError.invalidEnvelope }
        return content
    }
}

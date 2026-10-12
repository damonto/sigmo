import XCTest
import UserNotifications
@testable import Sigmo

final class PushPayloadTests: XCTestCase {
    private struct Vector: Decodable {
        let key: Data
        let envelope: PushEnvelope
        let content: PushContent
    }

    private func vector() throws -> Vector {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "native_push", withExtension: "json"))
        return try JSONDecoder().decode(Vector.self, from: Data(contentsOf: url))
    }

    func testGoAndWebCryptoFixtureDecrypts() throws {
        let vector = try vector()
        XCTAssertEqual(try PushPayload.decrypt(vector.envelope, key: vector.key), vector.content)
    }

    func testChangedEventSubscriptionNonceCiphertextAndTruncationFail() throws {
        let vector = try vector()
        let envelope = vector.envelope
        for field in ["eventId", "subscriptionId", "nonce", "ciphertext", "truncated", "version"] {
            let changed = PushEnvelope(version: field == "version" ? 2 : 1,
                subscriptionId: envelope.subscriptionId + (field == "subscriptionId" ? "x" : ""),
                eventId: envelope.eventId + (field == "eventId" ? "x" : ""),
                nonce: field == "nonce" ? Data(repeating: 0, count: 12).base64EncodedString() : envelope.nonce,
                ciphertext: field == "truncated" ? "YQ==" : field == "ciphertext" ? "AAAA" + envelope.ciphertext.dropFirst(4) : envelope.ciphertext)
            XCTAssertThrowsError(try PushPayload.decrypt(changed, key: vector.key), field)
        }
        XCTAssertThrowsError(try PushPayload.decrypt(envelope, key: Data(repeating: 0, count: 16)))
    }

    @MainActor
    func testKeychainRecoveryRetainsInstallationAndPendingNavigation() throws {
        let configuration = try testConfiguration()
        let identity = PushIdentity(id: "installation", managementToken: "management-credential",
            connections: [PushConnection(configurationID: configuration.id, backendID: "backend-one", address: configuration.address)])
        let memory = PushMemoryPersistence()
        memory.values["installation"] = try JSONEncoder().encode(identity)
        memory.unavailable = true
        let push = NativePushState(persistence: memory.persistence)
        XCTAssertFalse(push.hasIdentity)
        XCTAssertEqual(push.failure?.code, "push.keychainUnavailable")
        let content = PushContent(kind: "sms", backendId: "backend-one", backendName: "Home", conversationId: "conversation")
        XCTAssertFalse(push.receive(content, tapped: true))
        XCTAssertEqual(push.pendingNavigation?.content, content)
        XCTAssertNil(push.navigationFailure)
        memory.unavailable = false
        push.retry()
        XCTAssertTrue(push.hasIdentity)
        XCTAssertNil(push.failure)
        XCTAssertEqual(push.backendID(sessionID: configuration.id), "backend-one")
        XCTAssertEqual(push.pendingNavigation?.content, content)
        XCTAssertEqual(try memory.identity(), identity)
        XCTAssertEqual(memory.writes, 0)
    }

    @MainActor
    func testCorruptInstallationIsNotReplacedWithNewCredentials() {
        let memory = PushMemoryPersistence()
        memory.values["installation"] = Data("corrupt".utf8)
        let push = NativePushState(persistence: memory.persistence)
        push.retry()
        XCTAssertFalse(push.hasIdentity)
        XCTAssertNotNil(push.failure)
        XCTAssertEqual(memory.writes, 0)
    }

    @MainActor
    func testLogoutDisablesPreviewsAndRetainsNavigationUntilConnectionRemoval() throws {
        let vector = try vector()
        let configuration = try testConfiguration()
        let file = ConfigurationFile(url: URL.temporaryDirectory.appending(path: "PushLifecycle-\(UUID()).json"))
        addTeardownBlock { try FileManager.default.removeItem(at: file.url) }
        try file.save(BackendSettings(backends: [configuration]))
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        let binding = PushBinding(id: vector.envelope.subscriptionId, backendID: vector.content.backendId, sendToken: "delivery-credential")
        let identity = PushIdentity(id: "installation", managementToken: "management-credential", bindings: [binding],
            connections: [PushConnection(configurationID: configuration.id, backendID: vector.content.backendId, address: configuration.address)])
        let memory = PushMemoryPersistence()
        let account = "key-" + binding.id
        memory.values["installation"] = try JSONEncoder().encode(identity)
        memory.values[account] = try JSONEncoder().encode(PushDecryptionKey(backendID: vector.content.backendId, key: vector.key))
        let push = NativePushState(disabled: true, identity: identity, persistence: memory.persistence)
        store.sessions[configuration.id]?.logout()
        memory.rejectedWrite = account
        XCTAssertThrowsError(try push.removeInvalidConnections(store))
        XCTAssertEqual(try memory.identity().revocations, [binding.id])
        XCTAssertEqual(try memory.identity().retiredKeys, [binding.id])
        XCTAssertNil(push.binding(sessionID: configuration.id))
        memory.rejectedWrite = nil
        try push.removeInvalidConnections(store)
        let key = try JSONDecoder().decode(PushDecryptionKey.self, from: XCTUnwrap(memory.values[account]))
        XCTAssertEqual(key.allowsPreview, false)
        XCTAssertThrowsError(try PushPayload.decrypt(vector.envelope, record: key, forPreview: true))
        let content = try PushPayload.decrypt(vector.envelope, record: key)
        XCTAssertEqual(content, vector.content)
        XCTAssertFalse(push.receive(content, tapped: true))
        XCTAssertEqual(push.connection(for: content, in: store)?.id, configuration.id)
        XCTAssertEqual(push.pendingNavigation?.content, content)
        store.deleteBackend(configuration.id)
        try push.removeInvalidConnections(store)
        XCTAssertNil(memory.values[account])
        XCTAssertTrue(try memory.identity().retiredKeys?.isEmpty == true)
        XCTAssertFalse(push.receive(content, tapped: true))
        XCTAssertEqual(push.navigationFailure?.code, "push.connectionRemoved")
    }

    @MainActor
    func testNotificationResponseCompletesOnMainThread() async throws {
        let content = PushContent(kind: "sms", backendId: "backend-one", backendName: "Home", conversationId: "conversation", modemId: "modem")
        let scenarios: [(String, String, PushContent?)] = [
            ("tap", UNNotificationDefaultActionIdentifier, content),
            ("unreadable payload", UNNotificationDefaultActionIdentifier, nil),
            ("dismiss", UNNotificationDismissActionIdentifier, content),
        ]
        for (name, action, payload) in scenarios {
            let push = NativePushState(disabled: true)
            let completed = expectation(description: name)
            completed.assertForOverFulfill = true
            await Task.detached {
                push.receiveResponse(payload, action: action) {
                    XCTAssertTrue(Thread.isMainThread, "\(name) must complete on the main thread")
                    completed.fulfill()
                }
            }.value
            await fulfillment(of: [completed], timeout: 2)
            if action != UNNotificationDefaultActionIdentifier {
                XCTAssertNil(push.pendingNavigation)
                XCTAssertNil(push.navigationFailure)
            } else if payload == nil {
                XCTAssertEqual(push.navigationFailure?.code, "push.unavailableNotification")
            } else {
                XCTAssertEqual(push.pendingNavigation?.content, content)
            }
        }
    }

    @MainActor
    func testNotificationWaitsForTheActiveScene() throws {
        let configuration = try testConfiguration()
        let file = ConfigurationFile(url: URL.temporaryDirectory.appending(path: "PushScene-\(UUID()).json"))
        addTeardownBlock { try FileManager.default.removeItem(at: file.url) }
        try file.save(BackendSettings(backends: [configuration]))
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        let identity = PushIdentity(id: "installation", managementToken: "test",
            connections: [PushConnection(configurationID: configuration.id, backendID: "backend-one", address: configuration.address)])
        let push = NativePushState(disabled: true, identity: identity)
        let content = PushContent(kind: "sms", backendId: "backend-one", backendName: "Home", conversationId: "conversation", modemId: "modem")
        _ = push.receive(content, tapped: true)
        XCTAssertNil(push.takeNavigation(in: store, active: false))
        XCTAssertEqual(push.pendingNavigation?.content, content)
        XCTAssertNil(push.navigationFailure)
        XCTAssertEqual(push.takeNavigation(in: store, active: true), .messages(configuration.id, "modem", "conversation"))
        XCTAssertNil(push.pendingNavigation)
        XCTAssertNil(push.takeNavigation(in: store, active: true))
    }

    @MainActor
    func testNotificationNavigationIsScopedToBackendAndSurvivesLogin() throws {
        let first = try testConfiguration("one.example")
        let second = try testConfiguration("two.example")
        let identity = PushIdentity(id: "installation", managementToken: "test",
            bindings: [PushBinding(id: "grant", backendID: "backend-one", sendToken: "test")],
            connections: [PushConnection(configurationID: first.id, backendID: "backend-one", address: first.address),
                          PushConnection(configurationID: second.id, backendID: "backend-two", address: second.address)])
        let push = NativePushState(disabled: true, identity: identity)
        push.visibleConversation = PushConversation(backendID: "backend-one", conversationID: "same-conversation")
        let firstContent = PushContent(kind: "sms", backendId: "backend-one", backendName: "First", conversationId: "same-conversation")
        let otherContent = PushContent(kind: "sms", backendId: "backend-two", backendName: "Second", conversationId: "same-conversation")
        XCTAssertTrue(push.receive(firstContent, tapped: false))
        XCTAssertFalse(push.receive(otherContent, tapped: false))
        XCTAssertFalse(push.receive(otherContent, tapped: true))
        XCTAssertEqual(push.pendingNavigation?.content.backendId, "backend-two")
        // The target stays queued until the root view can navigate through authentication.
        XCTAssertNotNil(push.pendingNavigation)
        _ = push.receive(nil, tapped: true)
        XCTAssertEqual(push.navigationFailure?.code, "push.unavailableNotification")
    }
}

@MainActor
final class PushMemoryPersistence {
    var values: [String: Data] = [:]
    var unavailable = false
    var rejectedWrite: String?
    var writes = 0

    var persistence: PushPersistence {
        PushPersistence(read: { account, _ in
            if self.unavailable { throw PushKeychainError.missingAccessGroup }
            return self.values[account]
        }, write: { account, data, _ in
            if self.unavailable || self.rejectedWrite == account { throw PushKeychainError.missingAccessGroup }
            self.values[account] = data
            self.writes += 1
        })
    }

    func identity() throws -> PushIdentity {
        try JSONDecoder().decode(PushIdentity.self, from: XCTUnwrap(values["installation"]))
    }
}

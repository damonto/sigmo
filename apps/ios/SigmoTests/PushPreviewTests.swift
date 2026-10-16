import UserNotifications
import XCTest
@testable import Sigmo

final class PushPreviewTests: XCTestCase {
    func testSubtitleUsesSharedSettingsForMessagesAndReminders() {
        let singleBackend = PushPreviewSettings(includesBackendName: false)
        for kind in ["sms", "reminder"] {
            var content = PushContent(kind: kind, backendId: "backend", backendName: "Home", modemName: "Mobile",
                profileName: "Work", sender: "10086", text: "Test content")
            let preview = PushPreview(content: content, settings: singleBackend)
            XCTAssertEqual(preview.title, kind == "sms" ? "10086" : String(localized: "push.generic.reminder"))
            XCTAssertEqual(preview.subtitle, "Mobile · Work")
            XCTAssertEqual(preview.body, "Test content")
            XCTAssertEqual(PushPreview(content: content).subtitle, "Home · Mobile · Work")
            content.profileName = " \n "
            content.modemName = " Mobile "
            XCTAssertEqual(PushPreview(content: content, settings: singleBackend).subtitle, "Mobile")
            content.modemName = nil
            XCTAssertEqual(PushPreview(content: content, settings: singleBackend).subtitle, "")
            XCTAssertEqual(PushPreview(content: content).subtitle, "Home")
        }
        let legacy = PushContent(kind: "sms", backendId: "backend", backendName: "Home")
        let preview = PushPreview(content: legacy)
        XCTAssertEqual(preview.title, String(localized: "push.generic.message"))
        XCTAssertEqual(preview.subtitle, "Home")
        XCTAssertEqual(preview.body, "")
        XCTAssertEqual(PushPreview(content: legacy, settings: singleBackend).subtitle, "")
    }

    @MainActor
    func testPreviewSettingsTrackAddedAndRemovedOfflineBackends() async throws {
        let directory = URL.temporaryDirectory.appending(path: "PushPreview-\(UUID())")
        let file = ConfigurationFile(url: directory.appending(path: "backends.json"))
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let first = UUID(), second = UUID(), third = UUID()
        XCTAssertTrue(store.saveBackend(id: first, name: "Home", address: "https://one.example"))
        let memory = PushMemoryPersistence()
        let access = PushNotificationAccess(status: { .denied }, request: {}, register: {}, setBadge: { _ in })
        let persistence = PushPersistence(read: memory.persistence.read, write: { account, data, shared in
            if account == PushPreviewSettings.keychainAccount { XCTAssertTrue(shared) }
            try memory.persistence.write(account, data, shared)
        })
        let push = NativePushState(disabled: true, identity: PushIdentity(id: "installation", managementToken: "credential"),
            persistence: persistence, notificationAccess: access)
        func savedSettings() throws -> PushPreviewSettings {
            let data = try XCTUnwrap(memory.values[PushPreviewSettings.keychainAccount])
            return try JSONDecoder().decode(PushPreviewSettings.self, from: data)
        }
        await push.reconcile(store: store)
        XCTAssertFalse(try savedSettings().includesBackendName)
        XCTAssertTrue(store.saveBackend(id: second, name: "Office", address: "https://two.example"))
        await push.reconcile(store: store)
        XCTAssertTrue(try savedSettings().includesBackendName)
        XCTAssertTrue(store.saveBackend(id: third, name: "Travel", address: "https://three.example"))
        await push.reconcile(store: store)
        store.deleteBackend(second)
        await push.reconcile(store: store)
        XCTAssertTrue(try savedSettings().includesBackendName)
        store.deleteBackend(third)
        await push.reconcile(store: store)
        XCTAssertFalse(try savedSettings().includesBackendName)
    }
}

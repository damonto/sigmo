import XCTest
@testable import Sigmo

@MainActor
final class BackendStorageTests: XCTestCase {
    private var directory: URL!
    private var file: ConfigurationFile!

    override func setUp() async throws {
        directory = URL.temporaryDirectory.appending(path: UUID().uuidString)
        file = ConfigurationFile(url: directory.appending(path: "backends.json"))
    }
    override func tearDown() async throws {
        if FileManager.default.fileExists(atPath: directory.path) { try FileManager.default.removeItem(at: directory) }
    }

    func testBackendOrderRenameAndDeletionPersist() throws {
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        let one = UUID(), two = UUID(), three = UUID()
        store.saveBackend(id: one, name: "One", address: "https://one.example")
        store.saveBackend(id: two, name: "Two", address: "https://two.example")
        store.saveBackend(id: three, name: "Three", address: "https://three.example")
        store.moveBackends(from: IndexSet(integer: 0), to: 3)
        XCTAssertEqual(try file.load().backends.map(\.id), [two, three, one])
        store.saveBackend(id: two, name: "Renamed", address: "https://two.example")
        store.deleteBackend(three)
        let reopened = BackendStore(file: file, credentials: MemoryCredentials().store)
        XCTAssertEqual(reopened.settings, store.settings)
        XCTAssertEqual(reopened.settings.backends.map(\.id), [two, one])
        XCTAssertEqual(reopened.settings.backends[0].name, "Renamed")
    }

    func testCorruptConfigurationIsNeverOverwritten() throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let corrupt = Data("broken JSON".utf8)
        try corrupt.write(to: file.url)
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        XCTAssertFalse(store.configurationLoaded)
        XCTAssertFalse(store.saveBackend(name: "Do not overwrite", address: "https://one.example"))
        XCTAssertEqual(try Data(contentsOf: file.url), corrupt)
        try file.save(BackendSettings())
        store.reloadConfiguration()
        XCTAssertTrue(store.configurationLoaded)
        XCTAssertNil(store.failure)
    }

    func testSaveFailureDoesNotChangeInMemoryConfiguration() throws {
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        store.saveBackend(name: "Original", address: "https://one.example")
        let original = store.settings
        try FileManager.default.removeItem(at: file.url)
        try FileManager.default.createDirectory(at: file.url, withIntermediateDirectories: false)
        XCTAssertFalse(store.saveBackend(name: "Unsaved", address: "https://two.example"))
        XCTAssertEqual(store.settings, original)
        XCTAssertNotNil(store.failure)
    }

    func testFailedAddressChangeAndDeletionPreserveLoginAndPendingWork() async throws {
        for deletes in [false, true] {
            let configurationFile = ConfigurationFile(url: directory.appending(path: "\(deletes).json"))
            let credentials = MemoryCredentials()
            let stub = BackendStub()
            await stub.setRequired(true)
            let store = BackendStore(file: configurationFile, credentials: credentials.store, makeAPI: { configuration in
                BackendAPI(configuration: configuration) { try await stub.respond($0) }
            })
            let id = UUID()
            XCTAssertTrue(store.saveBackend(id: id, name: "Original", address: "https://one.example"))
            credentials.tokens[id] = storedCredential("old-token")
            let session = try XCTUnwrap(store.sessions[id])
            await session.refresh(force: true)
            let original = store.settings
            await stub.holdNextModemRequest()
            let pending = Task { await session.refresh(force: true) }
            await stub.waitForHeldRequest()
            try FileManager.default.removeItem(at: configurationFile.url)
            try FileManager.default.createDirectory(at: configurationFile.url, withIntermediateDirectories: false)

            if deletes { store.deleteBackend(id) }
            else { XCTAssertFalse(store.saveBackend(id: id, name: "Replacement", address: "https://two.example")) }

            XCTAssertEqual(store.settings, original)
            XCTAssertTrue(store.sessions[id] === session)
            XCTAssertEqual(credentials.tokens[id], storedCredential("old-token"))
            XCTAssertEqual(session.authentication, .authenticated)
            XCTAssertTrue(session.isRefreshing)
            XCTAssertNotNil(store.failure)
            await stub.release()
            await pending.value
            XCTAssertTrue(session.isOnline)
        }
    }

    func testCredentialRemovalFailureDoesNotChangeAddressOrLiveSession() async throws {
        let credentials = MemoryCredentials()
        let stub = BackendStub()
        await stub.setRequired(true)
        let store = BackendStore(file: file, credentials: credentials.store, makeAPI: { configuration in
            BackendAPI(configuration: configuration) { try await stub.respond($0) }
        })
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Original", address: "https://one.example"))
        credentials.tokens[id] = storedCredential("old-token")
        let session = try XCTUnwrap(store.sessions[id])
        await session.refresh(force: true)
        let original = store.settings
        credentials.writeFailure = true

        XCTAssertFalse(store.saveBackend(id: id, name: "Replacement", address: "https://two.example"))

        XCTAssertEqual(try file.load(), original)
        XCTAssertEqual(store.settings, original)
        XCTAssertTrue(store.sessions[id] === session)
        XCTAssertTrue(session.isOnline)
        XCTAssertEqual(credentials.tokens[id], storedCredential("old-token"))
    }

    func testUnreadableConfigurationIsNotTreatedAsFirstLaunch() throws {
        let original = BackendSettings(backends: [try testConfiguration()])
        try file.save(original)
        try FileManager.default.setAttributes([.posixPermissions: 0], ofItemAtPath: directory.path)
        defer {
            do { try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path) }
            catch { XCTFail("Restore temporary directory permissions: \(error)") }
        }
        XCTAssertThrowsError(try file.load())
        let store = BackendStore(file: file, credentials: MemoryCredentials().store)
        XCTAssertFalse(store.configurationLoaded)
        XCTAssertNotNil(store.failure)
        XCTAssertFalse(store.saveBackend(name: "Unsaved", address: "https://two.example"))
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
        XCTAssertEqual(try file.load(), original)
    }

    func testAddressChangeDeletesOnlyItsCredentialAndDiscardsOldResult() async throws {
        let credentials = MemoryCredentials()
        let stub = BackendStub()
        let store = BackendStore(file: file, credentials: credentials.store, makeAPI: { config in
            BackendAPI(configuration: config) { try await stub.respond($0) }
        })
        let one = UUID(), two = UUID()
        store.saveBackend(id: one, name: "One", address: "https://one.example")
        store.saveBackend(id: two, name: "Two", address: "https://two.example")
        credentials.tokens = [one: storedCredential("one"), two: storedCredential("two")]
        let old = try XCTUnwrap(store.sessions[one])
        await stub.holdNextModemRequest()
        let oldRequest = Task { await old.refresh(force: true) }
        await stub.waitForHeldRequest()
        XCTAssertTrue(store.saveBackend(id: one, name: "New", address: "https://new.example"))
        await stub.release()
        await oldRequest.value
        XCTAssertNil(credentials.tokens[one])
        XCTAssertEqual(credentials.tokens[two], storedCredential("two"))
        XCTAssertFalse(store.sessions[one] === old)
        XCTAssertNotEqual(store.sessions[one]?.incarnationID, old.incarnationID)
        XCTAssertTrue(try XCTUnwrap(store.sessions[one]).modems.isEmpty)
        XCTAssertTrue(old.modems.isEmpty)
        store.deleteBackend(two)
        XCTAssertNil(credentials.tokens[two])
        XCTAssertNil(store.sessions[two])
    }

    func testConfigurationAndLoginRestoreButDeviceSnapshotDoesNot() async throws {
        let credentials = MemoryCredentials()
        let stub = BackendStub()
        await stub.setRequired(true)
        let factory: (BackendConfiguration) -> BackendAPI = { config in BackendAPI(configuration: config) { try await stub.respond($0) } }
        let first = BackendStore(file: file, credentials: credentials.store, makeAPI: factory)
        first.saveBackend(name: "Saved", address: "https://one.example")
        let id = try XCTUnwrap(first.settings.backends.first?.id)
        credentials.tokens[id] = storedCredential("saved-token")
        await first.refreshAll()
        XCTAssertEqual(first.sessions[id]?.modems.count, 1)
        let second = BackendStore(file: file, credentials: credentials.store, makeAPI: factory)
        XCTAssertTrue(try XCTUnwrap(second.sessions[id]).modems.isEmpty)
        await second.refreshAll()
        XCTAssertEqual(second.sessions[id]?.authentication, .authenticated)
        XCTAssertEqual(second.sessions[id]?.modems.count, 1)
        let json = try String(contentsOf: file.url, encoding: .utf8)
        XCTAssertFalse(json.contains("saved-token"))
        XCTAssertFalse(json.contains("device-1"))
    }

    func testKeychainRoundTripUsesIndependentAccounts() throws {
        let first = UUID(), second = UUID()
        let store = BackendCredentials.keychain
        defer {
            // Unique test accounts are always removed, including after assertion failures.
            try? store.write(first, nil)
            try? store.write(second, nil)
        }
        try store.write(first, "first")
        try store.write(second, "second")
        try store.write(first, "updated")
        XCTAssertEqual(try store.read(first), "updated")
        XCTAssertEqual(try store.read(second), "second")
        try store.write(first, nil)
        XCTAssertNil(try store.read(first))
        XCTAssertEqual(try store.read(second), "second")
    }
}

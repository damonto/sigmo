import XCTest
@testable import Sigmo

@MainActor
final class HomeTests: XCTestCase {
    func testHomeHidesUnavailableConnectionsUntilRefreshRecovers() async throws {
        let directory = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let healthy = BackendStub(), changing = BackendStub()
        let first = UUID(), second = UUID()
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "connections.json")),
                                 credentials: MemoryCredentials().store, makeAPI: { configuration in
            let stub = configuration.id == first ? healthy : changing
            return BackendAPI(configuration: configuration) { try await stub.respond($0) }
        })
        XCTAssertTrue(store.saveBackend(id: first, name: "Home", address: "https://one.example"))
        XCTAssertTrue(store.saveBackend(id: second, name: "Studio", address: "https://two.example"))
        XCTAssertTrue(store.homeDevices(backendID: nil).isEmpty)
        XCTAssertTrue(store.onlineSessions.isEmpty)
        await store.refreshAll()
        let session = try XCTUnwrap(store.sessions[second])
        XCTAssertEqual(store.homeDevices(backendID: nil).map(\.session.id), [first, second])
        XCTAssertEqual(store.onlineSessions.map(\.id), [first, second])

        for status in ["status.offline", "status.unavailable", "status.loginRequired"] {
            switch status {
            case "status.offline": await changing.setOffline(true)
            case "status.unavailable": await changing.setModemStatus(500)
            case "status.loginRequired": await changing.setRequired(true)
            default: await changing.setLicense(authorized: false)
            }
            await session.refresh(force: true)
            XCTAssertEqual(session.statusKey, status)
            if status == "status.offline" || status == "status.unavailable" {
                // A failed refresh preserves the snapshot, but Home must not present it as available.
                XCTAssertFalse(session.modems.isEmpty, status)
                XCTAssertTrue(session.hasLoadedModems, status)
            }
            XCTAssertEqual(store.homeDevices(backendID: nil).map(\.session.id), [first], status)
            XCTAssertTrue(store.homeDevices(backendID: second).isEmpty, status)
            XCTAssertTrue(store.homeDevices(backendID: nil, search: "Studio").isEmpty, status)
            XCTAssertEqual(store.orderedSessions().map(\.id), [first, second], status)
            XCTAssertEqual(store.onlineSessions.map(\.id), [first], status)

            await changing.setOffline(false)
            await changing.setModemStatus(200)
            await changing.setRequired(false)
            await changing.setLicense(authorized: false)
            await store.refreshAll()
            XCTAssertEqual(session.statusKey, "status.online", status)
            XCTAssertEqual(store.homeDevices(backendID: nil).map(\.session.id), [first, second], status)
            XCTAssertEqual(store.homeDevices(backendID: second, search: "Studio").map(\.session.id), [second], status)
            XCTAssertEqual(store.onlineSessions.map(\.id), [first, second], status)
        }
    }

    func testLocalSearchCombinesConnectionFilterAndMakesNoRequests() async throws {
        let directory = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let stub = BackendStub()
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "connections.json")),
                                 credentials: MemoryCredentials().store, makeAPI: { configuration in
            BackendAPI(configuration: configuration) { try await stub.respond($0) }
        })
        let one = UUID(), two = UUID()
        XCTAssertTrue(store.saveBackend(id: one, name: "Hôme", address: "https://one.example"))
        XCTAssertTrue(store.saveBackend(id: two, name: "Studio", address: "https://two.example"))
        await store.refreshAll()
        let requestCount = await stub.allRequests().count
        for (query, expected) in [("  ", [one, two]), ("HOME", [one]), ("example", [one, two]),
                                  ("13800000000", [one, two]), ("138 0000", [one, two]),
                                  ("test modem studio", [two]), ("missing", [])] {
            XCTAssertEqual(store.homeDevices(backendID: nil, search: query).map(\.session.id), expected, query)
        }
        XCTAssertTrue(store.homeDevices(backendID: one, search: "Studio").isEmpty)
        XCTAssertEqual(store.homeDevices(backendID: two, search: "example 13800000000").map(\.session.id), [two])
        XCTAssertTrue(store.saveBackend(id: two, name: "Office", address: "https://two.example"))
        XCTAssertTrue(store.homeDevices(backendID: nil, search: "Studio").isEmpty)
        XCTAssertEqual(store.homeDevices(backendID: nil, search: "office").map(\.session.id), [two])
        let finalRequestCount = await stub.allRequests().count
        XCTAssertEqual(finalRequestCount, requestCount)
    }

    func testMergedCardsFilterOrderRenameAndDelete() async throws {
        let directory = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let stub = BackendStub()
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "connections.json")),
                                 credentials: MemoryCredentials().store, makeAPI: { configuration in
            BackendAPI(configuration: configuration) { try await stub.respond($0) }
        })
        let one = UUID(), two = UUID()
        XCTAssertTrue(store.saveBackend(id: one, name: "Home", address: "https://one.example"))
        XCTAssertTrue(store.saveBackend(id: two, name: "Studio", address: "https://two.example"))
        await store.refreshAll()
        let cards = store.homeDevices(backendID: nil)
        XCTAssertEqual(cards.map(\.modem.id), ["device-1", "device-1"])
        XCTAssertEqual(Set(cards.map(\.id)).count, 2)
        XCTAssertEqual(cards.map(\.session.id), [one, two])
        XCTAssertEqual(store.homeDevices(backendID: two).map(\.id), [.modem(two, "device-1")])
        store.moveBackends(from: IndexSet(integer: 0), to: 2)
        XCTAssertEqual(store.homeDevices(backendID: nil).map(\.session.id), [two, one])
        let session = try XCTUnwrap(store.sessions[two])
        XCTAssertTrue(store.saveBackend(id: two, name: "Renamed", address: "https://two.example"))
        XCTAssertTrue(store.homeDevices(backendID: two).first?.session === session)
        XCTAssertEqual(store.homeDevices(backendID: two).first?.session.configuration.name, "Renamed")
        store.deleteBackend(two)
        XCTAssertTrue(store.homeDevices(backendID: two).isEmpty)
        XCTAssertEqual(store.homeDevices(backendID: nil).map(\.session.id), [one])
    }

    func testUnreadHistoryReadMarkDeleteAndSearchStayConsistent() async throws {
        let fixture = MessagesFixture()
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: configuration) { try await fixture.respond($0) })
        await session.refresh(force: true)
        XCTAssertEqual(session.unreadCounts["device-1"], 2)
        let messages = MessagesState(session: session, modemID: "device-1")
        await messages.load()
        let active = try XCTUnwrap(messages.items.first { $0.id == "conversation-active" })
        let inactive = try XCTUnwrap(messages.items.first { $0.id == "conversation-inactive" })
        await messages.mark(inactive, unread: true)
        XCTAssertEqual(session.unreadCounts["device-1"], 3)
        messages.search = "missing"
        await messages.load()
        XCTAssertTrue(messages.items.isEmpty)
        XCTAssertEqual(session.unreadCounts["device-1"], 3)
        let thread = messages.thread(active)
        await thread.markRead(through: 2)
        XCTAssertEqual(session.unreadCounts["device-1"], 1)
        let deleted = await messages.delete(inactive)
        XCTAssertTrue(deleted)
        XCTAssertEqual(session.unreadCounts["device-1"], 0)
    }

    func testUnreadAggregationIgnoresUnassignedAndRemovedDevices() async throws {
        let stub = BackendStub()
        await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 2), ("device-1", 3), ("", 11), ("removed", 7)]))
        let session = try testSession(stub)
        await session.refresh(force: true)
        XCTAssertEqual(session.unreadCounts, ["device-1": 5])
        let requests = await stub.allRequests().filter { $0.url?.path.hasSuffix("/conversations") == true }
        XCTAssertEqual(requests.count, 1)
        XCTAssertNil(requests.first?.url?.query)
        await stub.setModemStatus(404)
        await session.refreshModem("device-1")
        XCTAssertTrue(session.unreadCounts.isEmpty)
        await session.refreshUnreadCounts()
        XCTAssertTrue(session.unreadCounts.isEmpty)
    }

    func testUnreadCountsRemainIndependentAcrossConnections() async throws {
        let first = BackendStub(), second = BackendStub()
        await first.setConversations(try snapshot(revision: 1, counts: [("device-1", 2)]))
        await second.setConversations(try snapshot(revision: 9, counts: [("device-1", 8)]))
        let one = try testSession(first), two = try testSession(second)
        await one.refresh(force: true)
        await two.refresh(force: true)
        XCTAssertEqual(one.unreadCounts["device-1"], 2)
        XCTAssertEqual(two.unreadCounts["device-1"], 8)
        await one.logout()
        XCTAssertTrue(one.unreadCounts.isEmpty)
        XCTAssertEqual(two.unreadCounts["device-1"], 8)
    }

    func testOlderUnreadResponseCannotRestoreAcknowledgedCounts() async throws {
        let stub = BackendStub()
        await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 8)]))
        let session = try testSession(stub)
        await session.refresh(force: true)
        await stub.holdNextConversationRequest()
        let old = Task { await session.refreshUnreadCounts() }
        await stub.waitForHeldRequest()
        await stub.setConversations(try snapshot(revision: 2, counts: [("device-1", 1)]))
        await session.refreshUnreadCounts(minimumRevision: 2)
        await stub.release()
        await old.value
        XCTAssertEqual(session.unreadCounts["device-1"], 1)
        // Even the newest request can reach a stale replica after an acknowledged write.
        await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 8)]))
        await session.refreshUnreadCounts()
        XCTAssertEqual(session.unreadCounts["device-1"], 1)
    }

    func testUnreadResponsesAfterLogoutAndStopAreDiscarded() async throws {
        for logout in [false, true] {
            let stub = BackendStub()
            await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 2)]))
            let session = try testSession(stub)
            await session.refresh(force: true)
            await stub.setConversations(try snapshot(revision: 2, counts: [("device-1", 9)]))
            await stub.holdNextConversationRequest()
            let pending = Task { await session.refreshUnreadCounts() }
            await stub.waitForHeldRequest()
            if logout { await session.logout() } else { session.stop() }
            await stub.release()
            await pending.value
            XCTAssertEqual(session.unreadCounts["device-1"], logout ? nil : 2)
        }
    }

    func testUnreadFailuresPreserveCountsUntilAccessIsRevoked() async throws {
        for status in [500, 404, 401] {
            let stub = BackendStub()
            await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 3)]))
            let session = try testSession(stub)
            await session.refresh(force: true)
            await stub.setConversations("", status: status)
            await session.refreshUnreadCounts()
            XCTAssertEqual(session.unreadCounts["device-1"], status == 401 ? nil : 3)
            XCTAssertEqual(session.hasLoadedModems, status != 401)
            XCTAssertNil(session.failure)
        }
    }

    func testAddressChangeDiscardsUnreadSnapshotAndLateResponse() async throws {
        let directory = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let stub = BackendStub()
        await stub.setConversations(try snapshot(revision: 1, counts: [("device-1", 3)]))
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "connections.json")),
                                 credentials: MemoryCredentials().store, makeAPI: { configuration in
            BackendAPI(configuration: configuration) { try await stub.respond($0) }
        })
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Home", address: "https://one.example"))
        await store.refreshAll()
        let old = try XCTUnwrap(store.sessions[id])
        XCTAssertEqual(old.unreadCounts["device-1"], 3)
        await stub.holdNextConversationRequest()
        let pending = Task { await old.refreshUnreadCounts() }
        await stub.waitForHeldRequest()
        XCTAssertTrue(store.saveBackend(id: id, name: "Moved", address: "https://two.example"))
        await stub.release()
        await pending.value
        XCTAssertTrue(old.unreadCounts.isEmpty)
        XCTAssertTrue(try XCTUnwrap(store.sessions[id]).unreadCounts.isEmpty)
    }

    private func snapshot(revision: Int64, counts: [(String, Int)]) throws -> String {
        struct Snapshot: Encodable {
            let backendId = "test-backend"
            let revision: Int64
            let unreadCount: Int
            let items: [SMSConversation]
        }
        let items = counts.enumerated().map { index, entry in
            SMSConversation(id: "conversation-\(index)", profileId: "profile-\(index)", participant: "10086", modemId: entry.0,
                            lineLabel: "", lineNumber: "", readThroughMessageId: 0, markedUnread: false, unreadCount: entry.1,
                            latestMessage: SMSMessage(id: Int64(index + 1), sender: "10086", recipient: "", text: "Test",
                                                      timestamp: Date(timeIntervalSince1970: 1_790_000_000), status: "received-unread", incoming: true),
                            canSend: true)
        }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return String(decoding: try encoder.encode(Snapshot(revision: revision, unreadCount: counts.reduce(0) { $0 + $1.1 }, items: items)), as: UTF8.self)
    }
}

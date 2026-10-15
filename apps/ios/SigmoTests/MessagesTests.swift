import XCTest
@testable import Sigmo

@MainActor
final class MessagesTests: XCTestCase {
    func testConversationDisplayFormatsNumbersAndPreservesSenderNames() {
        let message = SMSMessage(id: 1, sender: "+12025550123", recipient: "+12025550124", text: "hello",
            timestamp: .now, status: "received", incoming: true)
        let cases: [(participant: String, label: String, region: String?, expectedParticipant: String, expectedLine: String)] = [
            ("+12025550123", "", "US", "+1 (202) 555-0123", "+1 (202) 555-0124"),
            ("+12025550123", "", nil, "+1 (202) 555-0123", "+1 (202) 555-0124"),
            ("BANK123", "Work", "US", "BANK123", "Work"),
            ("10086", "Travel", "CN", "10086", "Travel"),
            ("", "Work", nil, String(localized: "messages.unknownSender"), "Work"),
        ]
        for item in cases {
            let conversation = SMSConversation(id: "conversation", profileId: "sim", participant: item.participant,
                modemId: "modem", lineLabel: item.label, lineNumber: "+12025550124", readThroughMessageId: 0,
                markedUnread: false, unreadCount: 0, latestMessage: message, canSend: true)
            XCTAssertEqual(conversation.participantLabel(regionCode: item.region), item.expectedParticipant)
            XCTAssertEqual(conversation.lineDescription(regionCode: item.region), item.expectedLine)
            XCTAssertEqual(conversation.participant, item.participant)
            XCTAssertEqual(conversation.lineNumber, "+12025550124")
        }
    }

    private func session(_ fixture: MessagesFixture) async throws -> BackendSession {
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration) { try await fixture.respond($0) })
        await session.refresh(force: true)
        XCTAssertTrue(session.canAccessDevices)
        return session
    }

    func testHistorySearchReadAndSIMIsolation() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let messages = MessagesState(session: session, modemID: "device-1")
        await messages.load()
        XCTAssertEqual(messages.items.count, 2)
        XCTAssertEqual(messages.unreadCount, 2)
        let active = try XCTUnwrap(messages.items.first { $0.id == "conversation-active" })
        let inactive = try XCTUnwrap(messages.items.first { $0.id == "conversation-inactive" })
        let first = messages.thread(active), second = messages.thread(inactive)
        first.draft = "first draft"; second.draft = "second draft"
        XCTAssertEqual(messages.thread(active).draft, "first draft")
        XCTAssertFalse(second.canSend)
        await second.load()
        XCTAssertEqual(second.messages.count, 1)
        await first.load()
        await first.markRead(through: 1)
        await messages.load()
        XCTAssertEqual(messages.unreadCount, 1)
        await messages.mark(active, unread: true)
        XCTAssertTrue(messages.items.first { $0.id == active.id }?.markedUnread == true)
        messages.search = "Balance"
        await messages.load()
        XCTAssertEqual(messages.items.map(\.id), [active.id])
        messages.search = ""; messages.historical = true
        await messages.load()
        XCTAssertEqual(messages.items.map(\.id), ["conversation-history"])
    }

    func testDeviceHistoryPagesWithoutFetchingAllConversations() async throws {
        let fixture = MessagesFixture(unreadMessages: 125), session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let conversation = try XCTUnwrap(list.items.first { $0.id == "conversation-active" })
        let before = await fixture.requestCount(method: "GET", suffix: "/conversations")
        let thread = list.thread(conversation)
        await thread.load()
        XCTAssertEqual(thread.messages.count, 50)
        XCTAssertNil(thread.badge, "Loading history must not acknowledge reads or dismiss notifications")
        XCTAssertNotNil(thread.snapshotBadge)
        XCTAssertFalse(thread.cursor.isEmpty)
        await thread.load(more: true)
        XCTAssertEqual(thread.messages.count, 100)
        await thread.load(more: true)
        XCTAssertEqual(thread.messages.map(\.id), (5...129).map(Int64.init))
        XCTAssertTrue(thread.cursor.isEmpty)
        let after = await fixture.requestCount(method: "GET", suffix: "/conversations")
        XCTAssertEqual(before, after)
    }

    func testCachedThreadRefreshesManualUnreadStateFromItsPage() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let thread = list.thread(try XCTUnwrap(list.items.first { $0.id == "conversation-active" }))
        await thread.load()
        XCTAssertEqual(thread.messages.count, 2)
        XCTAssertTrue(thread.cursor.isEmpty)
        XCTAssertNil(thread.failure)
        await thread.markRead(through: 2)
        await list.mark(thread.conversation, unread: true)
        await thread.load()
        XCTAssertTrue(thread.conversation.markedUnread)
        await thread.markRead(through: 2)
        await list.load()
        XCTAssertFalse(list.items.first { $0.id == thread.conversation.id }?.markedUnread ?? true)
    }

    func testRefreshMakesBackdatedImportsReachableWithoutResettingStablePaging() async throws {
        let fixture = MessagesFixture(unreadMessages: 125), session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let thread = list.thread(try XCTUnwrap(list.items.first { $0.id == "conversation-active" }))
        await thread.load()
        await thread.load(more: true)
        XCTAssertEqual(thread.messages.count, 100)
        let oldCursor = thread.cursor
        await thread.load()
        XCTAssertEqual(thread.cursor, oldCursor, "An unchanged head must preserve older-page progress")

        let imported = await fixture.insertIncoming(timestamp: Date(timeIntervalSince1970: 1_790_000_550))
        await thread.load()
        XCTAssertNotEqual(thread.cursor, oldCursor, "A new snapshot must include backdated arrivals")
        XCTAssertEqual(thread.messages.count, 100)
        let before = await fixture.requestCount(method: "GET", suffix: "/messages")
        await thread.load(more: true)
        XCTAssertNil(thread.failure)
        XCTAssertEqual(thread.messages.count, 126)
        XCTAssertEqual(thread.messages.prefix(3).map(\.id), [5, imported, 6])
        XCTAssertTrue(thread.cursor.isEmpty)
        let after = await fixture.requestCount(method: "GET", suffix: "/messages")
        XCTAssertEqual(after - before, 2, "Skip the retained page and fetch the next unseen records in one action")
        await thread.load()
        XCTAssertTrue(thread.cursor.isEmpty, "Stable polling must not reopen exhausted pagination")
    }

    func testInactiveReplyCanAdoptCurrentSIM() async throws {
        let fixture = MessagesFixture(), session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let historical = try XCTUnwrap(list.items.first { $0.id == "conversation-inactive" })
        let thread = list.thread(historical)
        thread.draft = "Reply using current SIM"
        XCTAssertFalse(thread.canSend)
        XCTAssertTrue(thread.canUseActive)
        thread.useActiveProfile()
        XCTAssertTrue(thread.canSend)
        await thread.send()
        XCTAssertNil(thread.failure)
        XCTAssertTrue(thread.draft.isEmpty)
        let requests = await fixture.requests
        let request = try XCTUnwrap(requests.last { $0.httpMethod == "POST" && $0.url?.path.hasSuffix("/messages") == true })
        let body = try JSONSerialization.jsonObject(with: XCTUnwrap(request.httpBody)) as? [String: String]
        XCTAssertEqual(body?["expectedProfileId"], session.modems.first?.sim.identifier)
        XCTAssertNotEqual(thread.conversation.id, historical.id)
    }

    func testSearchMatchesNumbersAndOlderTextWithExistingFilters() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        let cases: [(query: String, unread: Bool, historical: Bool, ids: [String])] = [
            ("  10086  ", false, false, ["conversation-inactive", "conversation-active"]),
            ("welcome", false, false, ["conversation-active"]),
            ("欢迎", false, false, ["conversation-active"]),
            ("Travel", false, false, ["conversation-inactive"]),
            ("Travel", true, false, []),
            ("10086", true, false, ["conversation-active"]),
            ("Unassigned", false, true, ["conversation-history"]),
            ("missing", false, false, []),
            (" \n ", false, false, ["conversation-inactive", "conversation-active"]),
        ]
        for entry in cases {
            list.search = entry.query
            list.unreadOnly = entry.unread
            list.historical = entry.historical
            await list.load()
            XCTAssertEqual(list.visibleItems.map(\.id), entry.ids, entry.query)
            XCTAssertNil(list.failure)
            XCTAssertEqual(session.unreadCounts["device-1"], 2, "Search must preserve the modem badge")
            let requests = await fixture.requests
            let request = try XCTUnwrap(requests.last)
            let query = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false)).queryItems ?? []
            XCTAssertEqual(query.first { $0.name == "q" }?.value, list.searchQuery.isEmpty ? nil : list.searchQuery)
            XCTAssertEqual(query.first { $0.name == "modemId" }?.value, entry.historical ? nil : "device-1")
            XCTAssertEqual(query.first { $0.name == "unassigned" }?.value, entry.historical ? "true" : nil)
        }
    }

    func testSearchPreservesSpecialCharactersInRequest() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        list.search = "  +86 & 欢迎?  "
        await list.load()
        let requests = await fixture.requests
        let request = try XCTUnwrap(requests.last)
        let query = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false)).queryItems
        XCTAssertEqual(query?.first { $0.name == "q" }?.value, "+86 & 欢迎?")
    }

    func testSearchDiscardsOldResponseDuringDebounceAndAfterNewResponse() async throws {
        for loadNewQueryFirst in [false, true] {
            let fixture = MessagesFixture()
            let session = try await session(fixture)
            let list = MessagesState(session: session, modemID: "device-1")
            await list.load()
            let original = list.items
            list.search = "Balance"
            await fixture.holdNext("/conversations")
            let oldRequest = Task { await list.load() }
            await fixture.waitForHeld()
            list.search = "Travel"
            if loadNewQueryFirst { await list.load() }
            await fixture.release()
            await oldRequest.value
            if loadNewQueryFirst {
                XCTAssertEqual(list.items.map(\.id), ["conversation-inactive"])
            } else {
                XCTAssertEqual(list.items, original, "A response for old input must not replace the snapshot during debounce")
                await list.load()
                XCTAssertEqual(list.items.map(\.id), ["conversation-inactive"])
            }
            XCTAssertFalse(list.loading)
            XCTAssertNil(list.failure)
        }
    }

    func testSearchCancellationKeepsLastSnapshot() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let original = list.items
        list.search = "Balance"
        await fixture.holdNext("/conversations")
        let request = Task { await list.load() }
        await fixture.waitForHeld()
        request.cancel()
        await fixture.release()
        await request.value
        XCTAssertEqual(list.items, original)
        XCTAssertFalse(list.loading)
        XCTAssertNil(list.failure)
    }

    func testConversationFailurePreservesSnapshotAndSurfacesError() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let original = list.items
        XCTAssertFalse(original.isEmpty)
        for status in [404, 500] {
            await fixture.setConversationStatus(status)
            await list.load()
            XCTAssertEqual(list.items, original)
            XCTAssertEqual(list.failure?.status, status)
            XCTAssertFalse(list.loading)
        }
        let oldEndpointReads = await fixture.requestCount(method: "GET", suffix: "/modems/device-1/messages")
        XCTAssertEqual(oldEndpointReads, 0)
        await fixture.setConversationStatus(200)
        await list.load()
        XCTAssertNil(list.failure)
    }

    func testTimeoutRetainsDraftAndDoesNotResendOnRefresh() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let thread = list.thread(try XCTUnwrap(list.items.first { $0.id == "conversation-active" }))
        thread.draft = "timeout draft"
        await fixture.setSendStatus(408)
        await thread.send()
        await thread.load()
        XCTAssertEqual(thread.draft, "timeout draft")
        XCTAssertEqual(thread.failure?.code, "messages.resultUnknown")
        let count = await fixture.requestCount(method: "POST", suffix: "/messages")
        XCTAssertEqual(count, 1)
        await fixture.setSendStatus(409)
        await thread.send()
        XCTAssertEqual(thread.draft, "timeout draft")
        XCTAssertEqual(thread.failure?.status, 409)
    }

    func testLogoutDiscardsLateResponsesAndDrafts() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await fixture.holdNext("/conversations")
        let load = Task { await list.load() }
        await fixture.waitForHeld()
        list.newDraft = "private draft"
        list.search = "private query"
        await session.logout(); list.clear()
        await fixture.release(); await load.value
        XCTAssertTrue(list.items.isEmpty)
        XCTAssertTrue(list.newDraft.isEmpty)
        XCTAssertTrue(list.search.isEmpty)
    }

    func testClearedConversationsDiscardLateMarkAndDeleteResults() async throws {
        for deleting in [false, true] {
            let fixture = MessagesFixture()
            let session = try await session(fixture)
            let list = MessagesState(session: session, modemID: "device-1")
            await list.load()
            let conversation = try XCTUnwrap(list.items.first)
            let readsBeforeAction = await fixture.requestCount(method: "GET", suffix: "/conversations")
            await fixture.holdNext("/conversations/" + conversation.id)
            let operation = Task {
                if deleting { _ = await list.delete(conversation) }
                else { await list.mark(conversation, unread: true) }
            }
            await fixture.waitForHeld()

            list.clear()
            await fixture.release()
            await operation.value

            XCTAssertTrue(list.items.isEmpty)
            XCTAssertNil(list.badge)
            XCTAssertFalse(list.loaded)
            XCTAssertNil(list.failure)
            let reads = await fixture.requestCount(method: "GET", suffix: "/conversations")
            XCTAssertEqual(reads, readsBeforeAction, "An obsolete action must not reload a cleared page")
        }
    }

    func testServerTimeoutRetainsNewAndExistingDraftsWithoutResending() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        await fixture.setSendStatus(504)
        let thread = list.thread(try XCTUnwrap(list.items.first { $0.id == "conversation-active" }))
        thread.draft = "Server timed out"
        await thread.send()
        await thread.load()
        XCTAssertEqual(thread.draft, "Server timed out")
        XCTAssertEqual(thread.failure?.code, "messages.resultUnknown")
        list.newRecipient = "10086"
        list.newDraft = "New conversation timeout"
        let result = await list.sendNew()
        XCTAssertNil(result)
        await list.load()
        XCTAssertEqual(list.newDraft, "New conversation timeout")
        XCTAssertEqual(list.failure?.code, "messages.resultUnknown")
        let submissions = await fixture.requestCount(method: "POST", suffix: "/messages")
        XCTAssertEqual(submissions, 2)
    }

    func testMalformedSuccessResponseLeavesBothSendResultsUnknown() async throws {
        let fixture = MessagesFixture(), configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration) { request in
                let response = try await fixture.respond(request)
                if request.httpMethod == "POST", request.url?.path.hasSuffix("/messages") == true {
                    return BackendHTTPResponse(data: Data("truncated response".utf8), status: 200)
                }
                return response
            })
        await session.refresh(force: true)
        let list = MessagesState(session: session, modemID: "device-1")
        await list.load()
        let thread = list.thread(try XCTUnwrap(list.items.first { $0.id == "conversation-active" }))
        thread.draft = "Existing conversation"
        await thread.send()
        XCTAssertEqual(thread.failure?.code, "messages.resultUnknown")
        XCTAssertEqual(thread.draft, "Existing conversation")
        list.newRecipient = "10086"
        list.newDraft = "New conversation"
        let result = await list.sendNew()
        XCTAssertNil(result)
        XCTAssertEqual(list.failure?.code, "messages.resultUnknown")
        XCTAssertEqual(list.newDraft, "New conversation")
        let submissions = await fixture.requestCount(method: "POST", suffix: "/messages")
        XCTAssertEqual(submissions, 2)
    }

    func testNewMessageIncludesExpectedSIMAndClearsOnlyAfterSuccess() async throws {
        let fixture = MessagesFixture()
        let session = try await session(fixture)
        let list = MessagesState(session: session, modemID: "device-1")
        list.newRecipient = "10086"; list.newDraft = "hello"
        let conversation = await list.sendNew()
        XCTAssertNotNil(conversation)
        XCTAssertTrue(list.newDraft.isEmpty)
        let requests = await fixture.requests
        let sent = try XCTUnwrap(requests.first { $0.httpMethod == "POST" })
        struct Body: Decodable { let expectedProfileId: String }
        XCTAssertEqual(try JSONDecoder().decode(Body.self, from: XCTUnwrap(sent.httpBody)).expectedProfileId, "8986000000000000000")
    }
}

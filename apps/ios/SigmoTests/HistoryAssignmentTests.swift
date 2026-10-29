import XCTest
@testable import Sigmo

@MainActor
final class HistoryAssignmentTests: XCTestCase {
    private func make(_ server: CommunicationFixture) async throws -> HistoryAssignmentState {
        let config = BackendConfiguration(name: "Backend", address: try BackendAddress.normalize("https://history.example"))
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: config, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        return HistoryAssignmentState(session: session, sourceID: "deleted")
    }

    private var source: CommunicationLine {
        CommunicationFixture.line("deleted", available: false, deletionState: "deleted", present: false, messageCount: 2)
    }

    func testOnlyDeletedLinesWithHistoryNeedAssignment() {
        let cases: [(state: String, messages: Int, calls: Int, expected: Bool)] = [
            ("deleted", 0, 0, false), ("deleted", 1, 0, true), ("deleted", 0, 1, true),
            ("", 1, 1, false), ("pending", 1, 1, false), ("assigned", 1, 1, false)
        ]
        for item in cases {
            let line = CommunicationFixture.line("source", deletionState: item.state,
                                                 messageCount: item.messages, callCount: item.calls)
            XCTAssertEqual(line.hasAssignableHistory, item.expected, "\(item)")
        }
    }

    func testEmptyHistoryNeverOffersAssignmentTargets() async throws {
        let server = CommunicationFixture(historyAssignment: .noHistory)
        let state = try await make(server)
        await state.load()

        XCTAssertNil(state.source)
        XCTAssertTrue(state.candidates.isEmpty)
        XCTAssertFalse(state.canSubmit)
        let assigned = await state.assign()
        XCTAssertFalse(assigned)
        let requests = await server.allRequests()
        XCTAssertFalse(requests.contains { $0.httpMethod == "PUT" })
    }

    func testCallHistoryAloneAllowsAssignment() async throws {
        let server = CommunicationFixture()
        await server.setLines([
            CommunicationFixture.line("deleted", available: false, deletionState: "deleted", present: false, callCount: 1),
            CommunicationFixture.line("work")
        ])
        let state = try await make(server)
        await state.load()

        XCTAssertEqual(state.source?.callCount, 1)
        XCTAssertEqual(state.selectedID, "work")
        XCTAssertTrue(state.canSubmit)
    }

    func testCandidatesStayOnSourceModemAndKeepInactiveLinesSelectable() async throws {
        let server = CommunicationFixture()
        await server.setLines([
            source,
            CommunicationFixture.line("inactive", available: false),
            CommunicationFixture.line("foreign", modemID: "device-2"),
            CommunicationFixture.line("absent", present: false),
            CommunicationFixture.line("pending", deletionState: "pending"),
            CommunicationFixture.line("assigned", deletionState: "assigned"),
            CommunicationFixture.line("deleted-again", deletionState: "deleted"),
            CommunicationFixture.line("active")
        ])
        let state = try await make(server)
        await state.load()
        _ = try await state.session.communicationLines()
        XCTAssertEqual(state.candidates.map(\.id), ["active", "inactive"])
        XCTAssertTrue(state.selectedID.isEmpty)
        XCTAssertFalse(state.canSubmit)

        state.selectedID = "foreign"
        let invalid = await state.assign()
        XCTAssertFalse(invalid)
        let initialRequests = await server.allRequests()
        XCTAssertFalse(initialRequests.contains { $0.httpMethod == "PUT" })

        state.selectedID = "inactive"
        XCTAssertTrue(state.canSubmit)
        let assigned = await state.assign()
        XCTAssertTrue(assigned)
        let requests = await server.allRequests()
        let request = try XCTUnwrap(requests.last { $0.httpMethod == "PUT" })
        XCTAssertTrue(request.url?.path.hasSuffix("/sim-profiles/deleted/history-assignment") == true)
        let body = try JSONSerialization.jsonObject(with: XCTUnwrap(request.httpBody)) as? [String: String]
        XCTAssertEqual(body, ["targetProfileId": "inactive"])
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/activation") == true })
        let lines = try await state.session.communicationLines()
        let archived = try XCTUnwrap(lines.first { $0.id == "deleted" })
        XCTAssertEqual(archived.deletionState, "assigned")
        XCTAssertEqual(archived.successorId, "inactive")
    }

    func testOnlyCandidateIsSelectedButRequiresConfirmation() async throws {
        let server = CommunicationFixture()
        await server.setLines([source, CommunicationFixture.line("inactive", available: false)])
        let state = try await make(server)
        await state.load()
        XCTAssertEqual(state.selectedID, "inactive")
        XCTAssertTrue(state.canSubmit)
        let requests = await server.allRequests()
        XCTAssertFalse(requests.contains { $0.httpMethod == "PUT" })
    }

    func testUnavailableSourceOrTargetsNeverFallBackToAnotherModem() async throws {
        let foreign = CommunicationFixture.line("foreign", modemID: "device-2")
        let cases: [(String, [CommunicationLine])] = [
            ("missing source", [foreign]),
            ("unknown modem", [CommunicationFixture.line("deleted", modemID: "", deletionState: "deleted", messageCount: 1), foreign]),
            ("live source", [CommunicationFixture.line("deleted"), CommunicationFixture.line("active")]),
            ("no local target", [source, foreign]),
            ("absent local target", [source, CommunicationFixture.line("absent", present: false), foreign])
        ]
        for (name, lines) in cases {
            let server = CommunicationFixture()
            await server.setLines(lines)
            let state = try await make(server)
            await state.load()
            XCTAssertTrue(state.candidates.isEmpty, name)
            XCTAssertTrue(state.selectedID.isEmpty, name)
            XCTAssertFalse(state.canSubmit, name)
        }
    }

    func testReloadReconcilesSelectionWithCurrentCandidates() async throws {
        let server = CommunicationFixture(historyAssignment: .multiple)
        let state = try await make(server)
        await state.load()
        state.selectedID = "backup"
        await state.load()
        XCTAssertEqual(state.selectedID, "backup")

        await server.setLines([source, CommunicationFixture.line("work"), CommunicationFixture.line("replacement")])
        await state.load()
        XCTAssertTrue(state.selectedID.isEmpty)
        XCTAssertFalse(state.canSubmit)

        await server.setLines([source, CommunicationFixture.line("replacement")])
        await state.load()
        XCTAssertEqual(state.selectedID, "replacement")
        XCTAssertTrue(state.canSubmit)
    }

    func testLoadFailureDisablesAssignmentUntilRetrySucceeds() async throws {
        let server = CommunicationFixture(historyAssignment: .single)
        let state = try await make(server)
        await state.load()
        XCTAssertTrue(state.canSubmit)

        await server.setDirectoryUnavailable(true)
        await state.load()
        guard case .failed = state.loadState else { return XCTFail("Expected a load failure") }
        XCTAssertFalse(state.canSubmit)
        let assigned = await state.assign()
        XCTAssertFalse(assigned)

        await server.setDirectoryUnavailable(false)
        let previousFailure = state.loadState
        await server.holdNextRead("/sim-profiles")
        let retry = Task { await state.load() }
        await server.waitForRead()
        XCTAssertEqual(state.loadState, previousFailure)
        XCTAssertTrue(state.loading)
        XCTAssertFalse(state.canSubmit)
        await server.releaseRead()
        await retry.value
        XCTAssertEqual(state.loadState, .loaded)
        XCTAssertFalse(state.loading)
        XCTAssertTrue(state.canSubmit)
    }

    func testAssignmentFailureKeepsSelectionForRetry() async throws {
        let server = CommunicationFixture(historyAssignment: .single)
        let state = try await make(server)
        await state.load()
        await server.setAssignmentStatus(503)
        let first = await state.assign()
        XCTAssertFalse(first)
        XCTAssertNotNil(state.failure)
        XCTAssertEqual(state.selectedID, "work")
        XCTAssertFalse(state.busy)
        XCTAssertTrue(state.canSubmit)

        await server.setAssignmentStatus(204)
        let retry = await state.assign()
        XCTAssertTrue(retry)
        XCTAssertNil(state.failure)
    }

    func testCancelledLoadDoesNotPublishLines() async throws {
        let server = CommunicationFixture(historyAssignment: .single)
        let state = try await make(server)
        await server.holdNextRead("/sim-profiles")
        let load = Task { await state.load() }
        await server.waitForRead()
        load.cancel()
        await server.releaseRead()
        await load.value
        XCTAssertNil(state.source)
        XCTAssertFalse(state.canSubmit)
        XCTAssertEqual(state.loadState, .idle)
    }

    func testCancelledReloadRequiresFreshInventoryBeforeAssignment() async throws {
        let server = CommunicationFixture(historyAssignment: .single)
        let state = try await make(server)
        await state.load()
        XCTAssertTrue(state.canSubmit)

        await server.holdNextRead("/sim-profiles")
        let reload = Task { await state.load() }
        await server.waitForRead()
        XCTAssertFalse(state.canSubmit)
        reload.cancel()
        await server.releaseRead()
        await reload.value
        XCTAssertFalse(state.canSubmit)
        let assigned = await state.assign()
        XCTAssertFalse(assigned)

        await state.load()
        XCTAssertTrue(state.canSubmit)
    }

    func testLateCancelledLoadCannotReplaceNewInventory() async throws {
        let server = CommunicationFixture(historyAssignment: .single)
        let state = try await make(server)
        await server.holdNextRead("/sim-profiles")
        let oldLoad = Task { await state.load() }
        await server.waitForRead()
        oldLoad.cancel()

        await server.setLines([source, CommunicationFixture.line("replacement")])
        await state.load()
        XCTAssertEqual(state.selectedID, "replacement")
        XCTAssertFalse(state.loading)
        XCTAssertTrue(state.canSubmit)

        await server.releaseRead()
        await oldLoad.value
        XCTAssertEqual(state.selectedID, "replacement")
        XCTAssertTrue(state.canSubmit)
    }
}

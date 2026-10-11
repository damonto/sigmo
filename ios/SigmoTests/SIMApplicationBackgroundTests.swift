import XCTest
@testable import Sigmo

@MainActor
final class SIMApplicationBackgroundTests: XCTestCase {
    func testInitialStatusReleasesAssertionAndIdleObservationCanSuspend() async throws {
        let fixture = try await make()
        let stream = Task { await fixture.sim.observe() }
        defer { fixture.sim.stop(); stream.cancel() }
        try await wait { fixture.sim.available }
        XCTAssertEqual(fixture.assertions.begun, 1)
        XCTAssertEqual(fixture.assertions.ended.count, 1)
        fixture.session.foreground = false
        XCTAssertTrue(fixture.sim.observationSuspended)
        // The subscription's SwiftUI task cancels when observationSuspended changes.
        stream.cancel()
        await stream.value
        XCTAssertNil(fixture.sim.failure)
        XCTAssertEqual(fixture.assertions.ended.count, 1)
    }

    func testInteractionSurvivesBackgroundAndReleasesAssertionOnDismissal() async throws {
        let fixture = try await make()
        let stream = Task { await fixture.sim.observe() }
        defer { fixture.sim.stop(); stream.cancel() }
        try await wait { fixture.sim.available }
        fixture.sim.open()
        await fixture.sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = fixture.sim.page { true } else { false } }
        XCTAssertEqual(fixture.assertions.begun, 2)
        XCTAssertEqual(fixture.assertions.ended.count, 1)
        fixture.session.foreground = false
        XCTAssertFalse(fixture.sim.observationSuspended)
        await fixture.sim.send(.init(type: "input_response", text: "1234"))
        XCTAssertNil(fixture.sim.failure)
        XCTAssertTrue(fixture.sim.available)
        await fixture.sim.dismiss()
        XCTAssertEqual(fixture.assertions.begun, 2)
        XCTAssertEqual(fixture.assertions.ended.count, 2)
        XCTAssertTrue(fixture.sim.observationSuspended)
    }

    func testExpirationClosesOnlyItsOwnInteractionAndEndsAssertionOnce() async throws {
        let fixture = try await make()
        let first = Task { await fixture.sim.observe() }
        defer { fixture.sim.stop(); first.cancel() }
        try await wait { fixture.sim.available }
        fixture.sim.open()
        await fixture.sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = fixture.sim.page { true } else { false } }
        let expire = try XCTUnwrap(fixture.assertions.expire)
        fixture.session.foreground = false
        expire()
        await first.value
        XCTAssertEqual(fixture.sim.failure?.code, "detail.interrupted")
        XCTAssertFalse(fixture.sim.available)
        XCTAssertEqual(fixture.assertions.ended.count, 2)

        fixture.session.foreground = true
        let replacement = Task { await fixture.sim.observe() }
        defer { replacement.cancel() }
        try await wait { fixture.sim.available }
        fixture.sim.open()
        await fixture.sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = fixture.sim.page { true } else { false } }
        // A delayed expiration from the previous assertion cannot stop its replacement.
        expire()
        XCTAssertTrue(fixture.sim.available)
        XCTAssertNil(fixture.sim.failure)
        XCTAssertEqual(fixture.assertions.ended.count, 3)
        fixture.sim.stop()
        await replacement.value
        XCTAssertEqual(fixture.assertions.ended.count, 4)
    }

    func testLogoutReleasesInteractionAssertionWithoutReportingCancellation() async throws {
        let fixture = try await make()
        let stream = Task { await fixture.sim.observe() }
        defer { fixture.sim.stop(); stream.cancel() }
        try await wait { fixture.sim.available }
        fixture.sim.open()
        await fixture.sim.send(.init(type: "menu_selection", itemId: 2))
        try await wait { if case .input = fixture.sim.page { true } else { false } }
        fixture.session.logout()
        await stream.value
        XCTAssertFalse(fixture.sim.available)
        XCTAssertNil(fixture.sim.failure)
        XCTAssertEqual(fixture.assertions.begun, fixture.assertions.ended.count)
    }

    func testImmediateExpirationDoesNotOpenSocket() async throws {
        let fixture = try await make()
        fixture.assertions.expiresImmediately = true
        await fixture.sim.observe()
        XCTAssertFalse(fixture.sim.available)
        XCTAssertEqual(fixture.sim.failure?.code, "detail.interrupted")
        XCTAssertEqual(fixture.assertions.ended.count, 1)
        let requests = await fixture.server.allRequests()
        XCTAssertFalse(requests.contains { $0.url?.scheme == "wss" })
    }

    private struct Fixture {
        let server: DetailFixtureServer
        let session: BackendSession
        let assertions: BackgroundAssertions
        let sim: SIMApplicationState
    }

    private func make() async throws -> Fixture {
        let server = DetailFixtureServer()
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration, transport: { try await server.respond($0) },
                            socketFactory: { try await server.socket($0) }))
        await session.refresh(force: true)
        let assertions = BackgroundAssertions()
        let sim = SIMApplicationState(session: session, modemID: "device-1", backgroundAccess: assertions.access)
        return Fixture(server: server, session: session, assertions: assertions, sim: sim)
    }

    private func wait(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while !condition(), ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        guard condition() else {
            XCTFail("SIM state did not arrive before the deadline")
            throw URLError(.timedOut)
        }
    }
}

import XCTest
@testable import Sigmo

@MainActor
final class CarrierLogoTests: XCTestCase {
    private func make(_ server: DetailFixtureServer, host: String = "logos.example") async throws -> BackendSession {
        let config = BackendConfiguration(name: host, address: try BackendAddress.normalize("https://\(host)"))
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: config, transport: { try await server.respond($0) }))
        await session.refresh(force: true)
        return session
    }

    private func profiles(_ icons: [(String, String)]) throws -> Data {
        let groups = icons.enumerated().map { index, item in
            ESIMProfileGroup(id: "se-\(index)", label: "", aid: nil, eid: nil, profiles: [
                ESIMProfile(seId: "se-\(index)", seLabel: "", seEid: nil, name: "Carrier", serviceProviderName: "Carrier",
                            iccid: item.0, isdPAID: nil, icon: item.1, profileName: "", profileNickname: nil,
                            profileState: 1, profileStateName: "enabled", profileClass: "operational",
                            profileOwner: ESIMOwner(mcc: "310", mnc: "070", gid1: nil, gid2: nil), regionCode: "US")
            ])
        }
        return try JSONEncoder().encode(ESIMProfiles(ses: groups))
    }

    func testPLMNValidationPreservesLeadingZeroes() {
        XCTAssertEqual(CarrierLogo.url(plmn: "310070")?.absoluteString, "https://sigmo-pro.952007.xyz/logo/310070")
        XCTAssertEqual(ESIMOwner(mcc: "310", mnc: "070", gid1: nil, gid2: nil).plmn, "310070")
        XCTAssertEqual(ESIMOwner(mcc: "310", mnc: "70", gid1: nil, gid2: nil).plmn, "31070")
        for plmn in ["", "310-070", "3107", "3100700", "../logo", "３１００７０"] {
            XCTAssertNil(CarrierLogo.url(plmn: plmn), plmn)
        }
        XCTAssertNil(ESIMOwner(mcc: "31", mnc: "0070", gid1: nil, gid2: nil).plmn)
        XCTAssertNil(CarrierLogo.embeddedImage("data:image/png;base64,broken"))
        XCTAssertNil(CarrierLogo.embeddedImage("https://example.test/image.png"))
        XCTAssertNotNil(CarrierLogo.embeddedImage("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"))
    }

    func testHomeMatchesICCIDAcrossSEsAndCachesUntilManualRefresh() async throws {
        let server = DetailFixtureServer()
        await server.setProfilesData(try profiles([("unrelated", "wrong"), ("8986000000000000001", "right")]))
        let session = try await make(server)
        let modem = try XCTUnwrap(session.modems.first)
        XCTAssertTrue(session.modemIcons.isPending(modem))
        await session.modemIcons.load(modem, session: session)
        XCTAssertEqual(session.modemIcons.icon(for: modem), "right")
        XCTAssertFalse(session.modemIcons.isPending(modem))
        await session.modemIcons.load(modem, session: session)
        let cachedCount = await server.allRequests().filter { $0.url?.path.hasSuffix("/esims") == true }.count
        XCTAssertEqual(cachedCount, 1)
        await session.refresh(force: true)
        await session.modemIcons.load(modem, session: session)
        let refreshedCount = await server.allRequests().filter { $0.url?.path.hasSuffix("/esims") == true }.count
        XCTAssertEqual(refreshedCount, 2)
    }

    func testHomeFailuresFallbackAndPhysicalOrLockedSIMsDoNotReadProfiles() async throws {
        let server = DetailFixtureServer(scenario: .unavailableProfiles)
        let session = try await make(server)
        let modem = try XCTUnwrap(session.modems.first)
        await session.modemIcons.load(modem, session: session)
        XCTAssertFalse(session.modemIcons.isPending(modem))
        XCTAssertNil(session.modemIcons.icon(for: modem))
        XCTAssertTrue(session.isOnline)
        XCTAssertNil(session.failure)
        for fixture in [DetailFixtureServer(physical: true), DetailFixtureServer(locked: true)] {
            let excluded = try await make(fixture)
            let device = try XCTUnwrap(excluded.modems.first)
            await excluded.modemIcons.load(device, session: excluded)
            XCTAssertFalse(excluded.modemIcons.isPending(device))
            let requests = await fixture.allRequests()
            XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/esims") == true })
        }
    }

    func testSharedReadAndLateResponseCannotCrossSIMSlots() async throws {
        let server = DetailFixtureServer()
        await server.setProfilesData(try profiles([("8986000000000000001", "old")]))
        let session = try await make(server)
        let modem = try XCTUnwrap(session.modems.first)
        await server.holdNext("/esims")
        let first = Task { await session.modemIcons.load(modem, session: session) }
        await server.waitForHeldRequest()
        let duplicate = Task { await session.modemIcons.load(modem, session: session) }
        await Task.yield()
        let heldCount = await server.allRequests().filter { $0.url?.path.hasSuffix("/esims") == true }.count
        XCTAssertEqual(heldCount, 1)
        try await session.protectedRequest { api, token in
            try await api.detailMutation(modem.id, path: ["sim-slots", "2"], method: "PUT", token: token)
        }
        await session.refreshModem(modem.id)
        await server.release()
        await first.value
        await duplicate.value
        let replacement = try XCTUnwrap(session.modems.first)
        XCTAssertNil(session.modemIcons.icon(for: replacement))
        await server.setProfilesData(try profiles([("8986000000000000099", "new")]))
        await session.modemIcons.load(replacement, session: session)
        XCTAssertEqual(session.modemIcons.icon(for: replacement), "new")
    }

    func testSessionsStayIsolatedAndLogoutDropsPendingResponse() async throws {
        let firstServer = DetailFixtureServer(), secondServer = DetailFixtureServer()
        await firstServer.setProfilesData(try profiles([("8986000000000000001", "first")]))
        await secondServer.setProfilesData(try profiles([("8986000000000000001", "second")]))
        let first = try await make(firstServer, host: "one.example")
        let second = try await make(secondServer, host: "two.example")
        let one = try XCTUnwrap(first.modems.first), two = try XCTUnwrap(second.modems.first)
        await second.modemIcons.load(two, session: second)
        await firstServer.holdNext("/esims")
        let pending = Task { await first.modemIcons.load(one, session: first) }
        await firstServer.waitForHeldRequest()
        first.logout()
        await firstServer.release()
        await pending.value
        XCTAssertNil(first.modemIcons.icon(for: one))
        XCTAssertEqual(second.modemIcons.icon(for: two), "second")
        XCTAssertFalse(first.canAccessDevices)
    }

    func testAmbiguousActiveProfilesHaveNoEmbeddedIcon() async throws {
        let server = DetailFixtureServer()
        await server.setProfilesData(try profiles([("8986000000000000001", "one"), ("8986000000000000001", "two")]))
        let session = try await make(server)
        let modem = try XCTUnwrap(session.modems.first)
        await session.modemIcons.load(modem, session: session)
        XCTAssertNil(session.modemIcons.icon(for: modem))
        XCTAssertFalse(session.modemIcons.isPending(modem))
    }

    func testReturningRowRetriesItsCancelledSharedRequest() async throws {
        let server = DetailFixtureServer()
        await server.setProfilesData(try profiles([("8986000000000000001", "returned")]))
        let session = try await make(server)
        let modem = try XCTUnwrap(session.modems.first)
        await server.holdNext("/esims")
        let leaving = Task { await session.modemIcons.load(modem, session: session) }
        await server.waitForHeldRequest()
        leaving.cancel()
        let returning = Task { await session.modemIcons.load(modem, session: session) }
        await Task.yield()
        await server.release()
        await leaving.value
        await returning.value
        XCTAssertEqual(session.modemIcons.icon(for: modem), "returned")
        XCTAssertFalse(session.modemIcons.isPending(modem))
        let count = await server.allRequests().filter { $0.url?.path.hasSuffix("/esims") == true }.count
        XCTAssertEqual(count, 2)
    }

    func testStaleRowCannotCancelCurrentSIMIcon() async throws {
        let server = DetailFixtureServer()
        let session = try await make(server)
        let old = try XCTUnwrap(session.modems.first)
        try await session.protectedRequest { api, token in
            try await api.detailMutation(old.id, path: ["sim-slots", "2"], method: "PUT", token: token)
        }
        await session.refreshModem(old.id)
        let current = try XCTUnwrap(session.modems.first)
        await server.setProfilesData(try profiles([("8986000000000000099", "current")]))
        await server.holdNext("/esims")
        let pending = Task { await session.modemIcons.load(current, session: session) }
        await server.waitForHeldRequest()
        await session.modemIcons.load(old, session: session)
        await server.release()
        await pending.value
        XCTAssertEqual(session.modemIcons.icon(for: current), "current")
        let count = await server.allRequests().filter { $0.url?.path.hasSuffix("/esims") == true }.count
        XCTAssertEqual(count, 1)
    }
}

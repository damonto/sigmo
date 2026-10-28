import XCTest
@testable import Sigmo

@MainActor
final class CarrierLogoTests: XCTestCase {
    func testPLMNValidationPreservesLeadingZeroes() {
        XCTAssertEqual(CarrierLogo.url(plmn: "310070")?.absoluteString, "https://sigmo.952007.xyz/logo/310070")
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

    func testHomeUsesOverviewIconWithoutProfileRequests() async throws {
        let stub = BackendStub()
        let icon = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
        let original = PreviewBackendServer.modemJSON()
        let withIcon = original.replacingOccurrences(of: #""sim":{"#, with: #""sim":{"icon":"\#(icon)","#)
        await stub.setModemJSON(withIcon)
        let config = BackendConfiguration(name: "Home", address: try BackendAddress.normalize("https://logos.example"))
        let session = BackendSession(configuration: config, credentials: MemoryCredentials().store,
                                     api: BackendAPI(configuration: config) { try await stub.respond($0) })

        await session.refresh(force: true)
        XCTAssertEqual(session.modems.first?.sim.icon, icon)

        // Profiles without an icon use the PLMN/flag fallback.
        await stub.setModemJSON(original)
        await session.refresh(force: true)
        XCTAssertNil(session.modems.first?.sim.icon)
        let requests = await stub.allRequests()
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/esims") == true })
    }
}

import XCTest
import UIKit
@testable import Sigmo

@MainActor
final class CarrierLogoTests: XCTestCase {
    func testPLMNValidationPreservesLeadingZeroes() {
        XCTAssertEqual(CarrierLogoArtwork.catalogURL(plmn: "310070")?.absoluteString, "https://sigmo.952007.xyz/logo/310070")
        XCTAssertEqual(ESIMOwner(mcc: "310", mnc: "070", gid1: nil, gid2: nil).plmn, "310070")
        XCTAssertEqual(ESIMOwner(mcc: "310", mnc: "70", gid1: nil, gid2: nil).plmn, "31070")
        for plmn in ["", "310-070", "3107", "3100700", "../logo", "３１００７０"] {
            XCTAssertNil(CarrierLogoArtwork.catalogURL(plmn: plmn), plmn)
        }
        XCTAssertNil(ESIMOwner(mcc: "31", mnc: "0070", gid1: nil, gid2: nil).plmn)
    }

    func testEmbeddedArtworkIsDownsampledAndTintIsOptional() async throws {
        let size = CGSize(width: 1024, height: 256)
        let logo = renderLogo(size: size) { context in
            context.setFillColor(UIColor.blue.cgColor)
            context.fill(CGRect(origin: .zero, size: size))
        }
        let data = try XCTUnwrap(logo.pngData())
        let source = CarrierLogoArtwork.Source(icon: "data:image/png;base64,\(data.base64EncodedString())", catalogURL: nil)

        let plainArtwork = await source.load(samplesTint: false)
        let plain = try XCTUnwrap(plainArtwork)
        XCTAssertLessThanOrEqual(plain.image.width, 384)
        XCTAssertEqual(plain.image.width, plain.image.height * 4)
        XCTAssertNil(plain.tint)

        let tintedArtwork = await source.load(samplesTint: true)
        XCTAssertNotNil(tintedArtwork?.tint)
    }

    func testInvalidEmbeddedArtworkFallsBack() async {
        for icon in ["data:image/png;base64,broken", "https://example.test/image.png"] {
            let source = CarrierLogoArtwork.Source(icon: icon, catalogURL: nil)
            let artwork = await source.load(samplesTint: true)
            XCTAssertNil(artwork, icon)
        }
    }

    func testDominantColorSelectsBrandHueOverWhiteBacking() throws {
        let logo = renderLogo { context in
            context.setFillColor(UIColor.white.cgColor)
            context.fill(CGRect(x: 0, y: 0, width: 32, height: 32))
            context.setFillColor(UIColor.blue.cgColor)
            context.fill(CGRect(x: 4, y: 10, width: 15, height: 12))
            context.setFillColor(UIColor.red.cgColor)
            context.fill(CGRect(x: 21, y: 10, width: 7, height: 12))
        }
        let bitmap = try XCTUnwrap(logo.cgImage)
        let tint = try XCTUnwrap(CarrierLogoArtwork.dominantColor(in: bitmap))
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0
        XCTAssertTrue(UIColor(cgColor: tint).getRed(&red, green: &green, blue: &blue, alpha: nil))
        XCTAssertEqual(red, 0, accuracy: 0.05)
        XCTAssertEqual(green, 0, accuracy: 0.05)
        XCTAssertEqual(blue, 1, accuracy: 0.05)
    }

    func testDominantColorIgnoresFaintAndMonochromePixels() throws {
        let logo = renderLogo { context in
            context.setFillColor(UIColor.red.withAlphaComponent(0.1).cgColor)
            context.fill(CGRect(x: 0, y: 0, width: 16, height: 32))
            context.setFillColor(UIColor.darkGray.cgColor)
            context.fill(CGRect(x: 16, y: 0, width: 16, height: 32))
        }
        let bitmap = try XCTUnwrap(logo.cgImage)
        XCTAssertNil(CarrierLogoArtwork.dominantColor(in: bitmap))
    }

    private func renderLogo(size: CGSize = CGSize(width: 32, height: 32), _ draw: (CGContext) -> Void) -> UIImage {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: size, format: format).image { draw($0.cgContext) }
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
        XCTAssertFalse(requests.contains { $0.url?.path.hasSuffix("/esim-profiles") == true })
    }
}

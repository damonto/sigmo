import XCTest
import WebKit
@testable import Sigmo

@MainActor
final class CarrierWebsheetTests: XCTestCase {
    func testLocalCarrierPageRelaysTypedCallback() async throws {
        let received = expectation(description: "Native callback")
        var payload: CarrierCallback?
        let url = try XCTUnwrap(URL(string: "https://fixture.example/prefix/api/v1/websheets/local"))
        let parent = CarrierWebsheetView(url: url, callback: { value in
            payload = value
            received.fulfill()
        }, failure: { failure in XCTFail(failure.message) })
        let coordinator = parent.makeCoordinator()
        let webView = parent.configuredWebView(coordinator: coordinator)
        defer { CarrierWebsheetView.dismantleUIView(webView, coordinator: coordinator) }
        let html = #"""
        <!doctype html><html><body><p>Carrier fixture</p><script>
        window.addEventListener('load', () => {
          window.postMessage({type:'sigmo-websheet-callback',callback:{event:'profileReadyWithActivationCode',activationCode:'fixture-code',iccid:'8900'}}, location.origin);
        });
        </script></body></html>
        """#
        webView.loadHTMLString(html, baseURL: url)
        await fulfillment(of: [received], timeout: 10)
        XCTAssertEqual(payload?.event, "profileReadyWithActivationCode")
        XCTAssertEqual(payload?.activationCode, "fixture-code")
        XCTAssertEqual(payload?.iccid, "8900")
        XCTAssertFalse(webView.configuration.websiteDataStore.isPersistent)
    }
}

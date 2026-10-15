import Foundation

// No permissions or physical reader access is needed for the sample source.
extension SmartCardAccess {
    static var demo: SmartCardAccess {
        SmartCardAccess(readers: { [LocalCardReader(name: "Demo USB Reader", cardPresent: true)] }, bind: { _, _ in
            SmartCardConnection(open: {}, transmit: { _ in Data([0x90, 0x00]) }, close: {}, invalidate: {})
        })
    }
}
enum DemoResources {
    static func skillArchive() throws -> Data {
        guard let url = Bundle.main.url(forResource: "DemoControl", withExtension: "zip") else {
            throw BackendFailure.local("demo.unsupportedRequest")
        }
        return try Data(contentsOf: url)
    }

    static var carrierHTML: String {
        """
        <!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'">
        <style>body { font: -apple-system-body; padding: 24px; } button { font: inherit; min-height: 48px; width: 100%; }</style>
        </head><body><h1>\(String(localized: "demo.carrierTitle"))</h1>
        <p>\(String(localized: "demo.carrierDescription"))</p>
        <button onclick="window.postMessage({type:'sigmo-websheet-callback',callback:{event:'entitlementChanged'}}, '*')">\(String(localized: "demo.carrierComplete"))</button>
        </body></html>
        """
    }
}


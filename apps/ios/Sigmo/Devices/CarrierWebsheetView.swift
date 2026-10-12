import SwiftUI
import WebKit

enum CarrierWebsheetAddress {
    static func url(_ sheet: CarrierWebsheet, api: BackendAPI, token: String?) throws -> URL {
        let query = token.map { [URLQueryItem(name: "token", value: $0)] } ?? []
        guard !sheet.id.isEmpty,
              let expected = try api.makeRequest(["websheets", sheet.id], query: query).url,
              let supplied = URL(string: sheet.embedUrl, relativeTo: api.address)?.absoluteURL,
              BackendAddress.sameOrigin(api.address, supplied),
              supplied.path == expected.path || supplied.path == "/api/v1/websheets/" + sheet.id else {
            throw BackendFailure.local("detail.websheetInvalid")
        }
        return expected
    }
}

struct CarrierWebsheetView: UIViewRepresentable {
    let url: URL
    let callback: (CarrierCallback) -> Void
    let failure: (BackendFailure) -> Void
    var fixtureHTML: String? = nil
    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeUIView(context: Context) -> WKWebView {
        let view = configuredWebView(coordinator: context.coordinator)
        #if DEBUG
        if let fixtureHTML {
            view.loadHTMLString(fixtureHTML, baseURL: url)
            return view
        }
        #endif
        view.load(URLRequest(url: url))
        return view
    }
    // Separating configuration permits local HTML to exercise the real bridge without network access.
    func configuredWebView(coordinator: Coordinator) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.userContentController.add(coordinator, name: "sigmoNative")
        let prefix = url.path.components(separatedBy: "/websheets/").first ?? "/api/v1"
        // Encode configured paths as JSON literals before inserting them into JavaScript.
        let encoded = (try? JSONEncoder().encode(prefix)).flatMap { String(data: $0, encoding: .utf8) } ?? "\"/api/v1\""
        let script = """
        (() => {
          const prefix = \(encoded);
          const rewrite = value => {
            if (typeof value !== 'string') return value;
            try {
              const u = new URL(value, location.href);
              if (u.origin === location.origin && u.pathname.startsWith('/api/v1/websheets/') && prefix !== '/api/v1') {
                u.pathname = prefix + u.pathname.slice('/api/v1'.length);
                return u.href;
              }
            } catch (_) {}
            return value;
          };
          const fetch = window.fetch;
          window.fetch = function(input, init) { return fetch.call(this, rewrite(input), init); };
          const open = XMLHttpRequest.prototype.open;
          XMLHttpRequest.prototype.open = function(method, url, ...rest) { return open.call(this, method, rewrite(url), ...rest); };
          window.addEventListener('message', event => {
            if (event.source !== window || event.origin !== location.origin || event.data?.type !== 'sigmo-websheet-callback') return;
            window.webkit.messageHandlers.sigmoNative.postMessage(JSON.stringify(event.data.callback));
          });
        })();
        """
        configuration.userContentController.addUserScript(WKUserScript(source: script, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = coordinator
        return view
    }
    func updateUIView(_ view: WKWebView, context: Context) { context.coordinator.parent = self }
    static func dismantleUIView(_ view: WKWebView, coordinator: Coordinator) {
        view.stopLoading()
        view.configuration.userContentController.removeScriptMessageHandler(forName: "sigmoNative")
        view.navigationDelegate = nil
    }
    @MainActor final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        var parent: CarrierWebsheetView
        init(_ parent: CarrierWebsheetView) { self.parent = parent }
        func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
            guard message.frameInfo.isMainFrame, let origin = message.frameInfo.request.url,
                  BackendAddress.sameOrigin(parent.url, origin),
                  let raw = message.body as? String, let data = raw.data(using: .utf8) else { return }
            do { parent.callback(try JSONDecoder().decode(CarrierCallback.self, from: data)) }
            catch { parent.failure(.local("api.invalidResponse")) }
        }
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url, BackendAddress.sameOrigin(parent.url, url) else {
                decisionHandler(.cancel); parent.failure(.local("detail.websheetInvalid")); return
            }
            let prefix = parent.url.path.components(separatedBy: "/websheets/").first ?? "/api/v1"
            if prefix != "/api/v1", url.path.hasPrefix("/api/v1/websheets/"),
               var components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
                components.percentEncodedPath = prefix + components.percentEncodedPath.dropFirst("/api/v1".count)
                var request = action.request
                request.url = components.url
                decisionHandler(.cancel)
                webView.load(request)
                return
            }
            decisionHandler(.allow)
        }
        func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
            if (error as? URLError)?.code != .cancelled { parent.failure(.wrapping(error)) }
        }
        func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
            if (error as? URLError)?.code != .cancelled { parent.failure(.wrapping(error)) }
        }
        func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse, decisionHandler: @escaping @MainActor @Sendable (WKNavigationResponsePolicy) -> Void) {
            if let http = response.response as? HTTPURLResponse, http.statusCode >= 400 {
                parent.failure(BackendFailure(status: http.statusCode, code: "websheet_http", message: HTTPURLResponse.localizedString(forStatusCode: http.statusCode)))
                decisionHandler(.cancel)
            } else { decisionHandler(.allow) }
        }
    }
}

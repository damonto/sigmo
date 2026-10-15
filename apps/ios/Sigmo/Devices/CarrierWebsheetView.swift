import SwiftUI
import WebKit

enum CarrierWebsheetAddress {
    static func url(_ sheet: CarrierWebsheet, api: BackendAPI) throws -> URL {
        guard !sheet.id.isEmpty,
              let expected = try api.makeRequest(["websheets", sheet.id]).url,
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
    let callback: (CarrierCallbackDelivery) -> Void
    let failure: (BackendFailure) -> Void
    var fixtureHTML: String? = nil
    var session: BackendSession? = nil
    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeUIView(context: Context) -> WKWebView {
        let view = configuredWebView(coordinator: context.coordinator)
        view.load(URLRequest(url: CarrierResourceLoader.localURL(url)))
        return view
    }
    private var localHTML: String? {
        #if DEBUG
        if let fixtureHTML { return fixtureHTML }
        #endif
        return session?.isDemo == true ? DemoResources.carrierHTML : nil
    }

    // Separating configuration permits local HTML to exercise the real bridge without network access.
    func configuredWebView(coordinator: Coordinator) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(coordinator, forURLScheme: CarrierResourceLoader.scheme)
        configuration.userContentController.add(coordinator, name: "sigmoNative")
        // Encode the capability address as a JSON literal; it contains no login credential.
        let encoded = (try? JSONEncoder().encode(url.absoluteString)).flatMap { String(data: $0, encoding: .utf8) } ?? "\"\""
        let script = """
        (() => {
          const root = new URL(\(encoded));
          const unprefixed = '/api/v1/websheets/' + root.pathname.split('/').pop();
          const rewrite = value => {
            if (typeof value !== 'string' && !(value instanceof URL)) return value;
            try {
              const u = new URL(value, location.href);
              if (u.host !== root.host || u.username || u.password || ![root.protocol, 'sigmo-carrier:'].includes(u.protocol)) return value;
              if (u.pathname === unprefixed || u.pathname.startsWith(unprefixed + '/')) {
                u.pathname = root.pathname + u.pathname.slice(unprefixed.length);
              }
              if (u.pathname !== root.pathname && !u.pathname.startsWith(root.pathname + '/')) return value;
              return 'sigmo-carrier://' + u.host + u.pathname + u.search + u.hash;
            } catch (_) {}
            return value;
          };
          // The server bridge posts first, then notifies its parent. Preserve that
          // acknowledgment so native UI never submits the same callback again.
          const deliveries = new Map();
          const remember = (key, status) => {
            deliveries.set(key, status);
            if (deliveries.size > 64) deliveries.delete(deliveries.keys().next().value);
          };
          const callbackKey = (input, init) => {
            try {
              const target = new URL(input instanceof Request ? input.url : input, location.href);
              if (target.host !== root.host || target.pathname !== root.pathname + '/callback') return null;
              if ((init?.method || (input instanceof Request ? input.method : 'GET')).toUpperCase() !== 'POST') return null;
              if (typeof init?.body !== 'string') return null;
              return JSON.stringify(JSON.parse(init.body));
            } catch (_) { return null; }
          };
          const fetch = window.fetch;
          window.fetch = function(input, init) {
            if (input instanceof Request) input = new Request(rewrite(input.url), input);
            else input = rewrite(input);
            const key = callbackKey(input, init);
            try {
              const result = fetch.call(this, input, init);
              if (key === null) return result;
              // The native resource loader rejects unsuccessful HTTP responses.
              return result.then(response => { remember(key, 'submitted'); return response; }, error => {
                remember(key, 'failed'); throw error;
              });
            } catch (error) {
              if (key !== null) remember(key, 'failed');
              throw error;
            }
          };
          const open = XMLHttpRequest.prototype.open;
          XMLHttpRequest.prototype.open = function(method, url, ...rest) { return open.call(this, method, rewrite(url), ...rest); };
          window.addEventListener('message', event => {
            if (event.source !== window || event.origin !== location.origin || event.data?.type !== 'sigmo-websheet-callback') return;
            const callback = event.data.callback;
            const status = deliveries.get(JSON.stringify(callback)) || 'native';
            window.webkit.messageHandlers.sigmoNative.postMessage(JSON.stringify({callback, status}));
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
        coordinator.stopResources()
        view.stopLoading()
        view.configuration.userContentController.removeScriptMessageHandler(forName: "sigmoNative")
        view.navigationDelegate = nil
    }
    @MainActor final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler, WKURLSchemeHandler {
        var parent: CarrierWebsheetView
        private let resources: CarrierResourceLoader
        private var tasks: [ObjectIdentifier: Task<Void, Never>] = [:]
        init(_ parent: CarrierWebsheetView, resources: CarrierResourceLoader? = nil) {
            self.parent = parent
            self.resources = resources ?? CarrierResourceLoader(root: parent.url)
        }
        func webView(_ webView: WKWebView, start urlSchemeTask: any WKURLSchemeTask) {
            let id = ObjectIdentifier(urlSchemeTask)
            tasks[id] = Task {
                defer { tasks[id] = nil }
                do {
                    guard let session = parent.session else { throw BackendFailure.local("detail.websheetInvalid") }
                    let request = try resources.request(urlSchemeTask.request)
                    let result: BackendHTTPResponse
                    if let html = parent.localHTML {
                        // Keep the same WebKit origin and native callback bridge,
                        // but serve local content without creating a URLSession.
                        guard request.url == parent.url, request.httpMethod == "GET" else {
                            throw BackendFailure.local("demo.unsupportedRequest")
                        }
                        result = BackendHTTPResponse(data: Data(html.utf8), status: 200, contentType: "text/html; charset=utf-8")
                    } else {
                        let resources = resources
                        result = try await session.protectedRequest { _, token in try await resources.load(request, token: token) }
                    }
                    try Task.checkCancellation()
                    guard tasks[id] != nil, let url = urlSchemeTask.request.url,
                          let response = HTTPURLResponse(url: url, statusCode: result.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": result.contentType ?? "application/octet-stream", "Access-Control-Allow-Origin": "*"]) else { return }
                    urlSchemeTask.didReceive(response)
                    urlSchemeTask.didReceive(result.data)
                    urlSchemeTask.didFinish()
                } catch {
                    guard tasks[id] != nil, !Task.isCancelled else { return }
                    urlSchemeTask.didFailWithError(error)
                    parent.failure(.wrapping(error))
                }
            }
        }
        func webView(_ webView: WKWebView, stop urlSchemeTask: any WKURLSchemeTask) {
            tasks.removeValue(forKey: ObjectIdentifier(urlSchemeTask))?.cancel()
        }
        func stopResources() {
            for task in tasks.values { task.cancel() }
            tasks.removeAll()
        }
        func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
            guard message.frameInfo.isMainFrame, let origin = message.frameInfo.request.url,
                  (BackendAddress.sameOrigin(parent.url, origin) || BackendAddress.sameOrigin(CarrierResourceLoader.localURL(parent.url), origin)),
                  let raw = message.body as? String, let data = raw.data(using: .utf8) else { return }
            do { parent.callback(try JSONDecoder().decode(CarrierCallbackDelivery.self, from: data)) }
            catch { parent.failure(.local("api.invalidResponse")) }
        }
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url, (BackendAddress.sameOrigin(parent.url, url) || BackendAddress.sameOrigin(CarrierResourceLoader.localURL(parent.url), url)) else {
                decisionHandler(.cancel); parent.failure(.local("detail.websheetInvalid")); return
            }
            let prefix = parent.url.path.components(separatedBy: "/websheets/").first ?? "/api/v1"
            var target = url
            if prefix != "/api/v1", url.path.hasPrefix("/api/v1/websheets/"), var components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
                components.percentEncodedPath = prefix + components.percentEncodedPath.dropFirst("/api/v1".count)
                if let rewritten = components.url { target = rewritten }
            }
            let local = CarrierResourceLoader.localURL(target)
            guard (try? resources.request(URLRequest(url: local))) != nil else {
                decisionHandler(.cancel); parent.failure(.local("detail.websheetInvalid")); return
            }
            if parent.session != nil, local != url {
                var request = action.request
                request.url = local
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

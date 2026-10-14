import Foundation

// A custom WebKit origin prevents carrier JavaScript from reading Sigmo responses.
// Only native code attaches credentials, and only within this sheet's capability path.
final class CarrierResourceLoader: NSObject, URLSessionTaskDelegate, Sendable {
    static let scheme = "sigmo-carrier"
    let root: URL
    private let configuration: @Sendable () -> URLSessionConfiguration

    init(root: URL, configuration: @escaping @Sendable () -> URLSessionConfiguration = { .ephemeral }) {
        self.root = root
        self.configuration = configuration
    }

    static func localURL(_ url: URL) -> URL {
        var parts = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        parts.scheme = scheme
        return parts.url!
    }

    private func allowed(_ url: URL) -> Bool {
        BackendAddress.sameOrigin(root, url) && url.user == nil && url.fragment == nil &&
            (url.path == root.path || url.path.hasPrefix(root.path + "/")) &&
            !url.path.components(separatedBy: "/").contains("..")
    }

    func request(_ input: URLRequest) throws -> URLRequest {
        guard let url = input.url, url.scheme == Self.scheme,
              var parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("detail.websheetInvalid")
        }
        parts.scheme = root.scheme
        guard let target = parts.url, allowed(target) else { throw BackendFailure.local("detail.websheetInvalid") }
        var request = URLRequest(url: target, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 45)
        request.httpMethod = input.httpMethod
        request.httpBody = input.httpBody
        if let stream = input.httpBodyStream {
            stream.open()
            defer { stream.close() }
            var data = Data()
            var buffer = [UInt8](repeating: 0, count: 8192)
            while true {
                let count = stream.read(&buffer, maxLength: buffer.count)
                if count < 0 { throw stream.streamError ?? URLError(.cannotDecodeContentData) }
                if count == 0 { break }
                guard data.count + count <= 8 * 1024 * 1024 else { throw BackendFailure.local("api.invalidResponse") }
                data.append(contentsOf: buffer.prefix(count))
            }
            request.httpBody = data
        }
        for header in ["Accept", "Accept-Language", "Content-Type"] {
            request.setValue(input.value(forHTTPHeaderField: header), forHTTPHeaderField: header)
        }
        return request
    }

    func load(_ input: URLRequest, token: String?) async throws -> BackendHTTPResponse {
        guard let url = input.url, allowed(url) else { throw BackendFailure.local("detail.websheetInvalid") }
        let config = configuration()
        config.httpCookieStorage = nil
        config.httpShouldSetCookies = false
        config.urlCredentialStorage = nil
        config.urlCache = nil
        config.timeoutIntervalForResource = 45
        let session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
        defer { session.invalidateAndCancel() }
        var request = input
        if let token { request.setValue("Bearer " + token, forHTTPHeaderField: "Authorization") }
        let (bytes, response) = try await session.bytes(for: request)
        guard let response = response as? HTTPURLResponse else { throw BackendFailure.local("api.invalidResponse") }
        var data = Data()
        for try await byte in bytes {
            guard data.count < 8 * 1024 * 1024 else { throw BackendFailure.local("api.invalidResponse") }
            data.append(byte)
        }
        let contentType = response.value(forHTTPHeaderField: "Content-Type")
        var result = BackendHTTPResponse(data: data, status: response.statusCode, contentType: contentType)
        guard (200..<300).contains(result.status) else { throw HTTPProblem.failure(result, source: .backend) }
        if contentType?.lowercased().contains("html") == true, let html = String(data: data, encoding: .utf8) {
            // The proxy emits absolute callback and resource URLs. Rewrite only this
            // capability's URLs; other backend paths never acquire native credentials.
            var unprefixed = URLComponents(url: root, resolvingAgainstBaseURL: false)!
            unprefixed.path = "/api/v1/websheets/" + root.lastPathComponent
            let rewritten = html.replacingOccurrences(of: root.absoluteString, with: Self.localURL(root).absoluteString)
                .replacingOccurrences(of: unprefixed.url!.absoluteString, with: Self.localURL(root).absoluteString)
            result = BackendHTTPResponse(data: Data(rewritten.utf8), status: result.status, contentType: contentType)
        }
        return result
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        guard let url = request.url, allowed(url) else { completionHandler(nil); return }
        completionHandler(request)
    }
}

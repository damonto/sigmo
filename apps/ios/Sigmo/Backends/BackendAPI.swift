import Foundation

final class BackendRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    let origin: URL
    init(origin: URL) { self.origin = origin }

    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        guard let target = request.url, BackendAddress.sameOrigin(origin, target) else {
            completionHandler(nil)
            return
        }
        completionHandler(request)
    }
}

enum BackendRequestTimeout: Sendable {
    case standard, hardware

    var interval: TimeInterval {
        switch self {
        case .standard: 30
        // Modem and eSIM operations can take five minutes before returning data.
        case .hardware: 10 * 60
        }
    }
}

final class BackendAPI: Sendable {
    typealias Transport = @Sendable (URLRequest) async throws -> BackendHTTPResponse
    let isDemo: Bool
    let backendID: UUID
    let address: URL
    private let transport: Transport
    private let session: URLSession?
    private let diagnostics: DiagnosticLogStore
    private let socketFactory: SocketFactory?
    typealias SocketFactory = @Sendable (URLRequest) async throws -> BackendSocket

    init(configuration: BackendConfiguration, diagnostics: DiagnosticLogStore = AppDiagnostics.store,
         transport: Transport? = nil, socketFactory: SocketFactory? = nil, isDemo: Bool = false) {
        self.isDemo = isDemo
        backendID = configuration.id
        address = configuration.address
        self.diagnostics = diagnostics
        self.socketFactory = socketFactory
        if isDemo {
            // A missing demo transport is an implementation error, never a
            // reason to connect the sample address to a real network service.
            self.session = nil
            self.transport = transport ?? { _ in throw BackendFailure.local("demo.unsupportedRequest") }
            return
        }
        if let transport {
            self.session = nil
            self.transport = transport
        } else {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.timeoutIntervalForRequest = BackendRequestTimeout.standard.interval
            configuration.timeoutIntervalForResource = BackendRequestTimeout.hardware.interval
            configuration.urlCache = nil
            configuration.httpCookieStorage = nil
            configuration.httpShouldSetCookies = false
            configuration.urlCredentialStorage = nil
            configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
            let session = URLSession(configuration: configuration, delegate: BackendRedirects(origin: address), delegateQueue: nil)
            self.session = session
            self.transport = { request in
                try await BackendHTTPResponse.receive(request, session: session)
            }
        }
    }

    deinit { session?.invalidateAndCancel() }

    func license(token: String? = nil) async throws -> LicenseStatus {
        let status: LicenseStatus = try await decode(["license"], token: token)
        guard status.deviceId.range(of: "^[0-9a-f]{32}$", options: .regularExpression) != nil else {
            throw BackendFailure.local("api.invalidResponse")
        }
        return status
    }

    func app(token: String? = nil) async throws -> AppInformation { try await decode(["app"], token: token) }
    func capabilities(token: String? = nil) async throws -> Set<String> {
        let response: CapabilityResponse = try await decode(["capabilities"], token: token)
        return Set(response.features ?? [])
    }
    func modems(token: String?) async throws -> [Modem] { try await decode(["modems"], token: token, timeout: .hardware) }
    func modem(_ id: String, token: String?) async throws -> Modem {
        let modem: Modem = try await decode(["modems", id], token: token, timeout: .hardware)
        guard modem.id == id else { throw BackendFailure.local("api.invalidResponse") }
        return modem
    }
    func createPairing(token: String? = nil) async throws -> LicensePairing {
        let pairing: LicensePairing = try await decode(["license", "pairings"], method: "POST", token: token)
        _ = try pairing.validatedURL()
        guard pairing.status == "pending" else { throw BackendFailure.local("api.invalidResponse") }
        return pairing
    }
    func pairing(_ id: String, token: String? = nil) async throws -> LicensePairing {
        let pairing: LicensePairing = try await decode(["license", "pairings", id], token: token)
        _ = try pairing.validatedURL()
        guard pairing.id == id else { throw BackendFailure.local("api.invalidResponse") }
        return pairing
    }

    func decode<Value: Decodable & Sendable>(_ path: [String], method: String = "GET",
                                            body: Data? = nil, token: String? = nil,
                                            query: [URLQueryItem] = [], timeout: BackendRequestTimeout = .standard) async throws -> Value {
        let response = try await request(path, method: method, body: body, token: token, query: query, timeout: timeout)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let value = try decoder.singleValueContainer().decode(String.self)
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: value) { return date }
            formatter.formatOptions = [.withInternetDateTime]
            guard let date = formatter.date(from: value) else { throw BackendFailure.local("api.invalidResponse") }
            return date
        }
        do { return try decoder.decode(Value.self, from: response.data) }
        catch {
            response.diagnosticRequest?.decodingFailed(error, response: response)
            throw BackendFailure(status: response.status, code: "invalid_response",
                                 message: NSLocalizedString("api.invalidResponse", comment: ""), requestID: response.requestID)
        }
    }

    func request(_ path: [String], method: String, body: Data? = nil, token: String? = nil,
                 query: [URLQueryItem] = [], accept: String = "application/json", contentType: String = "application/json",
                 timeout: BackendRequestTimeout = .standard) async throws -> BackendHTTPResponse {
        let request = try makeRequest(path, method: method, body: body, token: token, query: query, accept: accept, contentType: contentType, timeout: timeout)
        let diagnostic = DiagnosticRequest(store: diagnostics, service: .backend, method: method, path: path,
                                           backendID: backendID, requestID: request.value(forHTTPHeaderField: "X-Request-ID"))
        var received: BackendHTTPResponse?
        do {
            try Task.checkCancellation()
            var response: BackendHTTPResponse
            do {
                // URLRequest's timeout resets as bytes arrive. This bounds the whole
                // request as well, including a server that only trickles a response.
                response = try await withThrowingTaskGroup(of: BackendHTTPResponse.self) { group in
                    group.addTask { try await self.transport(request) }
                    group.addTask {
                        try await Task.sleep(for: .seconds(timeout.interval))
                        throw URLError(.timedOut)
                    }
                    defer { group.cancelAll() }
                    guard let response = try await group.next() else { throw CancellationError() }
                    return response
                }
            }
            catch {
                try Task.checkCancellation()
                throw BackendFailure.wrapping(error, source: .backend)
            }
            received = response
            try Task.checkCancellation()
            if response.status == 401, let challenge = AccessChallenge(response: response, backend: address) { throw challenge }
            if (300..<400).contains(response.status) { throw BackendFailure.local("auth.accessSetupRequired") }
            guard (200..<300).contains(response.status) else {
                throw HTTPProblem.failure(response, source: .backend)
            }
            // Injected transports must obey the same boundary as streamed HTTP.
            guard response.data.count <= BackendHTTPResponse.successfulBodyLimit(for: request) else {
                throw BackendHTTPResponse.oversizedResponse()
            }
            diagnostic?.completed(response)
            response.diagnosticRequest = diagnostic
            return response
        } catch {
            diagnostic?.failed(error, response: received)
            throw error
        }
    }
    func makeRequest(_ path: [String], method: String = "GET", body: Data? = nil, token: String? = nil,
                     query: [URLQueryItem] = [], accept: String = "application/json", contentType: String = "application/json",
                     timeout: BackendRequestTimeout = .standard) throws -> URLRequest {
        guard var components = URLComponents(url: address, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("address.invalid")
        }
        let segmentCharacters = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
        let encoded = try path.map { segment in
            // Dot segments can be normalized by URLSession or a reverse proxy,
            // changing which resource a request (including DELETE) addresses.
            guard segment != ".", segment != "..",
                  let value = segment.addingPercentEncoding(withAllowedCharacters: segmentCharacters) else {
                throw BackendFailure.local("address.invalid")
            }
            return value
        }
        components.percentEncodedPath += "/" + encoded.joined(separator: "/")
        if !query.isEmpty { components.queryItems = query }
        guard let url = components.url else { throw BackendFailure.local("address.invalid") }
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout.interval)
        let requestID = UUID().uuidString
        request.setValue(requestID, forHTTPHeaderField: "X-Request-ID")
        request.httpMethod = method
        request.httpBody = body
        request.setValue("\(accept), \(HTTPProblem.mediaType)", forHTTPHeaderField: "Accept")
        request.setValue(Locale.preferredLanguages.first ?? "en", forHTTPHeaderField: "Accept-Language")
        if body != nil { request.setValue(contentType, forHTTPHeaderField: "Content-Type") }
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return request
    }

    func socket(_ path: [String], token: String?) async throws -> BackendSocket {
        try Task.checkCancellation()
        var request = try makeRequest(path, token: token)
        guard let url = request.url,
              var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("address.invalid")
        }
        components.scheme = components.scheme == "https" ? "wss" : "ws"
        guard let socketURL = components.url else { throw BackendFailure.local("address.invalid") }
        request.url = socketURL
        let diagnostic = DiagnosticSocket(request: DiagnosticRequest(store: diagnostics, service: .backend, method: "GET", path: path,
            backendID: backendID, requestID: request.value(forHTTPHeaderField: "X-Request-ID"), kind: .websocket))
        do {
            if let socketFactory { return try await socketFactory(request).recording(diagnostic) }
            guard !isDemo else { throw BackendFailure.local("demo.unsupportedRequest") }
            guard session != nil else { throw BackendFailure.local("detail.socketUnavailable") }
            return try BackendSocket.live(request, diagnostics: diagnostic)
        } catch {
            diagnostic.failed(error)
            throw error
        }
    }

}

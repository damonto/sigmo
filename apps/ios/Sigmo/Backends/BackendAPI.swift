import Foundation

enum BackendRequestTimeout: TimeInterval, Sendable {
    case standard = 15
    case connection = 120
}

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

final class BackendAPI: Sendable {
    typealias Transport = @Sendable (URLRequest) async throws -> BackendHTTPResponse
    let backendID: UUID
    let address: URL
    private let transport: Transport
    private let session: URLSession?
    let socketFactory: SocketFactory
    typealias SocketFactory = @Sendable (URLRequest) async throws -> BackendSocket

    init(configuration: BackendConfiguration, transport: Transport? = nil, socketFactory: SocketFactory? = nil) {
        backendID = configuration.id
        address = configuration.address
        if let socketFactory {
            self.socketFactory = socketFactory
        } else if transport == nil {
            self.socketFactory = { try BackendSocket.live($0) }
        } else {
            self.socketFactory = { _ in throw BackendFailure.local("detail.socketUnavailable") }
        }
        if let transport {
            self.session = nil
            self.transport = transport
        } else {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.timeoutIntervalForRequest = 15
            // Registration and bearer changes can outlast the ordinary read timeout.
            configuration.timeoutIntervalForResource = BackendRequestTimeout.connection.rawValue
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

    func license() async throws -> LicenseStatus? {
        do {
            let status: LicenseStatus = try await decode(["license"])
            guard status.deviceId.range(of: "^[0-9a-f]{32}$", options: .regularExpression) != nil else {
                throw BackendFailure.local("api.invalidResponse")
            }
            return status
        } catch let failure as BackendFailure where failure.source == .backend && failure.status == 404 && failure.code == "resource_not_found" {
            return nil
        }
    }

    func app() async throws -> AppInformation { try await decode(["app"]) }
    func capabilities() async throws -> Set<String> {
        let response: CapabilityResponse = try await decode(["capabilities"])
        return Set(response.features ?? [])
    }
    func otpRequirement() async throws -> Bool {
        let response: OTPRequirement = try await decode(["auth", "otp", "required"])
        return response.otpRequired
    }
    func sendOTP() async throws { _ = try await request(["auth", "otp"], method: "POST") }
    func verifyOTP(_ code: String) async throws -> String {
        let data = try JSONEncoder().encode(OTPVerification(code: code))
        let response: OTPToken = try await decode(["auth", "otp", "verify"], method: "POST", body: data)
        guard !response.token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw BackendFailure.local("api.invalidResponse")
        }
        return response.token
    }
    func modems(token: String?) async throws -> [Modem] { try await decode(["modems"], token: token) }
    func modem(_ id: String, token: String?) async throws -> Modem {
        let modem: Modem = try await decode(["modems", id], token: token)
        guard modem.id == id else { throw BackendFailure.local("api.invalidResponse") }
        return modem
    }
    func createPairing() async throws -> LicensePairing {
        let pairing: LicensePairing = try await decode(["license", "pairings"], method: "POST")
        _ = try pairing.validatedURL()
        guard pairing.status == "pending" else { throw BackendFailure.local("api.invalidResponse") }
        return pairing
    }
    func pairing(_ id: String) async throws -> LicensePairing {
        let pairing: LicensePairing = try await decode(["license", "pairings", id])
        _ = try pairing.validatedURL()
        guard pairing.id == id else { throw BackendFailure.local("api.invalidResponse") }
        return pairing
    }

    func decode<Value: Decodable & Sendable>(_ path: [String], method: String = "GET",
                                            body: Data? = nil, token: String? = nil,
                                            query: [URLQueryItem] = [],
                                            timeout: BackendRequestTimeout = .standard) async throws -> Value {
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
            throw BackendFailure(status: response.status, code: "invalid_response",
                                 message: NSLocalizedString("api.invalidResponse", comment: ""), requestID: response.requestID)
        }
    }

    func request(_ path: [String], method: String, body: Data? = nil, token: String? = nil,
                 query: [URLQueryItem] = [], accept: String = "application/json",
                 timeout: BackendRequestTimeout = .standard) async throws -> BackendHTTPResponse {
        let request = try makeRequest(path, method: method, body: body, token: token, query: query, accept: accept, timeout: timeout)
        let requestID = request.value(forHTTPHeaderField: "X-Request-ID") ?? UUID().uuidString
        try Task.checkCancellation()
        let started = ContinuousClock.now
        let response: BackendHTTPResponse
        do { response = try await transport(request) }
        catch {
            try Task.checkCancellation()
            throw BackendFailure.wrapping(error, source: .backend)
        }
        try Task.checkCancellation()
        // IDs and timings are useful across backends; bodies and credentials never enter logs.
        AppDiagnostics.record("backend=\(backendID) request=\(requestID) method=\(method) status=\(response.status) duration=\(started.duration(to: .now))")
        guard (200..<300).contains(response.status) else {
            throw HTTPProblem.failure(response, source: .backend)
        }
        return response
    }
    func makeRequest(_ path: [String], method: String = "GET", body: Data? = nil, token: String? = nil,
                     query: [URLQueryItem] = [], accept: String = "application/json",
                     timeout: BackendRequestTimeout = .standard) throws -> URLRequest {
        guard var components = URLComponents(url: address, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("address.invalid")
        }
        let segmentCharacters = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
        let encoded = try path.map { segment in
            guard let value = segment.addingPercentEncoding(withAllowedCharacters: segmentCharacters) else {
                throw BackendFailure.local("address.invalid")
            }
            return value
        }
        components.percentEncodedPath += "/" + encoded.joined(separator: "/")
        if !query.isEmpty { components.queryItems = query }
        guard let url = components.url else { throw BackendFailure.local("address.invalid") }
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout.rawValue)
        let requestID = UUID().uuidString
        request.setValue(requestID, forHTTPHeaderField: "X-Request-ID")
        request.httpMethod = method
        request.httpBody = body
        request.setValue("\(accept), \(HTTPProblem.mediaType)", forHTTPHeaderField: "Accept")
        request.setValue(Locale.preferredLanguages.first ?? "en", forHTTPHeaderField: "Accept-Language")
        if body != nil { request.setValue("application/json", forHTTPHeaderField: "Content-Type") }
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
        return try await socketFactory(request)
    }

}

import Foundation

final class PushGateway: Sendable {
    typealias Transport = @Sendable (URLRequest) async throws -> BackendHTTPResponse
    private let address: URL?
    private let session: URLSession?
    private let transport: Transport
    private let diagnostics: DiagnosticLogStore

    init(address: URL? = nil, diagnostics: DiagnosticLogStore = AppDiagnostics.store, transport: Transport? = nil) {
        self.diagnostics = diagnostics
        let configured = Bundle.main.object(forInfoDictionaryKey: "SigmoPushGatewayURL") as? String
        self.address = address ?? URL(string: configured ?? "https://sigmo.952007.xyz")
        if let transport {
            self.session = nil
            self.transport = transport
            return
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.urlCredentialStorage = nil
        let session = URLSession(configuration: configuration, delegate: PushGatewayRedirects(), delegateQueue: nil)
        self.session = session
        self.transport = { request in
            try await BackendHTTPResponse.receive(request, session: session)
        }
    }

    deinit { session?.invalidateAndCancel() }

    func register(identity: PushIdentity, deviceToken: String? = nil, voipToken: String? = nil, environment: String, label: String) async throws -> GatewayRegistration {
        struct Body: Encodable { let id, environment, label: String; let deviceToken, voipToken: String? }
        let data = try JSONEncoder().encode(Body(id: identity.id, environment: environment, label: label, deviceToken: deviceToken, voipToken: voipToken))
        return try await decode(["push-installations"], method: "POST", credential: identity.managementToken, body: data)
    }

    func register(binding: PushBinding, identity: PushIdentity, backendID: UUID? = nil) async throws -> GatewayRegistration {
        struct Body: Encodable {
            let id, installationId, backendId, sendToken: String
            let enabled, sms, reminders, calls: Bool
        }
        let body = Body(id: binding.id, installationId: identity.id, backendId: binding.backendID, sendToken: binding.sendToken,
                        enabled: binding.enabled, sms: binding.sms, reminders: binding.reminders, calls: binding.allowsCalls)
        return try await decode(["push-subscriptions"], method: "POST", credential: identity.managementToken, body: JSONEncoder().encode(body), backendID: backendID)
    }

    func revoke(_ subscriptionID: String, identity: PushIdentity) async throws {
        _ = try await request(["push-subscriptions", subscriptionID], method: "DELETE", credential: identity.managementToken)
    }

    func badge(identity: PushIdentity) async throws -> GatewayRegistration {
        try await decode(["push-installations", identity.id], credential: identity.managementToken)
    }

    private func decode(_ path: [String], method: String = "GET", credential: String, body: Data? = nil, backendID: UUID? = nil) async throws -> GatewayRegistration {
        let response = try await request(path, method: method, credential: credential, body: body, backendID: backendID)
        do { return try JSONDecoder().decode(GatewayRegistration.self, from: response.data) }
        catch {
            response.diagnosticRequest?.decodingFailed(error, response: response)
            throw error
        }
    }

    private func request(_ path: [String], method: String, credential: String, body: Data? = nil, backendID: UUID? = nil) async throws -> BackendHTTPResponse {
        guard let address, address.scheme == "https", address.host != nil, address.user == nil,
              address.password == nil, address.query == nil, address.fragment == nil else {
            throw BackendFailure.local("push.gatewayUnavailable")
        }
        var url = address.appending(path: "v1")
        for segment in path { url.append(path: segment) }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body
        request.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json, \(HTTPProblem.mediaType)", forHTTPHeaderField: "Accept")
        let diagnostic = DiagnosticRequest(store: diagnostics, service: .pushGateway, method: method, path: path, backendID: backendID)
        var received: BackendHTTPResponse?
        do {
            try Task.checkCancellation()
            var response: BackendHTTPResponse
            do { response = try await transport(request) }
            catch {
                try Task.checkCancellation()
                throw BackendFailure.wrapping(error, source: .pushGateway)
            }
            received = response
            guard (200..<300).contains(response.status) else {
                throw HTTPProblem.failure(response, source: .pushGateway)
            }
            try Task.checkCancellation()
            diagnostic?.completed(response)
            response.diagnosticRequest = diagnostic
            return response
        } catch {
            diagnostic?.failed(error, response: received)
            throw error
        }
    }
}

private final class PushGatewayRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        // Installation credentials are valid for the configured gateway resource only.
        completionHandler(nil)
    }
}

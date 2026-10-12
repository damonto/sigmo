import Foundation

// Closures make the bidirectional transport replaceable without mocking URLSession.
struct BackendSocket: Sendable {
    let send: @Sendable (Data) async throws -> Void
    let receive: @Sendable () async throws -> Data
    let close: @Sendable () -> Void

    static func live(_ request: URLRequest) throws -> BackendSocket {
        guard let url = request.url,
              var origin = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("address.invalid")
        }
        origin.scheme = origin.scheme == "wss" ? "https" : "http"
        guard let originURL = origin.url else { throw BackendFailure.local("address.invalid") }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 30
        // Interactive carrier flows can wait for user input for several minutes.
        configuration.timeoutIntervalForResource = 0
        configuration.httpCookieStorage = nil
        configuration.urlCredentialStorage = nil
        let session = URLSession(configuration: configuration, delegate: BackendRedirects(origin: originURL), delegateQueue: nil)
        let task = session.webSocketTask(with: request)
        task.resume()
        return BackendSocket(send: { data in
            try Task.checkCancellation()
            guard let text = String(data: data, encoding: .utf8) else { throw BackendFailure.local("api.invalidResponse") }
            try await task.send(.string(text))
        }, receive: {
            do {
                try Task.checkCancellation()
                switch try await task.receive() {
                case .data(let data): return data
                case .string(let text): return Data(text.utf8)
                @unknown default: throw BackendFailure.local("api.invalidResponse")
                }
            } catch {
                if let response = task.response as? HTTPURLResponse, !(200..<300).contains(response.statusCode), response.statusCode != 101 {
                    throw BackendFailure(status: response.statusCode, code: "socket_handshake",
                                         message: HTTPURLResponse.localizedString(forStatusCode: response.statusCode))
                }
                throw error
            }
        }, close: {
            task.cancel(with: .goingAway, reason: nil)
            session.invalidateAndCancel()
        })
    }
}

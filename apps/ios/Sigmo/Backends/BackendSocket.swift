import Foundation
import Synchronization

// Closures make the bidirectional transport replaceable without mocking URLSession.
struct BackendSocket: Sendable {
    let send: @Sendable (Data) async throws -> Void
    let receive: @Sendable () async throws -> Data
    let close: @Sendable () -> Void

    static func live(_ request: URLRequest, diagnostics: DiagnosticSocket) throws -> BackendSocket {
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
        let session = URLSession(configuration: configuration, delegate: SocketDelegate(origin: originURL, diagnostics: diagnostics), delegateQueue: nil)
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
        }).recording(diagnostics)
    }

    func recording(_ diagnostics: DiagnosticSocket) -> BackendSocket {
        BackendSocket(send: { data in
            do { try await send(data) }
            catch { diagnostics.failed(error); throw error }
        }, receive: {
            do { return try await receive() }
            catch { diagnostics.failed(error); throw error }
        }, close: {
            diagnostics.closed()
            close()
        })
    }
}

// Sending, receiving, delegate callbacks and explicit closing can race. Report a
// single terminal event, without ever inspecting a message or close reason.
final class DiagnosticSocket: Sendable {
    private let request: DiagnosticRequest?
    private let finished = Mutex(false)

    init(request: DiagnosticRequest?) { self.request = request }

    func opened() {
        finished.withLock { finished in
            if !finished { request?.record("socket.opened") }
        }
    }

    func failed(_ error: any Error, status: Int? = nil) {
        finished.withLock { finished in
            guard !finished else { return }
            finished = true
            request?.record(DiagnosticError.isCancellation(error) ? "operation.cancelled" : "socket.failed", error: error, status: status)
        }
    }

    func closed(code: Int? = nil) {
        finished.withLock { finished in
            guard !finished else { return }
            finished = true
            request?.record("socket.closed", extra: code.map { ["closeCode": String($0)] } ?? [:])
        }
    }
}

private final class SocketDelegate: NSObject, URLSessionWebSocketDelegate, Sendable {
    private let redirects: BackendRedirects
    private let diagnostics: DiagnosticSocket

    init(origin: URL, diagnostics: DiagnosticSocket) {
        redirects = BackendRedirects(origin: origin)
        self.diagnostics = diagnostics
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        redirects.urlSession(session, task: task, willPerformHTTPRedirection: response, newRequest: request, completionHandler: completionHandler)
    }

    func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didOpenWithProtocol protocol: String?) {
        diagnostics.opened()
    }

    func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask,
                    didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        diagnostics.closed(code: closeCode.rawValue)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        if let error { diagnostics.failed(error, status: (task.response as? HTTPURLResponse)?.statusCode) }
    }
}

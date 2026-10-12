import Foundation

enum FailureSource: String, Sendable {
    case local
    case backend
    case pushGateway
}

struct BackendHTTPResponse: Sendable {
    let data: Data
    let status: Int
    var requestID: String?
    var contentType: String?
    var retryAfter: String?
    var allow: String?
    var authenticate: String?

    // Reading headers first lets an oversized or interrupted error preserve its
    // HTTP meaning. Successful downloads retain their existing size policy.
    static func receive(_ request: URLRequest, session: URLSession) async throws -> BackendHTTPResponse {
        try await BackgroundExecution.run(name: "HTTP request") {
            try await receiveResponse(request, session: session)
        }
    }

    private static func receiveResponse(_ request: URLRequest, session: URLSession) async throws -> BackendHTTPResponse {
        let (bytes, response) = try await session.bytes(for: request)
        guard let response = response as? HTTPURLResponse else {
            bytes.task.cancel()
            throw BackendFailure.local("api.invalidResponse")
        }
        let isError = !(200..<300).contains(response.statusCode)
        var data = Data()
        do {
            for try await byte in bytes {
                if isError && data.count == HTTPProblem.maxBytes {
                    bytes.task.cancel()
                    data.removeAll()
                    break
                }
                data.append(byte)
            }
        } catch {
            try Task.checkCancellation()
            if !isError { throw error }
            data.removeAll()
        }
        try Task.checkCancellation()
        return BackendHTTPResponse(data: data, status: response.statusCode,
                                   requestID: response.value(forHTTPHeaderField: "X-Request-ID"),
                                   contentType: response.value(forHTTPHeaderField: "Content-Type"),
                                   retryAfter: response.value(forHTTPHeaderField: "Retry-After"),
                                   allow: response.value(forHTTPHeaderField: "Allow"),
                                   authenticate: response.value(forHTTPHeaderField: "WWW-Authenticate"))
    }
}

enum HTTPProblem {
    static let mediaType = "application/problem+json"
    static let typePrefix = "https://sigmo-pro.952007.xyz/problems/"
    static let maxBytes = 64 * 1024
    private static let catalog: [String: String] = {
        guard let url = Bundle.main.url(forResource: "HTTPProblems", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let value = try? JSONDecoder().decode([String: String].self, from: data) else { return [:] }
        return value
    }()

    struct Body: Codable, Sendable {
        let type: String
        let title: String
        let status: Int
        var detail: String?
        var error_code: String?
        var request_id: String?
    }

    static func failure(_ response: BackendHTTPResponse, source: FailureSource, now: Date = Date()) -> BackendFailure {
        var failure = BackendFailure(status: response.status, code: "http_\(response.status)",
                                     message: HTTPURLResponse.localizedString(forStatusCode: response.status),
                                     requestID: response.requestID, source: source, retryAfter: response.retryAfter,
                                     retryAt: retryDate(response.retryAfter, now: now), allow: response.allow,
                                     authenticate: response.authenticate)
        let contentType = response.contentType?.split(separator: ";").first?.trimmingCharacters(in: .whitespaces).lowercased()
        guard contentType == mediaType, response.data.count <= maxBytes,
              let body = try? JSONDecoder().decode(Body.self, from: response.data) else { return failure }
        failure.problemType = body.type
        guard body.type.hasPrefix(typePrefix) else { return failure }
        let code = String(body.type.dropFirst(typePrefix.count))
        guard catalog[code] != nil, body.status == response.status, !body.title.isEmpty,
              body.error_code == nil || body.error_code == code,
              response.requestID == nil || body.request_id == nil || body.request_id == "" || response.requestID == body.request_id else { return failure }
        failure.code = code
        let messageKey = "apiError.\(code)"
        let localized = NSLocalizedString(messageKey, comment: "")
        failure.message = localized == messageKey ? body.detail ?? failure.message : localized
        if failure.requestID == nil { failure.requestID = body.request_id }
        return failure
    }

    static func retryDate(_ value: String?, now: Date) -> Date? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        if value.allSatisfy({ $0.isASCII && $0.isNumber }), let seconds = Int32(value), seconds >= 0 {
            return now.addingTimeInterval(TimeInterval(seconds))
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.isLenient = false
        for format in ["EEE, dd MMM yyyy HH:mm:ss 'GMT'", "EEEE, dd-MMM-yy HH:mm:ss 'GMT'", "EEE MMM d HH:mm:ss yyyy"] {
            formatter.dateFormat = format
            if let date = formatter.date(from: value), date > now { return date }
        }
        return nil
    }

    // Fixtures use the same representation as live servers. This does not
    // translate or accept the removed legacy error JSON format.
    static func response(status: Int, code: String, detail: String, requestID: String? = nil) -> BackendHTTPResponse {
        let body = Body(type: typePrefix + code, title: catalog[code] ?? code,
                        status: status, detail: detail, error_code: code, request_id: requestID)
        // These concrete scalar fields are always JSON-encodable.
        let data = (try? JSONEncoder().encode(body)) ?? Data()
        return BackendHTTPResponse(data: data, status: status, requestID: requestID, contentType: mediaType)
    }
}

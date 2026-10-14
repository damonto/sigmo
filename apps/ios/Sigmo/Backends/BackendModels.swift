import Foundation
import Network

struct BackendConfiguration: Codable, Identifiable, Equatable, Sendable {
    let id: UUID
    var name: String
    var address: URL

    // Storage keeps the canonical API endpoint; people edit the server address.
    var displayAddress: String {
        guard var parts = URLComponents(url: address, resolvingAgainstBaseURL: false) else { return address.absoluteString }
        if parts.percentEncodedPath.hasSuffix("/api/v1") {
            parts.percentEncodedPath.removeLast("/api/v1".count)
        }
        return parts.string ?? address.absoluteString
    }

    init(id: UUID = UUID(), name: String, address: URL) {
        self.id = id
        self.name = name
        self.address = address
    }
}

struct BackendSettings: Codable, Equatable, Sendable {
    var version = 1
    // Array order is the user's order; no second ordering index to synchronize.
    var backends: [BackendConfiguration] = []
}

enum BackendDestination: Hashable {
    case management
    case backend(UUID)
    case settings(UUID)
    case modem(UUID, String)
    case messages(UUID, String, String)
    case reminder(UUID, String, String, String, String)

    var backendID: UUID? {
        switch self {
        case .management: nil
        case .backend(let id), .settings(let id), .modem(let id, _), .messages(let id, _, _), .reminder(let id, _, _, _, _): id
        }
    }
}

enum BackendAddress {
    static func normalize(_ input: String) throws -> URL {
        let value = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard var parts = URLComponents(string: value),
              let scheme = parts.scheme?.lowercased(), ["http", "https"].contains(scheme),
              let host = parts.host, !host.isEmpty,
              parts.user == nil, parts.password == nil,
              parts.query == nil, parts.fragment == nil,
              parts.port.map({ (1...65535).contains($0) }) ?? true else {
            throw BackendFailure.local("address.invalid")
        }
        let plainHost = host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if scheme == "http" && !allowsHTTP(host: plainHost) {
            throw BackendFailure.local("address.httpsRequired")
        }
        parts.scheme = scheme
        parts.host = host.lowercased()
        if parts.port == (scheme == "https" ? 443 : 80) { parts.port = nil }
        var path = parts.percentEncodedPath
        while path.hasSuffix("/") { path.removeLast() }
        guard !path.split(separator: "/").contains(where: {
            let segment = String($0).removingPercentEncoding
            return segment == "." || segment == ".."
        }) else {
            throw BackendFailure.local("address.invalid")
        }
        if !path.hasSuffix("/api/v1") { path += "/api/v1" }
        parts.percentEncodedPath = path
        guard let url = parts.url else { throw BackendFailure.local("address.invalid") }
        return url
    }

    static func allowsHTTP(host: String) -> Bool {
        let host = host.lowercased()
        return !host.contains(".") && !host.contains(":")
            || host.hasSuffix(".local")
            || IPv4Address(host) != nil || IPv6Address(host) != nil
    }

    static func sameOrigin(_ first: URL, _ second: URL) -> Bool {
        func port(_ url: URL) -> Int { url.port ?? (url.scheme == "https" ? 443 : 80) }
        return first.scheme?.lowercased() == second.scheme?.lowercased()
            && first.host?.lowercased() == second.host?.lowercased() && port(first) == port(second)
    }
}

struct BackendFailure: Error, LocalizedError, Equatable, Sendable {
    var status: Int?
    var code: String
    var message: String
    var requestID: String?
    var isNetwork = false
    var source: FailureSource = .local
    // Recovery policies need the original transport code after HTTP wrapping.
    var transportCode: URLError.Code?
    var problemType: String?
    var retryAfter: String?
    var retryAt: Date?
    var allow: String?
    var authenticate: String?

    var errorDescription: String? { message }
    var isLocalAuthenticationFailure: Bool {
        source == .backend && status == 401 && code == "invalid_session" && problemType == HTTPProblem.typePrefix + "invalid_session"
    }

    static func local(_ key: String) -> BackendFailure {
        BackendFailure(code: key, message: NSLocalizedString(key, comment: ""))
    }

    static func wrapping(_ error: Error, source: FailureSource? = nil) -> BackendFailure {
        if var failure = error as? BackendFailure {
            if let source { failure.source = source }
            return failure
        }
        if let error = error as? URLError {
            return BackendFailure(code: "network.\(error.code.rawValue)", message: error.localizedDescription,
                                  isNetwork: true, source: source ?? .local, transportCode: error.code)
        }
        return BackendFailure(code: "client_error", message: error.localizedDescription, source: source ?? .local)
    }
}

struct AppInformation: Codable, Sendable, Equatable {
    let version: String
    let commit: String
    let channel: String
    let edition: String
    let target: String
    let distribution: String
}

struct LicenseStatus: Codable, Sendable {
    let authorized: Bool
    let deviceId: String
}

struct LicensePairing: Codable, Sendable {
    let id: String
    let activationUrl: String
    var status: String
    let expiresAt: Date

    var statusKey: String {
        switch status {
        case "pending": "activation.pending"
        case "active": "activation.active"
        case "expired": "activation.expired"
        default: "license.unknown"
        }
    }

    func validatedURL() throws -> URL {
        guard !id.isEmpty, ["pending", "active", "expired"].contains(status),
              let parts = URLComponents(string: activationUrl),
              parts.scheme == "https", parts.host?.lowercased() == "t.me",
              parts.port == nil, parts.user == nil, parts.password == nil, parts.fragment == nil,
              parts.queryItems?.count == 1, parts.queryItems?.first?.name == "start",
              parts.queryItems?.first?.value == id,
              parts.path.range(of: #"^/[A-Za-z][A-Za-z0-9_]{1,28}[bB][oO][tT]$"#, options: .regularExpression) != nil,
              let url = parts.url else { throw BackendFailure.local("activation.invalidLink") }
        return url
    }
}

struct CapabilityResponse: Decodable, Sendable {
    let features: [String]?
}

struct BackendClock: Sendable {
    var now: @Sendable () -> Date = { Date() }
    var sleep: @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
}

import Foundation
import Observation

@MainActor @Observable
final class MCPState {
    let session: BackendSession
    private(set) var settings: MCPSettings?
    private(set) var endpoint = ""
    private(set) var clientConfiguration = ""
    private(set) var keys: [MCPAPIKey] = []
    private(set) var audit: [MCPAuditEvent] = []
    private(set) var modems: [Modem] = []
    private(set) var failure: BackendFailure?
    private(set) var busy = false
    private(set) var skillData: Data?
    var secret: MCPSecret?
    var exporting = false
    @ObservationIgnored private var revision = 0

    init(session: BackendSession) {
        self.session = session
    }

    static func endpoint(address: URL, path: String) throws -> URL {
        guard var parts = URLComponents(url: address, resolvingAgainstBaseURL: false),
              parts.percentEncodedPath.hasSuffix("/api/v1"), path.hasPrefix("/"), !path.hasPrefix("//"),
              let endpoint = URLComponents(string: path), endpoint.scheme == nil, endpoint.host == nil,
              endpoint.query == nil, endpoint.fragment == nil,
              !endpoint.path.split(separator: "/").contains(where: { $0 == "." || $0 == ".." }) else {
            throw BackendFailure.local("api.invalidResponse")
        }
        parts.percentEncodedPath = String(parts.percentEncodedPath.dropLast(7)) + endpoint.percentEncodedPath
        guard let url = parts.url, BackendAddress.sameOrigin(address, url) else {
            throw BackendFailure.local("api.invalidResponse")
        }
        return url
    }

    static func clientConfig(endpoint: String, token: String = "<API_KEY>") throws -> String {
        // Encoding also escapes quotes and backslashes in reverse-proxy URLs.
        struct Configuration: Encodable {
            struct Server: Encodable {
                let url: String
                let headers: [String: String]
            }
            let mcpServers: [String: Server]
        }
        let value = Configuration(mcpServers: ["sigmo": .init(url: endpoint, headers: ["Authorization": "Bearer \(token)"])])
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        let data = try encoder.encode(value)
        return String(decoding: data, as: UTF8.self)
    }

    func load() async {
        await perform { current in
            let (settings, keys, audit, modems) = try await self.session.protectedRequest { api, token in
                async let settings = api.mcpSettings(token: token)
                async let keys = api.mcpKeys(token: token)
                async let audit = api.mcpAudit(token: token)
                async let modems = api.modems(token: token)
                return try await (settings, keys, audit, modems)
            }
            try self.check(current)
            try self.apply(settings)
            self.keys = keys.apiKeys ?? []
            self.audit = audit.events ?? []
            self.modems = modems
        }
    }

    func setEnabled(_ enabled: Bool) async {
        await perform { current in
            let settings = try await self.session.protectedRequest { api, token in
                try await api.saveMCP(enabled, token: token)
            }
            try self.check(current)
            try self.apply(settings)
        }
    }

    func create(_ request: MCPKeyRequest) async {
        guard request.isValid else { return }
        var normalized = request
        normalized.name = request.name.trimmingCharacters(in: .whitespacesAndNewlines)
        if normalized.allModems { normalized.modemIds = [] }
        let payload = normalized
        await perform { current in
            let created = try await self.session.protectedRequest { api, token in
                try await api.createMCPKey(payload, token: token)
            }
            try self.check(current)
            self.keys.insert(created.apiKey, at: 0)
            let configuration = try Self.clientConfig(endpoint: self.endpoint, token: created.token)
            self.secret = MCPSecret(apiKey: created.apiKey, token: created.token, clientConfiguration: configuration)
        }
    }

    func revoke(_ id: String) async {
        await perform { current in
            try await self.session.protectedRequest { api, token in
                try await api.revokeMCPKey(id, token: token)
            }
            try self.check(current)
            if let index = self.keys.firstIndex(where: { $0.id == id }) {
                self.keys[index].status = "revoked"
            }
        }
    }

    func download() async {
        await perform { current in
            let data = try await self.session.protectedRequest { api, token in
                try await api.skill(token: token)
            }
            try self.check(current)
            self.skillData = data
            self.exporting = true
        }
    }

    func finishExport(_ result: Result<URL, Error>) {
        discardExport()
        if case .failure(let error) = result { failure = .wrapping(error) }
    }

    func discardExport() {
        skillData = nil
        exporting = false
    }

    func clear() {
        revision += 1
        busy = false
        settings = nil
        endpoint = ""
        clientConfiguration = ""
        keys = []
        audit = []
        modems = []
        secret = nil
        discardExport()
        failure = nil
    }

    private func apply(_ settings: MCPSettings) throws {
        let endpoint = try Self.endpoint(address: session.configuration.address, path: settings.endpointPath).absoluteString
        let configuration = try Self.clientConfig(endpoint: endpoint)
        self.endpoint = endpoint
        clientConfiguration = configuration
        self.settings = settings
    }

    private func check(_ current: Int) throws {
        try Task.checkCancellation()
        guard current == revision else { throw CancellationError() }
    }

    private func perform(_ operation: (Int) async throws -> Void) async {
        guard !busy, !Task.isCancelled else { return }
        let current = revision
        busy = true
        failure = nil
        defer { if current == revision { busy = false } }
        do {
            try await operation(current)
        } catch {
            guard current == revision else { return }
            if !session.canAccessDevices { clear() }
            guard !Task.isCancelled, !(error is CancellationError) else { return }
            failure = .wrapping(error)
        }
    }
}

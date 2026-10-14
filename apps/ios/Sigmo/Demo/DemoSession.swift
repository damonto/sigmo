import Foundation
import SwiftUI

extension EnvironmentValues {
    @Entry var demoSession: DemoSession? = nil
}

// A separate store keeps every demo write, credential and recording out of the
// user's connections. Presentation never changes the app delegate's live store.
@MainActor
final class DemoSession: Identifiable {
    let id = UUID()
    let store: BackendStore
    let calls: CallsState
    let defaults: UserDefaults
    private let directory: URL
    private let defaultsName: String
    private let connections: DemoConnections
    private var notificationPreferences: [UUID: DemoNotificationPreferences] = [:]
    private var stopping: Task<Void, Never>?

    init() throws {
        let directory = URL.temporaryDirectory.appending(path: "SigmoDemo-\(id.uuidString)", directoryHint: .isDirectory)
        self.directory = directory
        defaultsName = "SigmoDemo.\(id.uuidString)"
        guard let defaults = UserDefaults(suiteName: defaultsName) else { throw BackendFailure.local("demo.startFailed") }
        self.defaults = defaults
        let primary = BackendConfiguration(name: "Demo Home", address: URL(string: "https://home.demo.invalid/api/v1")!)
        let secondary = BackendConfiguration(name: "Demo Travel", address: URL(string: "https://travel.demo.invalid/api/v1")!)
        let home = try DemoBackend(id: primary.id)
        let travel = try DemoBackend(id: secondary.id, physical: true)
        let connections = DemoConnections(servers: [primary.id: home, secondary.id: travel])
        self.connections = connections
        let file = ConfigurationFile(url: directory.appending(path: "backends.json"))
        try file.save(BackendSettings(backends: [primary, secondary]))
        var tokens: [UUID: String] = [:]
        let credentials = BackendCredentials(read: { tokens[$0] }, write: { tokens[$0] = $1 })
        let store = BackendStore(file: file, credentials: credentials, makeAPI: { configuration in
            connections.api(for: configuration)
        })
        self.store = store
        calls = CallsState(store: store, demo: true, clientID: "demo-\(id.uuidString)",
                           recordings: CallRecordings(directory: directory.appending(path: "Recordings")),
                           contacts: CallContacts(status: { .denied }, readNames: { [] }, requestAccess: {}))
    }

    func receiveMessage() async throws {
        guard stopping == nil else { throw CancellationError() }
        await store.refreshAll()
        let backend = try eventBackend()
        try await backend.sampleMessage()
        await store.refreshUnreadCounts()
    }

    func receiveCall() async throws {
        guard stopping == nil else { throw CancellationError() }
        await store.refreshAll()
        let backend = try eventBackend()
        try await backend.sampleCall()
    }

    private func eventBackend() throws -> DemoBackend {
        try Task.checkCancellation()
        guard stopping == nil else { throw CancellationError() }
        guard let id = store.onlineSessions.first?.id, let backend = connections.servers[id] else {
            throw BackendFailure.local("demo.noConnection")
        }
        return backend
    }

    func notifications(for connectionID: UUID) -> DemoNotificationPreferences {
        if let preferences = notificationPreferences[connectionID] { return preferences }
        let preferences = DemoNotificationPreferences()
        notificationPreferences[connectionID] = preferences
        return preferences
    }

    func stop() async {
        if let stopping { await stopping.value; return }
        store.stop()
        let task = Task {
            await calls.stopDemo()
            await connections.stop()
            defaults.removePersistentDomain(forName: defaultsName)
            do { try FileManager.default.removeItem(at: directory) }
            catch { store.failure = .wrapping(error) }
        }
        stopping = task
        await task.value
    }
}

// Connection identity, rather than an editable address, selects local state.
@MainActor
private final class DemoConnections {
    private(set) var servers: [UUID: DemoBackend]
    private var stopped = false

    init(servers: [UUID: DemoBackend]) { self.servers = servers }

    func api(for configuration: BackendConfiguration) -> BackendAPI {
        do {
            guard !stopped else { throw CancellationError() }
            let backend: DemoBackend
            if let existing = servers[configuration.id] { backend = existing }
            else {
                backend = try DemoBackend(id: configuration.id)
                servers[configuration.id] = backend
            }
            return BackendAPI(configuration: configuration, transport: { request in
                try await backend.respond(Self.localRequest(request, address: configuration.address))
            }, socketFactory: { request in
                try await backend.socket(Self.localRequest(request, address: configuration.address))
            }, isDemo: true)
        } catch {
            let failure = BackendFailure.wrapping(error)
            // Construction errors must also stay local, never use the live defaults.
            return BackendAPI(configuration: configuration, transport: { _ in throw failure },
                              socketFactory: { _ in throw failure }, isDemo: true)
        }
    }

    // Editing a connection's host or reverse-proxy prefix keeps its local state.
    // Preserve encoded resource IDs and query parameters while adapting the API root.
    nonisolated private static func localRequest(_ request: URLRequest, address: URL) throws -> URLRequest {
        guard let url = request.url,
              var components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let root = URLComponents(url: address, resolvingAgainstBaseURL: false) else {
            throw BackendFailure.local("demo.unsupportedRequest")
        }
        let prefix = root.percentEncodedPath + "/"
        guard components.percentEncodedPath.hasPrefix(prefix) else {
            throw BackendFailure.local("demo.unsupportedRequest")
        }
        components.percentEncodedPath = "/api/v1/" + components.percentEncodedPath.dropFirst(prefix.count)
        guard let localURL = components.url else { throw BackendFailure.local("demo.unsupportedRequest") }
        var local = request
        local.url = localURL
        return local
    }

    func stop() async {
        stopped = true
        for backend in servers.values { await backend.stop() }
        servers.removeAll()
    }
}

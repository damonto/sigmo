import Foundation
import Observation

@MainActor @Observable
final class BackendStore {
    private(set) var settings = BackendSettings()
    private(set) var sessions: [UUID: BackendSession] = [:]
    var failure: BackendFailure?
    private(set) var configurationLoaded = false
    private(set) var foreground = true

    @ObservationIgnored private let file: ConfigurationFile
    @ObservationIgnored private let credentials: BackendCredentials
    @ObservationIgnored private let clock: BackendClock
    @ObservationIgnored private let makeAPI: (BackendConfiguration) -> BackendAPI

    init(file: ConfigurationFile = .application, credentials: BackendCredentials = .keychain,
         clock: BackendClock = BackendClock(), makeAPI: @escaping (BackendConfiguration) -> BackendAPI = { BackendAPI(configuration: $0) }) {
        self.file = file
        self.credentials = credentials
        self.clock = clock
        self.makeAPI = makeAPI
        reloadConfiguration()
    }

    var onlineSessions: [BackendSession] {
        orderedSessions().filter(\.isOnline)
    }

    func orderedSessions(backendID: UUID? = nil) -> [BackendSession] {
        settings.backends.compactMap { backend in
            guard backendID == nil || backendID == backend.id else { return nil }
            return sessions[backend.id]
        }
    }

    func reloadConfiguration() {
        do {
            let loaded = try file.load()
            stop()
            settings = loaded
            sessions = Dictionary(uniqueKeysWithValues: loaded.backends.map { ($0.id, makeSession($0)) })
            configurationLoaded = true
            failure = nil
        } catch {
            AppDiagnostics.record(error, category: "storage", operation: "configuration.load.failed")
            configurationLoaded = false
            failure = .wrapping(error)
        }
    }

    @discardableResult
    func saveBackend(id: UUID? = nil, name: String, address: String) -> Bool {
        do {
            guard configurationLoaded else { throw BackendFailure.local("storage.invalidConfiguration") }
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { throw BackendFailure.local("configuration.nameRequired") }
            let endpoint = try BackendAddress.normalize(address)
            var next = settings
            let backendID = id ?? UUID()
            if let index = next.backends.firstIndex(where: { $0.id == backendID }) {
                next.backends[index].name = name
                next.backends[index].address = endpoint
            } else {
                next.backends.append(BackendConfiguration(id: backendID, name: name, address: endpoint))
            }
            return persist(next)
        } catch {
            AppDiagnostics.record(error, category: "storage", operation: "configuration.save.failed")
            failure = .wrapping(error)
            return false
        }
    }

    func deleteBackend(_ id: UUID) {
        var next = settings
        next.backends.removeAll { $0.id == id }
        persist(next)
    }

    func moveBackends(from: IndexSet, to: Int) {
        var next = settings
        next.backends = moved(next.backends, from: from, to: to)
        persist(next)
    }

    private func moved<Element>(_ items: [Element], from: IndexSet, to: Int) -> [Element] {
        let selected = from.map { items[$0] }
        var remaining = items.enumerated().filter { !from.contains($0.offset) }.map(\.element)
        remaining.insert(contentsOf: selected, at: to - from.filter { $0 < to }.count)
        return remaining
    }

    @discardableResult
    private func persist(_ next: BackendSettings) -> Bool {
        guard configurationLoaded else { failure = .local("storage.invalidConfiguration"); return false }
        let invalidated = sessions.values.filter { session in
            !next.backends.contains { $0.id == session.id && $0.address == session.configuration.address }
        }
        var removedCredentials: [(UUID, String?)] = []
        do {
            // Both stores are synchronous on the main actor, so no request can
            // publish a credential between removal and the configuration commit.
            // Delay live-session invalidation until both writes have succeeded.
            for session in invalidated {
                let saved = try credentials.read(session.id)
                try credentials.write(session.id, nil)
                removedCredentials.append((session.id, saved))
            }
            try file.save(next)
        } catch {
            AppDiagnostics.record(error, category: "storage", operation: "configuration.persist.failed")
            failure = .wrapping(error)
            for (id, saved) in removedCredentials {
                do { try credentials.write(id, saved) }
                catch {
                    AppDiagnostics.record(error, category: "storage", operation: "credentials.restore.failed", backendID: id)
                    // A failed compensation must fail closed rather than leave
                    // an in-memory login whose persisted credential was lost.
                    sessions[id]?.discardCredentials()
                    failure = .wrapping(error)
                }
            }
            return false
        }
        for session in invalidated { session.discardCredentials() }
        for (id, session) in sessions where !next.backends.contains(where: { $0.id == id }) {
            session.stop()
            sessions[id] = nil
        }
        for backend in next.backends {
            if let current = sessions[backend.id], current.configuration.address == backend.address {
                current.rename(backend.name)
            } else {
                sessions[backend.id]?.stop()
                sessions[backend.id] = makeSession(backend)
            }
        }
        settings = next
        failure = nil
        return true
    }

    private func makeSession(_ backend: BackendConfiguration) -> BackendSession {
        let session = BackendSession(configuration: backend, credentials: credentials, api: makeAPI(backend), clock: clock)
        session.foreground = foreground
        return session
    }

    func setForeground(_ foreground: Bool) {
        self.foreground = foreground
        for session in sessions.values { session.foreground = foreground }
    }

    func refreshAll(force: Bool = true) async {
        let current = Array(sessions.values)
        await withTaskGroup(of: Void.self) { group in
            for session in current { group.addTask { await session.refresh(force: force) } }
        }
    }

    func refreshUnreadCounts() async {
        let current = Array(sessions.values)
        await withTaskGroup(of: Void.self) { group in
            for session in current {
                group.addTask { await session.refreshUnreadCounts() }
            }
        }
    }

    func stop() { for session in sessions.values { session.stop() } }
}

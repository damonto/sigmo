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
    @ObservationIgnored private let gate = RefreshGate()
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
                if next.backends[index].address != endpoint { try sessions[backendID]?.removeCredentials() }
                next.backends[index].name = name
                next.backends[index].address = endpoint
            } else {
                next.backends.append(BackendConfiguration(id: backendID, name: name, address: endpoint))
            }
            return persist(next)
        } catch { failure = .wrapping(error); return false }
    }

    func deleteBackend(_ id: UUID) {
        do {
            try sessions[id]?.removeCredentials()
            var next = settings
            next.backends.removeAll { $0.id == id }
            persist(next)
        } catch { failure = .wrapping(error) }
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
        do {
            try file.save(next)
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
        } catch { failure = .wrapping(error); return false }
    }

    private func makeSession(_ backend: BackendConfiguration) -> BackendSession {
        let session = BackendSession(configuration: backend, credentials: credentials, api: makeAPI(backend), clock: clock, gate: gate)
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
        let gate = gate
        await withTaskGroup(of: Void.self) { group in
            for session in current {
                group.addTask {
                    do { try await gate.run { await session.refreshUnreadCounts() } }
                    catch { /* The caller can cancel queued badge reads. */ }
                }
            }
        }
    }

    func stop() { for session in sessions.values { session.stop() } }
}

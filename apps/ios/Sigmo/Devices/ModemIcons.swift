import Foundation
import Observation

struct ModemIconIdentity: Equatable {
    let id: String
    let sim: ModemSIMIdentity
    let locked: Bool

    init(_ modem: Modem) {
        id = modem.id
        sim = ModemSIMIdentity(modem)
        locked = modem.state == "locked"
    }

    var eligible: Bool { sim.kind == "euicc" && !sim.iccid.isEmpty && !locked }
}

/// Owned by one backend session so identical modem IDs cannot cross accounts.
@MainActor @Observable
final class ModemIcons {
    private(set) var revision = 0
    private struct Entry {
        let identity: ModemIconIdentity
        var icon: String?
        var pending: Bool
    }
    private struct Request {
        let id: UUID
        let task: Task<Void, Never>
    }
    private var entries: [String: Entry] = [:]
    @ObservationIgnored private var requests: [String: Request] = [:]

    func icon(for modem: Modem) -> String? {
        let identity = ModemIconIdentity(modem)
        guard identity.eligible, entries[modem.id]?.identity == identity else { return nil }
        return entries[modem.id]?.icon
    }

    func isPending(_ modem: Modem) -> Bool {
        let identity = ModemIconIdentity(modem)
        guard identity.eligible else { return false }
        guard let entry = entries[modem.id], entry.identity == identity else { return true }
        return entry.pending
    }

    func reset() {
        revision += 1
        for request in requests.values { request.task.cancel() }
        requests.removeAll()
        entries.removeAll()
    }

    func retain(_ modems: [Modem]) {
        let identities = modems.map(ModemIconIdentity.init).filter(\.eligible)
        for id in Array(entries.keys) {
            guard let entry = entries[id], !identities.contains(entry.identity) else { continue }
            requests.removeValue(forKey: id)?.task.cancel()
            entries.removeValue(forKey: id)
        }
    }

    func load(_ modem: Modem, session: BackendSession) async {
        let identity = ModemIconIdentity(modem)
        guard identity.eligible, session.canAccessDevices, !Task.isCancelled,
              session.modems.contains(where: { ModemIconIdentity($0) == identity }) else { return }
        while let entry = entries[modem.id], entry.identity == identity {
            if !entry.pending { return }
            guard let request = requests[modem.id] else { break }
            await request.task.value
            guard !Task.isCancelled, session.canAccessDevices,
                  session.modems.contains(where: { ModemIconIdentity($0) == identity }) else { return }
            // A row can reappear while its old, cancelled request is finishing.
            // Start a replacement if that request removed the pending entry.
        }
        requests.removeValue(forKey: modem.id)?.task.cancel()
        entries[modem.id] = Entry(identity: identity, pending: true)
        let requestID = UUID()
        let task = Task { [weak self] in
            guard let self else { return }
            defer {
                if requests[modem.id]?.id == requestID {
                    requests.removeValue(forKey: modem.id)
                    if entries[modem.id]?.pending == true { entries.removeValue(forKey: modem.id) }
                }
            }
            var icon: String?
            do {
                let profiles = try await session.protectedRequest { api, token in
                    try await api.profiles(modem.id, token: token)
                }
                let matches = profiles.ses.flatMap(\.profiles).filter {
                    $0.iccid == identity.sim.iccid && $0.enabled
                }
                if matches.count == 1 { icon = matches.first?.icon }
            } catch {
                // This optional read uses the session's authentication handling,
                // while unavailable eUICCs simply use the public logo or flag.
            }
            guard !Task.isCancelled, requests[modem.id]?.id == requestID,
                  session.canAccessDevices,
                  session.modems.contains(where: { ModemIconIdentity($0) == identity }) else { return }
            entries[modem.id] = Entry(identity: identity, icon: icon, pending: false)
        }
        requests[modem.id] = Request(id: requestID, task: task)
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }
}

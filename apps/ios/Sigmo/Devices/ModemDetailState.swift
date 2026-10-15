import Foundation
import Observation

struct ModemSIMIdentity: Equatable {
    let kind: String
    let slot: Int
    let iccid: String

    init(_ modem: Modem) {
        kind = modem.simKind
        slot = modem.sim.slot
        iccid = modem.sim.identifier
    }
}

@MainActor @Observable
final class ModemDetailState {
    var deletedHistoryProfileID: String?
    let session: BackendSession
    let modemID: String
    private(set) var elements: [SecureElement] = []
    private(set) var groups: [ESIMProfileGroup] = []
    private(set) var elementsLoaded = false
    private(set) var profilesLoaded = false
    private(set) var loading = false
    private(set) var busy = false
    private(set) var failure: BackendFailure?
    private(set) var elementsFailure: BackendFailure?
    private(set) var profilesFailure: BackendFailure?
    private(set) var message: String?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var loadedSIM: ModemSIMIdentity?
    @ObservationIgnored private var profilesLoadedAt = Date.distantPast

    init(session: BackendSession, modemID: String) {
        self.session = session
        self.modemID = modemID
    }
    var modem: Modem? { session.modems.first { $0.id == modemID } }
    var simIdentity: ModemSIMIdentity? { modem.map(ModemSIMIdentity.init) }
    var esim: Bool { modem?.simKind == "euicc" }
    var needsSIMUnlock: Bool { modem?.state == "locked" }
    var needsPIN: Bool { modem?.state == "locked" && modem?.unlockSupported == true }
    var canUseWiFi: Bool { session.capabilities?.contains("wifiCalling") == true }
    var canTransfer: Bool { session.capabilities?.contains("esimTransfer") == true }
    var canInstall: Bool { esim && !elements.isEmpty && elementsFailure == nil && !loading && !busy && !needsSIMUnlock }

    func load(force: Bool = true) async {
        guard !loading, !busy, !Task.isCancelled, session.canAccessDevices else { return }
        loading = true
        let current = revision
        defer {
            if current == revision { loading = false }
        }
        await loadSnapshot(current, force: force)
    }

    private func loadSnapshot(_ current: Int, force: Bool = true) async {
        guard current == revision, !Task.isCancelled else { return }
        await session.refreshModem(modemID)
        guard current == revision, !Task.isCancelled else { return }
        guard session.canAccessDevices else {
            clear()
            return
        }
        if let error = session.detailFailures[modemID] {
            failure = error
            if error.status == 404 { clearSIMData() }
            return
        }

        let identity = simIdentity
        if loadedSIM?.slot != identity?.slot || loadedSIM?.kind != identity?.kind {
            clearSIMData()
        }
        let unchanged = loadedSIM == identity
        loadedSIM = identity
        failure = nil
        guard esim else { return }
        if !force, unchanged, profilesLoaded, elementsLoaded, elementsFailure == nil,
           profilesFailure == nil, Date().timeIntervalSince(profilesLoadedAt) < 30 { return }
        await loadElements(current)
        guard current == revision, !Task.isCancelled else { return }
        guard session.canAccessDevices else {
            clear()
            return
        }
        await loadProfiles(current)
        if current == revision, !session.canAccessDevices { clear() }
    }

    private func loadElements(_ current: Int) async {
        do {
            let id = modemID
            let result = try await session.protectedRequest { api, token in try await api.secureElements(id, token: token) }
            guard current == revision, esim, !Task.isCancelled else { return }
            elements = result.ses
            elementsFailure = nil
            elementsLoaded = true
        } catch {
            if current == revision, !Task.isCancelled {
                elementsFailure = .wrapping(error)
                elementsLoaded = true
            }
        }
    }

    private func loadProfiles(_ current: Int) async {
        do {
            let id = modemID
            let result = try await session.protectedRequest { api, token in try await api.profiles(id, token: token) }
            guard current == revision, esim, !Task.isCancelled else { return }
            groups = result.ses
            profilesFailure = nil
            profilesLoaded = true
            profilesLoadedAt = Date()
        } catch {
            if current == revision, !Task.isCancelled {
                profilesFailure = .wrapping(error)
                profilesLoaded = true
            }
        }
    }

    func clear() {
        revision += 1
        clearSIMData()
        deletedHistoryProfileID = nil
        failure = nil
        message = nil
        loading = false
        busy = false
    }

    private func clearSIMData() {
        loadedSIM = nil
        elements = []
        groups = []
        elementsLoaded = false
        profilesLoaded = false
        elementsFailure = nil
        profilesFailure = nil
    }

    func mutate(_ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Void) async -> Bool {
        guard !busy, !Task.isCancelled, session.canAccessDevices else { return false }
        revision += 1
        let current = revision
        loading = false
        busy = true
        defer {
            if current == revision { busy = false }
        }
        failure = nil
        message = nil
        await session.invalidateModemReads(modemID)
        guard current == revision, !Task.isCancelled else { return false }
        do {
            try await session.protectedRequest(operation)
            guard current == revision, !Task.isCancelled else { return false }
            await session.invalidateModemReads(modemID)
            session.invalidateCommunicationLines()
            // Keep the write locked until its authoritative snapshot has been reconciled.
            await loadSnapshot(current)
            guard current == revision, !Task.isCancelled else { return false }
            message = String(localized: "detail.saved")
            return true
        } catch {
            guard current == revision else { return false }
            if !session.canAccessDevices {
                clear()
            } else if !Task.isCancelled {
                failure = .wrapping(error)
            }
            return false
        }
    }

    func unlock(_ pin: String) async -> Bool {
        let id = modemID
        return await mutate { api, token in
            try await api.detailMutation(id, path: ["sim-unlocks"], method: "POST", value: ["pin": pin.trimmingCharacters(in: .whitespacesAndNewlines)], token: token)
        }
    }
    func switchSlot(_ slot: Int) async -> Bool {
        let id = modemID
        return await mutate { api, token in try await api.detailMutation(id, path: ["sim-slots", String(slot)], method: "PUT", token: token) }
    }
    func enable(_ profile: ESIMProfile) async -> Bool {
        guard let profile = currentProfile(profile.id), !profile.enabled else { return false }
        let id = modemID
        return await mutate { api, token in try await api.detailMutation(id, path: ["ses", profile.seId, "esims", profile.iccid, "activation"], method: "PUT", token: token) }
    }
    func delete(_ profile: ESIMProfile) async -> Bool {
        guard let profile = currentProfile(profile.id), !profile.enabled else { return false }
        let id = modemID
        let deleted = await mutate { api, token in
            try await api.detailMutation(id, path: ["ses", profile.seId, "esims", profile.iccid], method: "DELETE", token: token)
        }
        guard deleted else { return false }
        deletedHistoryProfileID = profile.iccid
        // DELETE already confirmed removal, even if the following snapshot refresh failed.
        for index in groups.indices {
            groups[index].profiles.removeAll { $0.id == profile.id }
        }
        return true
    }
    static func validNickname(_ value: String) -> Bool {
        let name = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return !name.isEmpty && name.utf8.count <= 64
    }

    func currentProfile(_ id: String) -> ESIMProfile? {
        for group in groups {
            if let profile = group.profiles.first(where: { $0.id == id }) { return profile }
        }
        return nil
    }

    func isActiveProfile(_ id: String) -> Bool {
        guard let profile = currentProfile(id), profile.enabled,
              let identity = simIdentity, identity.kind == "euicc", !identity.iccid.isEmpty else { return false }
        return profile.iccid == identity.iccid
    }

    func rename(_ profile: ESIMProfile, name: String) async -> Bool {
        guard Self.validNickname(name) else { return false }
        let id = modemID
        return await mutate { api, token in
            try await api.detailMutation(id, path: ["ses", profile.seId, "esims", profile.iccid, "nickname"], method: "PUT",
                                         value: ["nickname": name.trimmingCharacters(in: .whitespacesAndNewlines)], token: token)
        }
    }
    func updateNumber(_ number: String) async -> Bool {
        let value = PhoneNumberDisplay.submission(number, regionCode: modem?.sim.regionCode)
        guard !value.isEmpty else { return false }
        let id = modemID
        return await mutate { api, token in try await api.detailMutation(id, path: ["msisdn"], method: "PUT", value: ["number": value], token: token) }
    }
    func reminderPath(profile: ESIMProfile?) -> [String]? {
        if let profile { return ["ses", profile.seId, "esims", profile.iccid, "reminder"] }
        guard let iccid = modem?.sim.identifier, !iccid.isEmpty else { return nil }
        return ["sims", iccid, "reminder"]
    }
    func saveReminder(profile: ESIMProfile?, value: ReminderSubmission?) async -> Bool {
        guard let path = reminderPath(profile: profile) else { return false }
        if let value, !value.isValid() {
            failure = .local("detail.invalidReminder")
            return false
        }
        let id = modemID
        return await mutate { api, token in
            if let value { try await api.detailMutation(id, path: path, method: "PUT", value: value, token: token) }
            else { try await api.detailMutation(id, path: path, method: "DELETE", token: token) }
        }
    }

}

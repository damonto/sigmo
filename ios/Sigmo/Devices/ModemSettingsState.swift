import Foundation
import Observation

private enum ModemSettingsSnapshot: Sendable {
    case airplane(ModemAirplaneMode)
    case mode(ModemNetworkModes)
    case bands(ModemNetworkBands)
    case device(ModemDeviceSettings)
}

@MainActor @Observable
final class ModemSettingsState {
    let session: BackendSession
    let modemID: String
    private(set) var airplane: ModemAirplaneMode?
    private(set) var modes: ModemNetworkModes?
    private(set) var bands: ModemNetworkBands?
    private(set) var device: ModemDeviceSettings?
    private(set) var loading: Set<ModemSettingsSection> = []
    private(set) var failures: [ModemSettingsSection: BackendFailure] = [:]
    private(set) var busy = false
    private(set) var activeSection: ModemSettingsSection?
    private(set) var failure: BackendFailure?
    // The value records whether the write received a successful HTTP response.
    private(set) var confirmationsPending: [ModemSettingsSection: Bool] = [:]
    private(set) var modemRefreshPending = false
    private var lastWriteAcknowledged = false
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var readRevisions: [ModemSettingsSection: Int] = [:]

    init(session: BackendSession, modemID: String) {
        self.session = session
        self.modemID = modemID
    }

    var modem: Modem? { session.modems.first { $0.id == modemID } }
    var hasAccess: Bool { session.canAccessDevices && modem != nil }
    var needsConfirmation: Bool { !confirmationsPending.isEmpty || modemRefreshPending }
    var writeAcknowledged: Bool { lastWriteAcknowledged && !confirmationsPending.values.contains(false) }
    var canChangeAirplane: Bool {
        ready(.airplane) && airplane?.supported == true && radioConfirmed
    }
    var canChangeMode: Bool {
        radioAvailable && ready(.mode) && (modes?.supported.count ?? 0) > 1
    }
    var canChangeBands: Bool {
        radioAvailable && ready(.bands) && bands?.supported.isEmpty == false
    }
    var canChangeDevice: Bool {
        ready(.device) && device != nil && confirmationsPending[.device] == nil
    }
    var radioAvailable: Bool {
        ready(.airplane) && airplane?.enabled == false && radioConfirmed
    }
    private var radioConfirmed: Bool {
        confirmationsPending[.airplane] == nil && confirmationsPending[.mode] == nil && confirmationsPending[.bands] == nil
    }

    private func ready(_ section: ModemSettingsSection) -> Bool {
        hasAccess && !busy && !loading.contains(section) && failures[section] == nil
    }

    func canSaveBands(_ values: Set<ModemBandValue>) -> Bool {
        guard canChangeBands, let bands, !values.isEmpty else { return false }
        return values != Set(bands.current) && values.isSubset(of: Set(bands.supported.map(\.value)))
    }

    func canSaveDevice(_ draft: ModemDeviceDraft) -> Bool {
        canChangeDevice && draft.submission != nil && draft.submission != device
    }

    func load(_ sections: Set<ModemSettingsSection> = Set(ModemSettingsSection.allCases)) async {
        guard !busy, !Task.isCancelled else { return }
        guard hasAccess else { clear(); return }
        let current = revision
        await readSections(sections, revision: current)
        guard current == revision, !Task.isCancelled else { return }
        await refreshModem(revision: current)
    }

    private func readSections(_ sections: Set<ModemSettingsSection>, revision: Int) async {
        await withTaskGroup(of: Void.self) { group in
            for section in sections {
                group.addTask { await self.read(section, revision: revision) }
            }
        }
    }

    private func read(_ section: ModemSettingsSection, revision current: Int) async {
        guard current == revision, hasAccess, !Task.isCancelled else { return }
        let readID = (readRevisions[section] ?? 0) + 1
        readRevisions[section] = readID
        loading.insert(section)
        defer {
            if current == revision, readRevisions[section] == readID { loading.remove(section) }
        }
        let id = modemID
        do {
            let snapshot: ModemSettingsSnapshot = try await session.protectedRequest { api, token in
                switch section {
                case .airplane: return .airplane(try await api.modemAirplaneMode(id, token: token))
                case .mode: return .mode(try await api.modemModes(id, token: token))
                case .bands: return .bands(try await api.modemBands(id, token: token))
                case .device: return .device(try await api.modemDeviceSettings(id, token: token))
                }
            }
            guard current == revision, readRevisions[section] == readID, hasAccess, !Task.isCancelled else { return }
            try apply(snapshot)
            failures[section] = nil
            confirmationsPending[section] = nil
        } catch {
            guard current == revision, readRevisions[section] == readID, !Task.isCancelled else { return }
            guard hasAccess else { clear(); return }
            guard !(error is CancellationError) else { return }
            failures[section] = .wrapping(error)
        }
    }

    private func apply(_ snapshot: ModemSettingsSnapshot) throws {
        switch snapshot {
        case .airplane(let value):
            airplane = value
        case .mode(let value):
            let options = Set(value.supported.map(\.id))
            guard options.count == value.supported.count,
                  options.isEmpty || options.contains(value.current.id) else {
                throw BackendFailure.local("api.invalidResponse")
            }
            modes = value
        case .bands(let value):
            let supported = Set(value.supported.map(\.value)), current = Set(value.current)
            guard supported.count == value.supported.count, current.count == value.current.count,
                  current.isSubset(of: supported), supported.isEmpty || !current.isEmpty else {
                throw BackendFailure.local("api.invalidResponse")
            }
            bands = value
        case .device(let value):
            device = value
        }
    }

    private func refreshModem(revision current: Int) async {
        guard current == revision, !Task.isCancelled else { return }
        await session.refreshModem(modemID)
        guard current == revision, !Task.isCancelled else { return }
        guard hasAccess else { clear(); return }
        if session.detailFailures[modemID] == nil { modemRefreshPending = false }
    }

    // A 204 confirms the write even when its subsequent GET is unavailable.
    // The caller may close its editor; the shared page retains a read-only retry.
    func save(_ change: ModemSettingsChange) async -> Bool {
        guard !Task.isCancelled, canSubmit(change) else { return false }
        revision += 1
        let current = revision
        loading = []
        busy = true
        activeSection = change.section
        confirmationsPending[change.section] = false
        modemRefreshPending = true
        lastWriteAcknowledged = false
        failure = nil
        defer {
            if current == revision { busy = false; activeSection = nil }
        }
        await session.invalidateModemReads(modemID)
        guard current == revision, !Task.isCancelled else { return false }
        let id = modemID
        do {
            try await session.protectedRequest { api, token in
                switch change {
                case .airplane(let enabled): try await api.setModemAirplaneMode(id, enabled: enabled, token: token)
                case .mode(let mode): try await api.setModemMode(id, mode: mode, token: token)
                case .bands(let bands): try await api.setModemBands(id, bands: bands, token: token)
                case .device(let settings): try await api.setModemDeviceSettings(id, settings: settings, token: token)
                }
            }
            guard current == revision, !Task.isCancelled else { return false }
            lastWriteAcknowledged = true
            confirmationsPending[change.section] = true
        } catch {
            guard current == revision else { return false }
            guard hasAccess else { clear(); return false }
            // Cancellation does not establish whether the modem applied the request.
            guard !Task.isCancelled, !(error is CancellationError) else { return false }
            failure = .wrapping(error)
            if change.section == .device, failure?.code == "update_settings_failed" {
                failure?.message = String(localized: "modemSettings.saveFailed")
            }
        }
        await session.invalidateModemReads(modemID)
        guard current == revision, !Task.isCancelled else { return false }
        await readSections(change.refreshSections, revision: current)
        guard current == revision, !Task.isCancelled else { return false }
        await refreshModem(revision: current)
        return current == revision && !Task.isCancelled && hasAccess && lastWriteAcknowledged
    }

    private func canSubmit(_ change: ModemSettingsChange) -> Bool {
        switch change {
        case .airplane(let enabled):
            return canChangeAirplane && airplane?.enabled != enabled
        case .mode(let mode):
            return canChangeMode && modes?.current.id != mode && modes?.supported.contains(where: { $0.id == mode }) == true
        case .bands(let values):
            return Set(values).count == values.count && canSaveBands(Set(values))
        case .device(let settings):
            return canSaveDevice(ModemDeviceDraft(settings)) && settings.alias == settings.alias.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    func clear() {
        revision += 1
        airplane = nil
        modes = nil
        bands = nil
        device = nil
        loading = []
        failures = [:]
        busy = false
        activeSection = nil
        failure = nil
        confirmationsPending = [:]
        modemRefreshPending = false
        lastWriteAcknowledged = false
    }
}

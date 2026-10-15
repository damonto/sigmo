import Foundation
import Observation

@MainActor @Observable
final class SIMPINState {
    let session: BackendSession
    let modemID: String
    private(set) var status: SIMPINStatus?
    private(set) var legacy = false
    private(set) var available = false
    private(set) var loading = false
    private(set) var busy = false
    private(set) var action: SIMPINAction?
    private(set) var errorCode: String?
    private(set) var acknowledged = false
    private(set) var targetChanged = false
    var pin = ""
    var newPIN = ""
    var confirmation = ""
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var readRevision = 0

    init(session: BackendSession, modemID: String) {
        self.session = session
        self.modemID = modemID
    }

    var modem: Modem? { session.modems.first { $0.id == modemID } }
    var identity: SIMPINPageIdentity {
        SIMPINPageIdentity(
            accessRevision: session.accessRevision,
            modem: modem.map(ModemSIMIdentity.init)
        )
    }
    var needsUnlock: Bool {
        modem?.state == "locked" && (!legacy || modem?.unlockSupported == true)
    }

    var canSubmit: Bool {
        guard !busy, !loading, !acknowledged, !targetChanged, session.canAccessDevices, let action,
              status?.supported == true || (legacy && action == .pin && modem?.unlockSupported == true) else {
            return false
        }
        guard action == .puk ? validSIMPUK(pin) : validSIMPIN(pin) else { return false }
        return !action.needsNewPIN || (validSIMPIN(newPIN) && newPIN == confirmation)
    }

    var statusMessage: String {
        if status?.supported == false {
            return String(localized: "simPin.errors.sim_pin_unsupported")
        }
        let lock = status?.lock ?? .unknown
        let key = "simPin.locks." + (lock == .unlocked ? "unknown" : lock.rawValue)
        return String(localized: String.LocalizationValue(key))
    }

    var attemptMessage: String? {
        guard let action, let status else { return nil }
        let count = action == .puk ? status.pukRetries : status.pinRetries
        guard let count else { return nil }
        let key = action == .puk ? "simPin.pukAttempts" : "simPin.pinAttempts"
        return String(format: String(localized: String.LocalizationValue(key)), Int64(count))
    }

    var errorMessage: String? {
        guard let errorCode else { return nil }
        let code: String
        switch errorCode {
        case "sim_pin_invalid_request", "sim_pin_incorrect", "sim_puk_incorrect", "sim_puk_required",
             "sim_permanently_blocked", "sim_pin_state_conflict", "sim_pin_unsupported",
             "sim_pin_unavailable", "sim_pin_result_unknown", "enable_modem_after_unlock_failed":
            code = errorCode
        default:
            code = "sim_pin_result_unknown"
        }
        return String(localized: String.LocalizationValue("simPin.errors." + code))
    }

    var failure: BackendFailure? {
        guard let errorMessage else { return nil }
        return BackendFailure(code: errorCode ?? "sim_pin_result_unknown", message: errorMessage)
    }

    func clearCredentials() {
        pin = ""
        newPIN = ""
        confirmation = ""
    }

    func choose(_ value: SIMPINAction?) {
        guard !busy, !loading else { return }
        clearCredentials()
        action = value
        acknowledged = false
        errorCode = nil
    }

    func leave() {
        let unresolved = busy && !acknowledged
        revision += 1
        readRevision += 1
        clearCredentials()
        action = nil
        errorCode = nil
        acknowledged = false
        targetChanged = false
        if unresolved { status = nil }
        busy = false
        loading = false
    }

    func reset() {
        leave()
        status = nil
        legacy = false
        available = false
    }

    func load() async {
        guard !busy else { return }
        errorCode = nil
        await readStatus()
    }

    private func readStatus() async {
        guard session.canAccessDevices, !Task.isCancelled else { return }
        let page = revision
        let target = identity
        let id = modemID
        readRevision += 1
        let request = readRevision
        loading = true
        defer {
            if page == revision, request == readRevision { loading = false }
        }
        do {
            let value = try await session.protectedRequest { api, token in
                try await api.simPINStatus(id, token: token)
            }
            guard page == revision, target == identity, request == readRevision, !Task.isCancelled else { return }
            status = value
            legacy = false
            available = value.supported
            clearCredentials()
            action = value.supported ? value.lock.unlockAction : nil
        } catch {
            guard page == revision, target == identity, request == readRevision, !Task.isCancelled else { return }
            status = nil
            action = nil
            clearCredentials()
            let failure = BackendFailure.wrapping(error)
            legacy = failure.status == 404 || failure.status == 405
            available = !legacy && failure.code != "sim_pin_unsupported"
            if legacy {
                action = modem?.unlockSupported == true ? .pin : nil
            } else if errorCode == nil {
                errorCode = "sim_pin_unavailable"
            }
        }
    }

    // Unlock sheets can dismiss as soon as the credential command is acknowledged.
    // Their parent owns the subsequent modem refresh, independently of sheet lifetime.
    func submit(refreshAfterSuccess: Bool = true) async {
        guard !Task.isCancelled, canSubmit, let action else { return }
        let page = revision
        let target = identity
        let id = modemID
        let code = pin
        let replacement = newPIN
        let simToken = status?.simToken
        busy = true
        errorCode = nil
        acknowledged = false
        readRevision += 1
        defer {
            if page == revision {
                clearCredentials()
                busy = false
            }
        }
        do {
            try await session.protectedRequest { api, token in
                try await api.submitSIMPIN(
                    id, action: action, pin: code, newPIN: replacement,
                    simToken: simToken, token: token
                )
            }
            guard page == revision, target == identity, !Task.isCancelled else { return }
            acknowledged = true
        } catch {
            guard page == revision, target == identity, !Task.isCancelled else { return }
            let code = BackendFailure.wrapping(error).code
            if code == "sim_target_changed" {
                targetChanged = true
                clearCredentials()
                self.action = nil
                return
            }
            errorCode = code
            acknowledged = code == "enable_modem_after_unlock_failed"
        }
        clearCredentials()
        status = nil
        if acknowledged, !refreshAfterSuccess {
            self.action = nil
            return
        }
        await readStatus()
        guard page == revision, target == identity, !Task.isCancelled else { return }
        retainFailedAction(action, token: simToken)
        await session.refreshModem(id)
    }

    private func retainFailedAction(_ submitted: SIMPINAction, token: String?) {
        guard !acknowledged, let status, status.supported, status.simToken == token,
              status.lock == .unlocked, let enabled = status.enabled else { return }
        switch submitted {
        case .change, .disable:
            if enabled { action = submitted }
        case .enable where !enabled:
            action = submitted
        default:
            break
        }
    }
}

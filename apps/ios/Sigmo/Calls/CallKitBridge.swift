import AVFAudio
@preconcurrency import CallKit
import Foundation

enum CallAction: Sendable {
    case start(UUID, String), answer(UUID), end(UUID), hold(UUID, Bool), mute(UUID, Bool), digits(UUID, String)
    var id: UUID {
        switch self {
        case .start(let id, _), .answer(let id), .end(let id), .hold(let id, _), .mute(let id, _), .digits(let id, _): id
        }
    }

    var diagnosticName: String {
        switch self {
        case .start: "start"
        case .answer: "answer"
        case .end: "end"
        case .hold: "hold"
        case .mute: "mute"
        case .digits: "dtmf"
        }
    }
}

// CXProvider delivers callbacks on the explicitly configured main queue. Keep
// the SDK boundary here so the call state can also run without system calls.
@MainActor
final class CallKitBridge: NSObject, @preconcurrency CXProviderDelegate {
    enum Completion: Equatable {
        case fulfilled, failed
    }

    var perform: (@MainActor (CallAction) async throws -> Void)?
    var reset: (@MainActor () -> Void)?
    var audioActivated: (@MainActor (Bool) -> Void)?
    private let provider: CXProvider?
    private let controller: CXCallController?
    private let diagnostics: CallDiagnostics
    private let didComplete: (@MainActor (UUID, Completion) -> Void)?
    private struct PendingAction {
        let value: CallAction
        let action: CXCallAction
        let task: Task<Void, Never>
        let started: ContinuousClock.Instant
    }
    private var actions: [UUID: PendingAction] = [:]
    private var actionTail: Task<Void, Never>?

    init(enabled: Bool, diagnostics: DiagnosticLogStore = AppDiagnostics.store,
         didComplete: (@MainActor (UUID, Completion) -> Void)? = nil) {
        self.didComplete = didComplete
        self.diagnostics = CallDiagnostics(store: diagnostics)
        if enabled {
            let configuration = CXProviderConfiguration()
            configuration.supportsVideo = false
            configuration.supportedHandleTypes = [.phoneNumber, .generic]
            // One connected call plus one waiting invitation. CallsState requires
            // a confirmed end before accepting the waiting call.
            configuration.maximumCallGroups = 2
            configuration.maximumCallsPerCallGroup = 1
            configuration.includesCallsInRecents = true
            provider = CXProvider(configuration: configuration)
            controller = CXCallController()
        } else {
            provider = nil
            controller = nil
        }
        super.init()
        provider?.setDelegate(self, queue: .main)
    }

    func request(_ action: CallAction) async throws {
        try await request([action])
    }

    func request(_ actions: [CallAction]) async throws {
        for action in actions { diagnostics.record("callkit.action.requested", id: action.id, fields: ["action": action.diagnosticName]) }
        guard let controller else {
            for action in actions { try await perform?(action) }
            return
        }
        do {
            try await controller.request(CXTransaction(actions: actions.map(systemAction)))
            for action in actions { diagnostics.record("callkit.action.accepted", id: action.id, fields: ["action": action.diagnosticName]) }
        } catch {
            for action in actions { diagnostics.record("callkit.action.rejected", id: action.id, fields: ["action": action.diagnosticName], error: error) }
            throw error
        }
    }

    private func systemAction(_ action: CallAction) -> CXCallAction {
        switch action {
        case .start(let id, let number): CXStartCallAction(call: id, handle: CXHandle(type: .phoneNumber, value: number))
        case .answer(let id): CXAnswerCallAction(call: id)
        case .end(let id): CXEndCallAction(call: id)
        case .hold(let id, let hold): CXSetHeldCallAction(call: id, onHold: hold)
        case .mute(let id, let muted): CXSetMutedCallAction(call: id, muted: muted)
        case .digits(let id, let digits): CXPlayDTMFCallAction(call: id, digits: digits, type: .singleTone)
        }
    }

    func incoming(id: UUID, number: String, name: String?, completion: @escaping @Sendable (Bool) -> Void) {
        let started = ContinuousClock.now
        let diagnostics = diagnostics
        diagnostics.record("callkit.incoming.started", id: id, fields: ["systemEnabled": String(provider != nil)])
        guard let provider else {
            diagnostics.record("callkit.incoming.reported", id: id, fields: ["accepted": "true", "durationMS": CallDiagnostics.durationMS(since: started)])
            completion(true)
            return
        }
        provider.reportNewIncomingCall(with: id, update: update(number: number, name: name, holding: true)) { error in
            diagnostics.record("callkit.incoming.reported", id: id,
                               fields: ["accepted": String(error == nil), "durationMS": CallDiagnostics.durationMS(since: started)], error: error)
            completion(error == nil)
        }
    }

    func update(id: UUID, number: String, name: String?, holding: Bool) {
        provider?.reportCall(with: id, updated: update(number: number, name: name, holding: holding))
    }

    private func update(number: String, name: String?, holding: Bool) -> CXCallUpdate {
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(type: number.isEmpty ? .generic : .phoneNumber, value: number.isEmpty ? callText("calls.unknown") : number)
        update.localizedCallerName = name
        update.supportsDTMF = true
        update.supportsHolding = holding
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.hasVideo = false
        return update
    }

    func connecting(_ id: UUID) { provider?.reportOutgoingCall(with: id, startedConnectingAt: .now) }
    func connected(_ id: UUID, at date: Date?) { provider?.reportOutgoingCall(with: id, connectedAt: date ?? .now) }
    func ended(_ id: UUID, reason: CXCallEndedReason) {
        diagnostics.record("callkit.ended", id: id, fields: CallDiagnostics.ending(reason))
        provider?.reportCall(with: id, endedAt: .now, reason: reason)
    }

    func providerDidReset(_ provider: CXProvider) {
        diagnostics.record("callkit.reset", fields: ["pendingActions": String(actions.count)])
        for action in actions.values {
            diagnostics.record("callkit.action.cancelled", id: action.value.id,
                               fields: ["action": action.value.diagnosticName, "actionID": action.action.uuid.uuidString, "cause": "provider_reset"])
            action.task.cancel()
        }
        actions.removeAll()
        actionTail = nil
        CallWebRTC.deactivate(AVAudioSession.sharedInstance())
        audioActivated?(false)
        reset?()
    }

    @discardableResult
    func execute(_ action: CXCallAction, _ value: CallAction) -> Task<Void, Never> {
        let started = ContinuousClock.now
        diagnostics.record("callkit.action.received", id: value.id, fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString])
        let local: Bool
        if case .mute = value { local = true } else { local = false }
        if case .end(let id) = value {
            // Stop waiting for optional remote controls before ending this call.
            // Start/answer must still resolve whether a carrier action was accepted.
            let pendingControls = actions.values.filter { pending in
                guard pending.value.id == id else { return false }
                switch pending.value {
                case .hold, .digits: return true
                default: return false
                }
            }
            for pending in pendingControls {
                diagnostics.record("callkit.action.cancelled", id: pending.value.id,
                                   fields: ["action": pending.value.diagnosticName, "actionID": pending.action.uuid.uuidString, "cause": "end_requested"])
                pending.task.cancel()
                complete(pending.action, with: .failed)
            }
        }
        let previous = local ? nil : actionTail
        let didComplete = didComplete
        let diagnostics = diagnostics
        let task = Task { [weak self] in
            defer { self?.actions[action.uuid] = nil }
            await previous?.value
            guard !Task.isCancelled else { return }
            guard let self else {
                if !action.isComplete {
                    action.fail()
                    didComplete?(action.uuid, .failed)
                }
                return
            }
            diagnostics.record("callkit.action.started", id: value.id,
                               fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString, "queueMS": CallDiagnostics.durationMS(since: started)])
            do {
                try await self.perform?(value)
                guard !Task.isCancelled else { return }
                self.complete(action, with: .fulfilled)
            } catch {
                diagnostics.record("callkit.action.error", id: value.id,
                                   fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString], error: error)
                if !Task.isCancelled { self.complete(action, with: .failed) }
            }
        }
        actions[action.uuid] = PendingAction(value: value, action: action, task: task, started: started)
        if !local { actionTail = task }
        return task
    }

    private func complete(_ action: CXCallAction, with completion: Completion) {
        // Relinquish ownership before calling the SDK. A late result or another
        // end request must not complete an already preempted or timed-out action.
        guard let pending = actions.removeValue(forKey: action.uuid), !action.isComplete else { return }
        diagnostics.record("callkit.action.completed", id: pending.value.id,
                           fields: ["action": pending.value.diagnosticName, "actionID": action.uuid.uuidString,
                                    "completion": completion == .fulfilled ? "fulfilled" : "failed", "durationMS": CallDiagnostics.durationMS(since: pending.started)])
        switch completion {
        case .fulfilled: action.fulfill()
        case .failed: action.fail()
        }
        didComplete?(action.uuid, completion)
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) { execute(action, .start(action.callUUID, action.handle.value)) }
    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) { execute(action, .answer(action.callUUID)) }
    func provider(_ provider: CXProvider, perform action: CXEndCallAction) { execute(action, .end(action.callUUID)) }
    func provider(_ provider: CXProvider, perform action: CXSetHeldCallAction) { execute(action, .hold(action.callUUID, action.isOnHold)) }
    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) { execute(action, .mute(action.callUUID, action.isMuted)) }
    func provider(_ provider: CXProvider, perform action: CXPlayDTMFCallAction) { execute(action, .digits(action.callUUID, action.digits)) }
    func provider(_ provider: CXProvider, timedOutPerforming action: CXAction) {
        timedOut(action)
    }
    func timedOut(_ action: CXAction) {
        // CallKit has already completed this action; only cancel our remaining work.
        diagnostics.record("callkit.action.timed_out", id: (action as? CXCallAction)?.callUUID,
                           fields: ["actionID": action.uuid.uuidString, "action": actions[action.uuid]?.value.diagnosticName ?? "unknown"])
        actions.removeValue(forKey: action.uuid)?.task.cancel()
    }
    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        diagnostics.record("callkit.audio.activated")
        CallWebRTC.activate(audioSession)
        audioActivated?(true)
    }
    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        diagnostics.record("callkit.audio.deactivated")
        CallWebRTC.deactivate(audioSession)
        audioActivated?(false)
    }
}

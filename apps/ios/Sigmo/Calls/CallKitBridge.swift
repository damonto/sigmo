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
    private let didComplete: (@MainActor (UUID, Completion) -> Void)?
    private struct PendingAction {
        let value: CallAction
        let action: CXCallAction
        let task: Task<Void, Never>
    }
    private var actions: [UUID: PendingAction] = [:]
    private var actionTail: Task<Void, Never>?

    init(enabled: Bool, didComplete: (@MainActor (UUID, Completion) -> Void)? = nil) {
        self.didComplete = didComplete
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
        guard let controller else {
            for action in actions { try await perform?(action) }
            return
        }
        try await controller.request(CXTransaction(actions: actions.map(systemAction)))
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
        guard let provider else { completion(true); return }
        provider.reportNewIncomingCall(with: id, update: update(number: number, name: name, holding: true)) { error in
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
    func ended(_ id: UUID, reason: CXCallEndedReason) { provider?.reportCall(with: id, endedAt: .now, reason: reason) }

    func providerDidReset(_ provider: CXProvider) {
        for action in actions.values { action.task.cancel() }
        actions.removeAll()
        actionTail = nil
        CallWebRTC.deactivate(AVAudioSession.sharedInstance())
        audioActivated?(false)
        reset?()
    }

    @discardableResult
    func execute(_ action: CXCallAction, _ value: CallAction) -> Task<Void, Never> {
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
                pending.task.cancel()
                complete(pending.action, with: .failed)
            }
        }
        let previous = local ? nil : actionTail
        let didComplete = didComplete
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
            do {
                try await self.perform?(value)
                guard !Task.isCancelled else { return }
                self.complete(action, with: .fulfilled)
            } catch { if !Task.isCancelled { self.complete(action, with: .failed) } }
        }
        actions[action.uuid] = PendingAction(value: value, action: action, task: task)
        if !local { actionTail = task }
        return task
    }

    private func complete(_ action: CXCallAction, with completion: Completion) {
        // Relinquish ownership before calling the SDK. A late result or another
        // end request must not complete an already preempted or timed-out action.
        guard actions.removeValue(forKey: action.uuid) != nil, !action.isComplete else { return }
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
        actions.removeValue(forKey: action.uuid)?.task.cancel()
    }
    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        CallWebRTC.activate(audioSession)
        audioActivated?(true)
    }
    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        CallWebRTC.deactivate(audioSession)
        audioActivated?(false)
    }
}

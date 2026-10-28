import AVFAudio
@preconcurrency import LiveCommunicationKit
import Foundation

enum CallEndReason: Int, Sendable {
    // Preserve the existing diagnostic codes while mapping system terminology.
    case failed = 1, remoteEnded, unanswered, answeredElsewhere, declinedElsewhere

    fileprivate var systemReason: Conversation.EndedReason {
        switch self {
        case .failed: .failed
        case .remoteEnded: .remoteEnded
        case .unanswered: .unanswered
        case .answeredElsewhere: .joinedElsewhere
        case .declinedElsewhere: .declinedElsewhere
        }
    }
}

enum CallAction: Equatable, Sendable {
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

// These SDK values lack Sendable annotations. Boxes transfer only immutable
// updates and system-owned action references; app state stays on the main actor.
private struct NativeCallAction: @unchecked Sendable { let value: ConversationAction }
private struct NativeCallUpdate: @unchecked Sendable { let value: Conversation.Update }

private final class SystemConversationManager: @unchecked Sendable {
    let value: ConversationManager

    init(configuration: ConversationManager.Configuration) {
        value = ConversationManager(configuration: configuration)
    }

    func perform(_ actions: [NativeCallAction]) async throws {
        try await value.perform(actions.map(\.value))
    }

    func incoming(id: UUID, update: NativeCallUpdate) async throws {
        try await value.reportNewIncomingConversation(uuid: id, update: update.value)
    }
}

// Keep the system SDK boundary here so fixtures use the same call state without
// registering real conversations. SDK callbacks explicitly hop to the main actor.
@MainActor
final class CallSystemBridge: ConversationManagerDelegate {
    enum Completion: Equatable {
        case fulfilled, failed
    }

    var perform: (@MainActor (CallAction) async throws -> Void)?
    var reset: (@MainActor () -> Void)?
    var audioActivated: (@MainActor (Bool) -> Void)?
    private let manager: SystemConversationManager?
    private let diagnostics: CallDiagnostics
    private let didComplete: (@MainActor (UUID, Completion) -> Void)?
    private struct PendingAction {
        let value: CallAction
        let action: ConversationAction
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
            // One connected call plus one waiting invitation. CallsState requires
            // a confirmed end before accepting the waiting call.
            let configuration = ConversationManager.Configuration(ringtoneName: nil, iconTemplateImageData: nil,
                maximumConversationGroups: 2, maximumConversationsPerConversationGroup: 1,
                includesConversationInRecents: true, supportsVideo: false, supportedHandleTypes: [.phoneNumber, .generic])
            manager = SystemConversationManager(configuration: configuration)
        } else {
            manager = nil
        }
        manager?.value.delegate = self
    }

    func request(_ action: CallAction) async throws {
        try await request([action])
    }

    func request(_ actions: [CallAction]) async throws {
        for action in actions { diagnostics.record("conversation.action.requested", id: action.id, fields: ["action": action.diagnosticName]) }
        guard let manager else {
            for action in actions { try await perform?(action) }
            return
        }
        do {
            try await manager.perform(actions.map { NativeCallAction(value: systemAction($0)) })
            for action in actions { diagnostics.record("conversation.action.accepted", id: action.id, fields: ["action": action.diagnosticName]) }
        } catch {
            for action in actions { diagnostics.record("conversation.action.rejected", id: action.id, fields: ["action": action.diagnosticName], error: error) }
            throw error
        }
    }

    private func systemAction(_ action: CallAction) -> ConversationAction {
        switch action {
        case .start(let id, let number): StartConversationAction(conversationUUID: id, handles: [handle(number: number, name: nil)], isVideo: false)
        case .answer(let id): JoinConversationAction(conversationUUID: id)
        case .end(let id): EndConversationAction(conversationUUID: id)
        case .hold(let id, let held): PauseConversationAction(conversationUUID: id, isPaused: held)
        case .mute(let id, let muted): MuteConversationAction(conversationUUID: id, isMuted: muted)
        case .digits(let id, let digits): PlayToneAction(conversationUUID: id, digits: digits, tone: .single)
        }
    }

    func incoming(id: UUID, number: String, name: String?, completion: @escaping @Sendable (Bool) -> Void) {
        let started = ContinuousClock.now
        let diagnostics = diagnostics
        diagnostics.record("conversation.incoming.started", id: id, fields: ["systemEnabled": String(manager != nil)])
        guard let manager else {
            diagnostics.record("conversation.incoming.reported", id: id, fields: ["accepted": "true", "durationMS": CallDiagnostics.durationMS(since: started)])
            completion(true)
            return
        }
        // Report before any backend I/O. PushKit completion follows the system's
        // result; callers can concurrently restore the invitation's backend state.
        Task { @MainActor in
            do {
                try await manager.incoming(id: id, update: NativeCallUpdate(value: update(number: number, name: name, holding: true)))
                diagnostics.record("conversation.incoming.reported", id: id,
                    fields: ["accepted": "true", "durationMS": CallDiagnostics.durationMS(since: started)])
                completion(true)
            } catch {
                diagnostics.record("conversation.incoming.reported", id: id,
                    fields: ["accepted": "false", "durationMS": CallDiagnostics.durationMS(since: started)], error: error)
                completion(false)
            }
        }
    }

    func update(id: UUID, number: String, name: String?, holding: Bool) {
        report(.conversationUpdated(update(number: number, name: name, holding: holding)), id: id)
    }

    private func handle(number: String, name: String?) -> Handle {
        Handle(type: number.isEmpty ? .generic : .phoneNumber,
               value: number.isEmpty ? "unknown" : number,
               displayName: name ?? (number.isEmpty ? callText("calls.unknown") : number))
    }

    private func update(number: String, name: String?, holding: Bool) -> Conversation.Update {
        var update = Conversation.Update()
        update.localMember = Handle(type: .generic, value: "sigmo", displayName: "Sigmo")
        update.activeRemoteMembers = [handle(number: number, name: name)]
        update.capabilities = holding ? [.playingTones, .pausing] : [.playingTones]
        return update
    }

    private func report(_ event: Conversation.Event, id: UUID) {
        guard let manager, let conversation = manager.value.conversations.first(where: { $0.uuid == id }) else { return }
        manager.value.reportConversationEvent(event, for: conversation)
    }

    func connecting(_ id: UUID) { report(.conversationStartedConnecting(.now), id: id) }
    func connected(_ id: UUID, at date: Date?) { report(.conversationConnected(date ?? .now), id: id) }
    func ended(_ id: UUID, reason: CallEndReason) {
        diagnostics.record("conversation.ended", id: id, fields: CallDiagnostics.ending(reason))
        report(.conversationEnded(.now, reason.systemReason), id: id)
    }

    func resetFromSystem() {
        diagnostics.record("conversation.reset", fields: ["pendingActions": String(actions.count)])
        for action in actions.values {
            diagnostics.record("conversation.action.cancelled", id: action.value.id,
                fields: ["action": action.value.diagnosticName, "actionID": action.action.uuid.uuidString, "cause": "system_reset"])
            action.task.cancel()
        }
        actions.removeAll()
        actionTail = nil
        CallWebRTC.deactivate(AVAudioSession.sharedInstance())
        audioActivated?(false)
        reset?()
    }

    @discardableResult
    func execute(_ action: ConversationAction, _ value: CallAction) -> Task<Void, Never> {
        let started = ContinuousClock.now
        diagnostics.record("conversation.action.received", id: value.id, fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString])
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
                diagnostics.record("conversation.action.cancelled", id: pending.value.id,
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
                if !Self.isComplete(action) {
                    action.fail()
                    didComplete?(action.uuid, .failed)
                }
                return
            }
            diagnostics.record("conversation.action.started", id: value.id,
                               fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString, "queueMS": CallDiagnostics.durationMS(since: started)])
            do {
                try await self.perform?(value)
                guard !Task.isCancelled else { return }
                self.complete(action, with: .fulfilled)
            } catch {
                diagnostics.record("conversation.action.error", id: value.id,
                                   fields: ["action": value.diagnosticName, "actionID": action.uuid.uuidString], error: error)
                if !Task.isCancelled { self.complete(action, with: .failed) }
            }
        }
        actions[action.uuid] = PendingAction(value: value, action: action, task: task, started: started)
        if !local { actionTail = task }
        return task
    }

    private func complete(_ action: ConversationAction, with completion: Completion) {
        // Relinquish ownership before calling the SDK. A late result or another
        // end request must not complete an already preempted or timed-out action.
        guard let pending = actions.removeValue(forKey: action.uuid), !Self.isComplete(action) else { return }
        diagnostics.record("conversation.action.completed", id: pending.value.id,
                           fields: ["action": pending.value.diagnosticName, "actionID": action.uuid.uuidString,
                                    "completion": completion == .fulfilled ? "fulfilled" : "failed", "durationMS": CallDiagnostics.durationMS(since: pending.started)])
        switch completion {
        case .fulfilled: fulfill(action)
        case .failed: action.fail()
        }
        didComplete?(action.uuid, completion)
    }

    private static func isComplete(_ action: ConversationAction) -> Bool {
        switch action.state {
        case .complete, .failed: true
        default: false
        }
    }

    private func fulfill(_ action: ConversationAction) {
        switch action {
        case let action as StartConversationAction: action.fulfill(dateStarted: .now)
        case let action as JoinConversationAction: action.fulfill(dateConnected: .now)
        case let action as EndConversationAction: action.fulfill(dateEnded: .now)
        default: action.fulfill()
        }
    }

    nonisolated func conversationManagerDidBegin(_ manager: ConversationManager) {}
    nonisolated func conversationManagerDidReset(_ manager: ConversationManager) {
        Task { @MainActor [weak self] in self?.resetFromSystem() }
    }
    nonisolated func conversationManager(_ manager: ConversationManager, conversationChanged conversation: Conversation) {}
    nonisolated func conversationManager(_ manager: ConversationManager, perform action: ConversationAction) {
        let delivery = NativeCallAction(value: action)
        Task { @MainActor [weak self] in
            let action = delivery.value
            guard let self else { action.fail(); return }
            execute(action)
        }
    }

    @discardableResult
    func execute(_ action: ConversationAction) -> Task<Void, Never>? {
        let id = action.conversationUUID
        switch action {
        case let action as StartConversationAction:
            guard !action.isVideo, action.handles.count == 1, let number = action.handles.first?.value else { action.fail(); return nil }
            return execute(action, .start(id, number))
        case is JoinConversationAction: return execute(action, .answer(id))
        case is EndConversationAction: return execute(action, .end(id))
        case let action as PauseConversationAction: return execute(action, .hold(id, action.isPaused))
        case let action as MuteConversationAction: return execute(action, .mute(id, action.isMuted))
        case let action as PlayToneAction:
            guard action.tone == .single else { action.fail(); return nil }
            return execute(action, .digits(id, action.digits))
        default: action.fail(); return nil
        }
    }

    nonisolated func conversationManager(_ manager: ConversationManager, timedOutPerforming action: ConversationAction) {
        let delivery = NativeCallAction(value: action)
        Task { @MainActor [weak self] in self?.timedOut(delivery.value) }
    }
    func timedOut(_ action: ConversationAction) {
        // The system owns timeout completion; late backend results only release work.
        diagnostics.record("conversation.action.timed_out", id: action.conversationUUID,
            fields: ["actionID": action.uuid.uuidString, "action": actions[action.uuid]?.value.diagnosticName ?? "unknown"])
        actions.removeValue(forKey: action.uuid)?.task.cancel()
    }
    nonisolated func conversationManager(_ manager: ConversationManager, didActivate audioSession: AVAudioSession) {
        Task { @MainActor [weak self] in
            guard let self else { return }
            diagnostics.record("conversation.audio.activated")
            CallWebRTC.activate(audioSession)
            audioActivated?(true)
        }
    }
    nonisolated func conversationManager(_ manager: ConversationManager, didDeactivate audioSession: AVAudioSession) {
        Task { @MainActor [weak self] in
            guard let self else { return }
            diagnostics.record("conversation.audio.deactivated")
            CallWebRTC.deactivate(audioSession)
            audioActivated?(false)
        }
    }
}

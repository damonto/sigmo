import AVFAudio
import CallKit
import Foundation
import Intents
import Observation

@MainActor @Observable
final class CallsState {
    private(set) var calls: [UUID: ActiveCall] = [:] {
        didSet { updatePlaybackAvailability() }
    }
    private(set) var revision = 0
    var presented = false
    var foreground = true {
        didSet { updatePlaybackAvailability() }
    }
    private var systemAudioActive = false
    var failure: BackendFailure? {
        didSet {
            if let failure, failure != oldValue {
                AppDiagnostics.record(failure, category: "calls", operation: "call.failed")
            }
        }
    }
    var callbackNumber: String?
    private(set) var systemCallback: SystemCallbackRequest?
    let audio = CallAudio()
    let contacts: CallContacts
    let recordings: CallRecordings
    private(set) var clientID: String
    let store: BackendStore
    let system: CallKitBridge
    @ObservationIgnored private let diagnostics: CallDiagnostics
    @ObservationIgnored private let fixtures: Bool
    private let demo: Bool
    private var demoStopped = false
    @ObservationIgnored private let identity: PushPersistence
    private struct MonitorKey: Hashable {
        let line: String
        let address: String
        let session: ObjectIdentifier
        let access: Int
    }
    private struct Monitor {
        let id: UUID
        let task: Task<Void, Never>
    }
    private struct CallbackRoutesFile: Codable {
        static let currentVersion = 1

        let version: Int
        let routes: [String: CallbackRoute]
    }
    @ObservationIgnored private var monitors: [MonitorKey: Monitor] = [:]
    @ObservationIgnored private var mediaTask: Task<Void, Never>?
    @ObservationIgnored private var mediaID: UUID?
    @ObservationIgnored private var mediaAttempts = 0
    @ObservationIgnored private var bootstraps: [UUID: Task<Void, Never>] = [:]
    @ObservationIgnored private var access: [UUID: Int] = [:]
    @ObservationIgnored private var ended: [CallIdentity: Date] = [:]
    @ObservationIgnored private var endedUUIDs: [UUID: Date] = [:]
    @ObservationIgnored private var reporting: Set<UUID> = [] {
        didSet { updatePlaybackAvailability() }
    }
    @ObservationIgnored private var callbackRoutes: [String: CallbackRoute] = [:]
    @ObservationIgnored private var callbackResolution: (requestID: UUID, destination: SystemCallbackDestination)?

    init(store: BackendStore, fixtures: Bool = false, demo: Bool = false, clientID: String? = nil, recordings: CallRecordings? = nil,
         identity: PushPersistence = .keychain, contacts: CallContacts = CallContacts(), diagnostics: DiagnosticLogStore = AppDiagnostics.store) {
        self.demo = demo
        self.contacts = contacts
        self.store = store
        self.fixtures = fixtures || demo
        self.identity = identity
        self.diagnostics = CallDiagnostics(store: diagnostics)
        self.system = CallKitBridge(enabled: !fixtures && !demo, diagnostics: diagnostics)
        self.recordings = recordings ?? CallRecordings(directory: (fixtures || demo) ? URL.temporaryDirectory.appending(path: "SigmoCalls-\(UUID())") : nil)
        self.clientID = clientID ?? (demo ? "demo-\(UUID().uuidString)" : fixtures ? "fixture-call-client" : "")
        restoreClientIdentity()
        system.perform = { [weak self] action in
            guard let self else { throw CancellationError() }
            try await self.perform(action)
        }
        system.reset = { [weak self] in self?.reset() }
        system.audioActivated = { [weak self] active in
            self?.systemAudioActive = active
            if !active { self?.audio.recordingInterrupted() }
            self?.updatePlaybackAvailability()
        }
        audio.onFailure = { [weak self] error in self?.mediaFailed(error) }
        audio.onReady = { [weak self] in
            self?.diagnostics.record("call.media.ready", id: self?.mediaID)
            self?.mediaAttempts = 0
        }
        if let data = try? Data(contentsOf: self.recordings.directory.appending(path: "callback-routes.data")),
           let file = try? JSONDecoder().decode(CallbackRoutesFile.self, from: data),
           file.version == CallbackRoutesFile.currentVersion {
            callbackRoutes = file.routes
        }
    }

    var primary: ActiveCall? {
        calls.values.first { $0.ownsCall && !$0.endRequested }
            ?? calls.values.first { !$0.endRequested }
    }

    private func restoreClientIdentity() {
        guard clientID.isEmpty else { return }
        do {
            if let data = try identity.read("call-client", false) {
                guard let value = String(data: data, encoding: .utf8),
                      value.range(of: "^[A-Za-z0-9_-]{1,128}$", options: .regularExpression) != nil else {
                    throw BackendFailure.local("push.keychainUnavailable")
                }
                clientID = value
            } else {
                let value = UUID().uuidString
                try identity.write("call-client", Data(value.utf8), false)
                clientID = value
            }
            if failure?.code == "push.keychainUnavailable" { failure = nil }
        } catch { failure = .local("push.keychainUnavailable") }
    }
    var incoming: ActiveCall? { calls.values.first { $0.record?.incoming == true && !$0.answering && !$0.endRequested } }
    var hasCall: Bool { !calls.isEmpty }

    private func updatePlaybackAvailability() {
        recordings.playback.allowed = foreground && !hasCall && reporting.isEmpty && !systemAudioActive
    }

    func name(_ call: ActiveCall) -> String {
        name(number: call.number, session: store.sessions[call.line.backendID], region: call.line.region)
    }

    func name(number: String, session: BackendSession?, region: String? = nil) -> String {
        let matches = session?.contacts(matching: number) ?? []
        if matches.count == 1 { return matches[0].displayName }
        if matches.isEmpty, let name = contacts.name(for: number, region: region ?? "") { return name }
        return PhoneNumberDisplay.format(number, regionCode: region)
    }

    func available(_ line: CallLine) -> Bool {
        !line.profileID.isEmpty && (try? session(for: line)) != nil && !clientID.isEmpty
            && store.sessions[line.backendID]?.capabilities?.contains("nativeCalls") == true
    }

    var availableLines: [CallLine] {
        store.sessions.values.flatMap { session in session.modems.map { CallLine(session: session, modem: $0) } }
            .filter { available($0) }.sorted { ($0.backendName, $0.label, $0.id) < ($1.backendName, $1.label, $1.id) }
    }

    @discardableResult
    func requestCallback(_ activity: NSUserActivity) -> Bool {
        guard let intent = activity.interaction?.intent else { return false }
        return requestCallback(intent)
    }

    @discardableResult
    func requestCallback(_ intent: INIntent) -> Bool {
        let contacts: [INPerson]
        switch intent {
        case let intent as INStartCallIntent:
            guard intent.callCapability != .videoCall else { return false }
            if let requested = intent.contacts, !requested.isEmpty {
                contacts = requested
            } else {
                contacts = intent.callRecordToCallBack?.participants ?? []
            }
        case let intent as INStartAudioCallIntent:
            // Phone Recents can still deliver this deprecated intent.
            contacts = intent.contacts ?? []
        default:
            return false
        }
        return requestCallback(contacts: contacts)
    }

    private func requestCallback(contacts: [INPerson]) -> Bool {
        // Sigmo supports one remote phone number per call.
        guard contacts.count == 1,
              let handle = contacts.first?.personHandle, handle.type == .phoneNumber,
              let number = handle.value else { return false }
        let normalized = CallDigits.normalize(number)
        guard CallDigits.callable(normalized), !CallDigits.isService(normalized) else { return false }
        // The system can repeat an activity after its original request was consumed.
        if calls.values.contains(where: { !$0.endRequested && CallDigits.normalize($0.number) == normalized }) {
            return true
        }
        guard systemCallback?.number != normalized else { return true }
        // Keep the request until the root view has restored backend state and
        // selected the appropriate dialing surface.
        systemCallback = SystemCallbackRequest(number: normalized)
        return true
    }

    func callbackLine(for number: String) -> CallLine? {
        guard case .dial(_, let line) = callbackDestination(for: number) else { return nil }
        return line
    }

    func resolveSystemCallback(_ request: SystemCallbackRequest) async -> SystemCallbackDestination {
        if let callbackResolution, callbackResolution.requestID == request.id { return callbackResolution.destination }
        await store.refreshAll()
        let destination = callbackDestination(for: request.number)
        if systemCallback?.id == request.id { callbackResolution = (request.id, destination) }
        return destination
    }

    func consumeSystemCallback(_ request: SystemCallbackRequest) {
        guard systemCallback?.id == request.id else { return }
        systemCallback = nil
        callbackResolution = nil
    }

    private func callbackDestination(for number: String) -> SystemCallbackDestination {
        guard let route = callbackRoutes[CallDigits.normalize(number)],
              !route.requiresSelection,
              let session = store.sessions[route.line.backendID] else {
            return .home(number: number, backendID: nil)
        }
        guard let modem = session.modems.first(where: { $0.id == route.line.modemID }) else {
            return .home(number: number, backendID: session.id)
        }
        let line = CallLine(session: session, modem: modem)
        guard session.configuration.address.absoluteString == route.line.address,
              session.isOnline,
              modem.sim.active,
              modem.state != "locked",
              modem.sim.identifier == route.line.profileID,
              available(line) else {
            return .modem(number: number, backendID: session.id, modemID: modem.id)
        }
        return .dial(number: number, line: line)
    }

    func rememberCallbackRoute(number: String, line: CallLine, startedAt: String) {
        let key = CallDigits.normalize(number)
        var route = CallbackRoute(line: line, startedAt: startedAt)
        if let existing = callbackRoutes[key] {
            // A number can belong to several system Recents entries with different
            // SIMs. Retain that ambiguity even when an older event arrives later.
            let requiresSelection = existing.requiresSelection || !existing.line.matches(line)
            if existing.date > route.date {
                route = CallbackRoute(line: existing.line, startedAt: existing.startedAt, requiresSelection: requiresSelection)
            } else {
                route = CallbackRoute(line: line, startedAt: startedAt, requiresSelection: requiresSelection)
            }
            if route == existing { return }
        }
        callbackRoutes[key] = route
        do {
            let file = CallbackRoutesFile(version: CallbackRoutesFile.currentVersion, routes: callbackRoutes)
            try JSONEncoder().encode(file).write(to: recordings.directory.appending(path: "callback-routes.data"), options: .atomic)
        } catch { failure = .local("calls.callbackRouteNotSaved") }
    }

    private func session(for line: CallLine) throws -> BackendSession {
        guard let session = store.sessions[line.backendID], session.canAccessDevices,
              session.configuration.address.absoluteString == line.address,
              let modem = session.modems.first(where: { $0.id == line.modemID }), modem.sim.identifier == line.profileID else {
            throw BackendFailure.local("calls.lineUnavailable")
        }
        return session
    }

    func dial(number: String, line: CallLine) async {
        restoreClientIdentity()
        guard available(line), calls.values.allSatisfy(\.endRequested), CallDigits.callable(number), !CallDigits.isService(number) else {
            failure = .local(hasCall ? "calls.alreadyCalling" : "calls.lineUnavailable")
            return
        }
        if !fixtures, AVAudioApplication.shared.recordPermission == .undetermined { _ = await AVAudioApplication.requestRecordPermission() }
        // The permission sheet can outlive a login, SIM change, or another call.
        guard available(line), calls.values.allSatisfy(\.endRequested) else {
            failure = .local(hasCall ? "calls.alreadyCalling" : "calls.lineUnavailable")
            return
        }
        // The entered prefix determines international dialing; SIM region is only display metadata.
        let normalized = CallDigits.normalize(number)
        let id = UUID()
        calls[id] = ActiveCall(id: id, line: line, number: normalized, ownsCall: true)
        access[id] = store.sessions[line.backendID]?.accessRevision
        recordings.stopPlayback()
        presented = true
        failure = nil
        do { try await system.request(.start(id, normalized)) }
        catch { finish(id, reason: .failed, cause: "start_request_failed"); failure = .wrapping(error) }
    }

    func answer(_ id: UUID, endCurrent: Bool = false) async {
        if !fixtures, foreground, AVAudioApplication.shared.recordPermission == .undetermined { _ = await AVAudioApplication.requestRecordPermission() }
        do {
            if endCurrent, let current = calls.values.first(where: { $0.ownsCall && $0.id != id && !$0.endRequested }) {
                try await system.request([.end(current.id), .answer(id)])
                return
            }
            try await system.request(.answer(id))
        } catch { failure = .wrapping(error) }
    }

    func act(_ action: CallAction) async {
        do { try await system.request(action) }
        catch { failure = .wrapping(error) }
    }

    func toggleRecording() async {
        guard !demoStopped else { return }
        if recordings.activeID != nil { await recordings.stop(audio: audio)?.value }
        else {
            guard let id = primary?.id else { return }
            await recordings.load()
            guard !Task.isCancelled, !demoStopped, let call = primary, call.id == id, recordings.activeID == nil else { return }
            recordings.start(call: call, audio: audio)
            if demo, recordings.activeCallID == call.id { audio.emitDemoRecording() }
            #if DEBUG
            if fixtures && !demo, recordings.activeCallID == call.id { CallsFixture.emitRecordingAudio(to: audio) }
            #endif
        }
    }

    private func perform(_ action: CallAction) async throws {
        diagnostics.record("call.action.started", id: action.id,
                           fields: ["action": action.diagnosticName, "waitingForBootstrap": String(bootstraps[action.id] != nil)])
        await bootstraps[action.id]?.value
        switch action {
        case .start, .answer:
            // Release playback before configuring/fulfilling CallKit's action.
            // A delayed deactivation must never tear down the new call session.
            await recordings.playback.stop()?.value
            try Task.checkCancellation()
        default: break
        }
        guard let call = calls[action.id] else {
            diagnostics.record("call.action.unavailable", id: action.id,
                               fields: ["action": action.diagnosticName, "alreadyEnded": String(endedUUIDs[action.id] != nil)])
            throw BackendFailure.local("calls.ended")
        }
        do {
            let session = try session(for: call.line)
            guard access[call.id] == session.accessRevision else { throw BackendFailure.local("calls.lineUnavailable") }
            failure = nil
            switch action {
            case .start:
                if !fixtures { try audio.prepare() }
                system.connecting(call.id)
                // Restoring media for this installation never places another carrier call.
                if let record = call.record {
                    if record.connected { system.connected(call.id, at: CallRecord.date(record.answeredAt)) }
                    startMedia(call.id)
                    return
                }
                let submission = CallDial(to: call.number, clientId: clientID, expectedProfileId: call.line.profileID)
                let record: CallRecord
                do {
                    record = try await session.callRequest { api, token in try await api.dial(modemID: call.line.modemID, submission: submission, token: token) }
                } catch {
                    // The response can be lost after the backend accepted the dial.
                    let snapshot = try? await session.callRequest { api, token in try await api.calls(modemID: call.line.modemID, token: token) }
                    guard let accepted = snapshot?.first(where: { !$0.terminal && $0.ownerClientId == self.clientID && $0.number == call.number }) else {
                        failure = .local("calls.dialUnconfirmed")
                        throw error
                    }
                    record = accepted
                }
                guard calls[call.id] != nil else { return }
                ingest(record, line: call.line, id: call.id)
            case .answer:
                guard !calls.values.contains(where: { $0.id != call.id && $0.ownsCall && !$0.endRequested }) else { throw BackendFailure.local("calls.alreadyCalling") }
                guard call.record?.incoming == true else { throw BackendFailure.local("calls.ended") }
                if !fixtures { try audio.prepare() }
                calls[call.id]?.answering = true
                let result = try await update(call, CallUpdate(clientId: clientID, state: "active"))
                calls[call.id]?.ownsCall = true
                calls[call.id]?.answering = false
                ingest(result, line: call.line, id: call.id)
                if foreground { presented = true }
            case .end:
                calls[call.id]?.endRequested = true
                if call.record != nil {
                    do { _ = try await update(call, CallUpdate(clientId: clientID, state: "ended", reason: call.record?.incoming == true ? "busy" : nil)) }
                    catch {
                        if (error as? BackendFailure)?.code == "call_in_use" { finish(call.id, reason: .answeredElsewhere, cause: "other_owner"); return }
                        calls[call.id]?.endRequested = false
                        failure = .local("calls.endUnconfirmed")
                        throw error
                    }
                }
                finish(call.id, reason: .remoteEnded, report: false, cause: "end_action")
            case .hold(_, let held):
                guard call.ownsCall else { throw BackendFailure.local("calls.ended") }
                let result = try await update(call, CallUpdate(clientId: clientID, hold: held ? "local" : "none"))
                ingest(result, line: call.line, id: call.id)
            case .mute(_, let muted):
                calls[call.id]?.muted = muted
                updateAudioInput()
            case .digits(_, let digits):
                guard let record = call.record, record.canSendDTMF, call.ownsCall else { throw BackendFailure.local("calls.dtmfUnavailable") }
                let clientID = clientID
                try await session.callRequest { api, token in
                    try await api.callDTMF(modemID: call.line.modemID, id: record.id, clientID: clientID, digits: digits, token: token)
                }
            }
        } catch {
            diagnostics.record("call.action.failed", id: action.id, fields: ["action": action.diagnosticName], error: error)
            if Task.isCancelled {
                switch action {
                case .hold, .digits, .mute: throw CancellationError()
                default: break
                }
            }
            calls[call.id]?.answering = false
            calls[call.id]?.failure = .wrapping(error)
            if case .answer = action { finish(call.id, reason: (error as? BackendFailure)?.code == "call_in_use" ? .answeredElsewhere : .failed, cause: "answer_failed") }
            if case .start = action { finish(call.id, reason: .failed, cause: "start_failed") }
            failure = failure ?? .wrapping(error)
            throw error
        }
    }

    private func update(_ call: ActiveCall, _ change: CallUpdate) async throws -> CallRecord {
        let session = try session(for: call.line)
        guard let id = call.record?.id else { throw BackendFailure.local("calls.ended") }
        diagnostics.record("call.update.started", id: call.id,
                           fields: ["backendID": call.line.backendID.uuidString, "operation": change.state == "active" ? "answer" : change.state == "ended" ? "end" : "hold"])
        do {
            let record = try await session.callRequest { api, token in try await api.updateCall(modemID: call.line.modemID, id: id, update: change, token: token) }
            diagnostics.record("call.update.completed", id: call.id, fields: CallDiagnostics.snapshot(record, clientID: clientID))
            return record
        } catch {
            diagnostics.record("call.update.failed", id: call.id, error: error)
            let failure = BackendFailure.wrapping(error)
            guard failure.isNetwork || (failure.status ?? 0) >= 500 || failure.status == 408 else { throw error }
            // A dropped response is not permission to replay a carrier action.
            guard let latest = try? await session.callRequest({ api, token in try await api.call(modemID: call.line.modemID, id: id, token: token) }) else { throw error }
            diagnostics.record("call.update.reconciled", id: call.id, fields: CallDiagnostics.snapshot(latest, clientID: clientID))
            if change.state == "ended", latest.terminal { return latest }
            if latest.ownerClientId == clientID {
                if change.state == "active", latest.connected { return latest }
                if let hold = change.hold, latest.connected,
                   (hold == "local" && latest.localHold) || (hold == "none" && !latest.localHold) { return latest }
            }
            throw error
        }
    }

    func ingest(_ record: CallRecord, line: CallLine, id suppliedID: UUID? = nil, source: String = #function) {
        guard !demoStopped, record.profileId == line.profileID, available(line),
              record.backendId?.isEmpty == false, let recordID = record.callUUID.flatMap(UUID.init(uuidString:)) else { return }
        let key = CallIdentity(record, line: line)
        guard ended[key] == nil, endedUUIDs[recordID] == nil else { return }
        let existing = calls.values.first {
            guard $0.line.matches(line) else { return false }
            if let previous = $0.record { return CallIdentity(previous, line: $0.line) == key }
            // The WebSocket can deliver the accepted dial before its HTTP response.
            return $0.ownsCall && record.ownerClientId == clientID && $0.number == record.number
        }
        let id = suppliedID ?? existing?.id ?? recordID
        guard endedUUIDs[id] == nil else { return }
        if let current = calls[id] {
            guard current.line.matches(line), access[id] == store.sessions[line.backendID]?.accessRevision else { return }
        }
        if let old = calls[id]?.record {
            guard record.supersedes(old), old != record else { return }
        }
        var snapshot = CallDiagnostics.snapshot(record, clientID: clientID)
        snapshot["backendID"] = line.backendID.uuidString
        snapshot["source"] = source
        snapshot["previousState"] = calls[id]?.record.map { CallDiagnostics.snapshot($0, clientID: clientID)["state"] ?? "other" } ?? "none"
        diagnostics.record("call.state.changed", id: id, fields: snapshot)
        if record.ownerClientId?.isEmpty == false && record.ownerClientId != clientID {
            finish(id, reason: .answeredElsewhere, cause: "other_owner")
            return
        }
        revision += 1
        if record.terminal {
            if record.state == "failed", calls[id] != nil {
                failure = BackendFailure(code: "call_failed", message: record.reason.isEmpty ? record.stateLabel : record.reasonLabel)
            }
            ended[key] = .now
            finish(id, reason: record.state == "failed" ? .failed : .remoteEnded, cause: "backend_terminal")
            return
        }
        guard calls[id] != nil || bootstraps[id] == nil else { return }
        if record.incoming || record.ownerClientId == clientID {
            rememberCallbackRoute(number: record.number, line: line, startedAt: record.startedAt)
        }
        if calls[id] == nil {
            if record.incoming {
                guard Date().timeIntervalSince(record.date) < 90 else { return }
                calls[id] = ActiveCall(id: id, line: line, record: record, number: record.number, ownsCall: false)
                access[id] = store.sessions[line.backendID]?.accessRevision
                reportIncoming(id)
            } else if record.ownerClientId == clientID {
                calls[id] = ActiveCall(id: id, line: line, record: record, number: record.number, ownsCall: true)
                access[id] = store.sessions[line.backendID]?.accessRevision
                Task { [weak self] in
                    guard let self else { return }
                    do { try await self.system.request(.start(id, record.number)) }
                    catch { self.finish(id, reason: .failed, cause: "restore_request_failed"); self.failure = .wrapping(error) }
                }
            }
            return
        }
        calls[id]?.record = record
        if !record.incoming, record.ownerClientId != clientID {
            finish(id, reason: .answeredElsewhere, cause: "not_incoming_or_other_owner")
            return
        }
        if record.ownerClientId == clientID { calls[id]?.ownsCall = true }
        if record.connected, oldConnected(existing?.record) == false, record.direction == "outgoing" { system.connected(id, at: CallRecord.date(record.answeredAt)) }
        if record.hasMedia, calls[id]?.ownsCall == true { startMedia(id) }
        updateAudioInput()
        updateSystemLabels()
    }

    private func oldConnected(_ record: CallRecord?) -> Bool { record?.connected == true }

    private func reportIncoming(_ id: UUID) {
        guard let call = calls[id] else { return }
        guard reporting.insert(id).inserted else { return }
        updateSystemLabels()
        system.incoming(id: id, number: call.number, name: name(call)) { [weak self] accepted in
            Task { @MainActor in
                self?.reporting.remove(id)
                if !accepted { self?.finish(id, reason: .unanswered, report: false, cause: "incoming_report_rejected") }
            }
        }
    }

    @discardableResult
    func receivePush(content: PushContent?, id: UUID, connection: BackendConfiguration?, completion: @escaping @Sendable () -> Void) -> Task<Void, Never>? {
        let duplicate = calls[id] != nil || reporting.contains(id) || bootstraps[id] != nil
        let issues = CallDiagnostics.validationIssues(content: content, id: id, connection: connection, ended: endedUUIDs[id] != nil)
        let valid = issues.isEmpty
        let started = ContinuousClock.now
        let diagnostics = diagnostics
        var fields = ["valid": String(valid), "duplicate": String(duplicate), "validationIssues": issues.joined(separator: ",")]
        fields["backendID"] = connection?.id.uuidString
        if let expiry = content?.expiresAt { fields["expiresAt"] = String(expiry) }
        diagnostics.record("voip.received", id: id, category: "push", fields: fields)
        // Even an expired or undecryptable VoIP invitation is reported before
        // asynchronous work. Reporting the same UUID makes duplicate delivery safe.
        let result = AsyncStream<Bool>.makeStream(bufferingPolicy: .bufferingNewest(1))
        if !duplicate { reporting.insert(id) }
        system.incoming(id: id, number: valid ? content?.sender ?? "" : "", name: nil) { accepted in
            diagnostics.record("voip.reported", id: id, category: "push",
                               fields: ["accepted": String(accepted), "durationMS": CallDiagnostics.durationMS(since: started)])
            completion()
            result.continuation.yield(accepted)
            result.continuation.finish()
        }
        guard !duplicate else { return nil }
        // Install the task before returning to PushKit. A system answer delivered
        // immediately afterward waits for this snapshot, never for audio activation.
        bootstraps[id] = Task { [weak self] in
            guard let self else { return }
            defer { self.bootstraps[id] = nil; self.reporting.remove(id) }
            var accepted = false
            for await value in result.stream { accepted = value }
            guard accepted else { return }
            guard valid, let content, let connection else { self.endInvitation(id, reason: .failed, cause: "invalid_push"); return }
            var phase = "restore_identity"
            do {
                diagnostics.record("voip.bootstrap.started", id: id, category: "push", fields: ["backendID": connection.id.uuidString])
                self.restoreClientIdentity()
                phase = "resolve_session"
                guard let session = self.store.sessions[connection.id], session.configuration.address == connection.address else { throw BackendFailure.local("calls.lineUnavailable") }
                phase = "refresh_session"
                if !session.canAccessDevices || !session.hasLoadedModems { await session.refresh(force: true) }
                let revision = session.accessRevision
                phase = "resolve_line"
                guard let modem = session.modems.first(where: { $0.id == content.modemId && $0.sim.identifier == content.profileId }) else { throw BackendFailure.local("calls.lineUnavailable") }
                let line = CallLine(session: session, modem: modem)
                phase = "fetch_snapshot"
                diagnostics.record("voip.snapshot.started", id: id, category: "push", fields: ["backendID": connection.id.uuidString])
                let record = try await session.callRequest { api, token in try await api.call(modemID: modem.id, id: content.callId ?? "", token: token) }
                var snapshot = CallDiagnostics.snapshot(record, clientID: self.clientID)
                snapshot["backendID"] = connection.id.uuidString
                snapshot["durationMS"] = CallDiagnostics.durationMS(since: started)
                diagnostics.record("voip.snapshot.received", id: id, category: "push", fields: snapshot)
                phase = "validate_snapshot"
                diagnostics.record("voip.snapshot.validated", id: id, category: "push", fields: [
                    "backendMatches": String(record.backendId == content.backendId),
                    "profileMatches": String(record.profileId == line.profileID),
                    "uuidMatches": String(record.callUUID.flatMap(UUID.init(uuidString:)) == id)
                ])
                guard record.backendId == content.backendId, record.profileId == line.profileID,
                      record.callUUID.flatMap(UUID.init(uuidString:)) == id else { throw BackendFailure.local("calls.ended") }
                try Task.checkCancellation()
                _ = try self.session(for: line)
                guard session === self.store.sessions[connection.id], session.accessRevision == revision,
                      self.available(line) else { throw BackendFailure.local("calls.lineUnavailable") }
                guard self.endedUUIDs[id] == nil else { self.endInvitation(id, reason: .remoteEnded, cause: "already_ended"); return }
                if record.terminal { self.endInvitation(id, reason: record.state == "failed" ? .failed : .remoteEnded, cause: "backend_terminal"); return }
                if !record.incoming || (record.ownerClientId?.isEmpty == false && record.ownerClientId != self.clientID) {
                    self.endInvitation(id, reason: .answeredElsewhere, cause: "not_incoming_or_other_owner")
                    return
                }
                guard (content.expiresAt ?? 0) > Int64(Date().timeIntervalSince1970) else {
                    self.endInvitation(id, reason: .unanswered, cause: "push_expired")
                    return
                }
                self.calls[id] = ActiveCall(id: id, line: line, record: record, number: record.number, ownsCall: false)
                self.access[id] = revision
                self.rememberCallbackRoute(number: record.number, line: line, startedAt: record.startedAt)
                self.updateSystemLabels()
                self.reconcileMonitors()
                diagnostics.record("voip.processed", id: id, category: "push",
                                   fields: ["backendID": connection.id.uuidString, "durationMS": CallDiagnostics.durationMS(since: started)])
                Task { [weak self] in await self?.reloadContacts() }
            } catch {
                diagnostics.record("voip.processing.failed", id: id, category: "push",
                                   fields: ["backendID": connection.id.uuidString, "phase": phase, "durationMS": CallDiagnostics.durationMS(since: started)], error: error)
                guard !Task.isCancelled else { return }
                self.endInvitation(id, reason: .failed, cause: "bootstrap_failed")
                self.failure = .wrapping(error)
            }
        }
        return bootstraps[id]
    }

    private func endInvitation(_ id: UUID, reason: CXCallEndedReason, cause: String, source: String = #function) {
        var fields = CallDiagnostics.ending(reason)
        fields["cause"] = cause
        fields["source"] = source
        diagnostics.record("voip.ended", id: id, category: "push", fields: fields)
        endedUUIDs[id] = .now
        system.ended(id, reason: reason)
    }

    func run() async {
        defer { for monitor in monitors.values { monitor.task.cancel() }; monitors.removeAll() }
        while !Task.isCancelled && !demoStopped {
            await reconcile()
            ended = ended.filter { $0.value > Date().addingTimeInterval(-3600) }
            endedUUIDs = endedUUIDs.filter { $0.value > Date().addingTimeInterval(-3600) }
            do { try await Task.sleep(for: .seconds(2)) } catch { return }
        }
    }

    func reconcile() async {
        guard !Task.isCancelled, !demoStopped else { return }
        // A PushKit cold launch may precede the first unlock after reboot.
        restoreClientIdentity()
        reconcileMonitors()
        await reconcileCalls()
    }

    private func reconcileMonitors() {
        var wanted: Set<MonitorKey> = []
        for session in store.sessions.values where session.canAccessDevices && session.capabilities?.contains("nativeCalls") == true {
            for modem in session.modems {
                let line = CallLine(session: session, modem: modem)
                guard foreground || calls.values.contains(where: { $0.line.id == line.id }) else { continue }
                let key = MonitorKey(line: line.id, address: line.address,
                                     session: ObjectIdentifier(session), access: session.accessRevision)
                wanted.insert(key)
                if monitors[key] == nil {
                    let id = UUID()
                    let task = Task { [weak self] in
                        await self?.monitor(line)
                        if self?.monitors[key]?.id == id { self?.monitors[key] = nil }
                    }
                    monitors[key] = Monitor(id: id, task: task)
                }
            }
        }
        for key in Array(monitors.keys) where !wanted.contains(key) { monitors.removeValue(forKey: key)?.task.cancel() }
    }

    private func monitor(_ line: CallLine) async {
        var attempt = 0
        while !Task.isCancelled {
            do {
                let session = try session(for: line)
                let revision = session.accessRevision
                let connected = try await session.callRequest { api, token in try await api.socket(["modems", line.modemID, "calls", "events"], token: token) }
                defer { connected.close() }
                let snapshot = try await session.callRequest { api, token in try await api.calls(modemID: line.modemID, token: token) }
                try Task.checkCancellation()
                guard revision == session.accessRevision else { return }
                for record in snapshot where !record.terminal { ingest(record, line: line) }
                attempt = 0
                try await withTaskCancellationHandler {
                    while !Task.isCancelled {
                        let event = try JSONDecoder().decode(CallEvent.self, from: await connected.receive())
                        try Task.checkCancellation()
                        guard revision == session.accessRevision else { return }
                        if event.type == "call", let call = event.call { ingest(call, line: line) }
                    }
                } onCancel: { connected.close() }
            } catch { /* The periodic snapshot reconciles dropped events and expired access. */ }
            attempt = min(attempt + 1, 4)
            do { try await Task.sleep(for: .seconds(min(15, 1 << attempt))) } catch { return }
        }
    }

    private func reconcileCalls() async {
        for call in Array(calls.values) where !call.endRequested {
            guard bootstraps[call.id] == nil else { continue }
            do {
                let session = try session(for: call.line)
                guard access[call.id] == session.accessRevision else { throw BackendFailure.local("calls.lineUnavailable") }
                guard let record = call.record else { continue }
                let latest = try await session.callRequest { api, token in try await api.call(modemID: call.line.modemID, id: record.id, token: token) }
                guard calls[call.id] != nil else { continue }
                ingest(latest, line: call.line, id: call.id)
                calls[call.id]?.failure = nil
                if latest.incoming, Date().timeIntervalSince(latest.date) > 60 { finish(call.id, reason: .unanswered, cause: "incoming_timeout") }
            } catch {
                guard !Task.isCancelled else { return }
                let failure = BackendFailure.wrapping(error)
                let session = try? session(for: call.line)
                let accessChanged = session == nil || access[call.id] != session?.accessRevision
                let missing = failure.source == .backend && failure.status == 404 && failure.code == "call_not_found"
                if accessChanged || missing { finish(call.id, reason: .failed, cause: accessChanged ? "access_changed" : "call_not_found") }
                else {
                    // A failed observation does not prove that the carrier call ended.
                    // Keep media and identity so the next event/read can recover it.
                    calls[call.id]?.failure = failure
                }
            }
        }
    }

    private func startMedia(_ id: UUID) {
        guard mediaID != id, let call = calls[id], let record = call.record, record.hasMedia else { return }
        stopMedia()
        mediaID = id
        diagnostics.record("call.media.started", id: id, fields: ["attempt": String(mediaAttempts + 1)])
        if fixtures {
            audio.startLocal()
            updateAudioInput()
            return
        }
        mediaTask = Task { [weak self] in
            guard let self else { return }
            do {
                let session = try self.session(for: call.line)
                let ice = try await session.callRequest { api, token in try await api.callICE(token: token) }
                let socket = try await session.callRequest { api, token in try await api.socket(["modems", call.line.modemID, "calls", record.id, "webrtc", "sessions"], token: token) }
                guard self.mediaID == id, !Task.isCancelled else { socket.close(); return }
                self.updateAudioInput()
                try await self.audio.start(socket: socket, ice: ice, clientID: self.clientID)
                self.updateAudioInput()
            } catch { if !Task.isCancelled, self.mediaID == id { self.mediaFailed(.wrapping(error)) } }
        }
    }

    private func mediaFailed(_ error: BackendFailure) {
        guard let id = mediaID else { return }
        diagnostics.record("call.media.failed", id: id, fields: ["attempt": String(mediaAttempts + 1)], error: error)
        stopMedia()
        mediaAttempts += 1
        if error.code == "call_in_use" { finish(id, reason: .answeredElsewhere, cause: "media_other_owner"); return }
        guard mediaAttempts <= 2 else { failure = error; Task { [weak self] in await self?.act(.end(id)) }; return }
        mediaTask = Task { [weak self] in
            do { try await Task.sleep(for: .seconds(1)) } catch { return }
            self?.startMedia(id)
        }
    }

    private func updateAudioInput() {
        guard let id = mediaID, let call = calls[id] else { return }
        audio.setInput(enabled: !call.muted && call.record?.localHold != true && call.record?.remoteHold != true)
    }

    private func updateSystemLabels() {
        for call in calls.values {
            system.update(id: call.id, number: call.number, name: name(call), holding: incoming == nil || incoming?.id == call.id)
        }
    }

    func reloadContacts() async {
        await contacts.reload()
        for session in store.onlineSessions {
            do { try await session.refreshContacts() }
            catch { /* Keep local names as a fallback while an address book is unavailable. */ }
        }
        updateSystemLabels()
    }

    private func stopMedia() {
        if let id = mediaID { diagnostics.record("call.media.stopped", id: id) }
        mediaTask?.cancel(); mediaTask = nil; mediaID = nil; audio.stop()
    }

    private func finish(_ id: UUID, reason: CXCallEndedReason, report: Bool = true, cause: String, source: String = #function) {
        guard let call = calls.removeValue(forKey: id) else {
            if bootstraps[id] != nil || reporting.contains(id) { endInvitation(id, reason: reason, cause: cause, source: source) }
            return
        }
        // Keep the final state and timestamps within the log store's 16-field
        // limit. Routing/hold details are already in the state-change events.
        var fields = call.record.map { CallDiagnostics.snapshot($0, clientID: clientID).filter { !["direction", "route", "hold"].contains($0.key) } } ?? [:]
        fields.merge(CallDiagnostics.ending(reason)) { _, new in new }
        fields["backendID"] = call.line.backendID.uuidString
        fields["cause"] = cause
        fields["source"] = source
        fields["reportToCallKit"] = String(report)
        diagnostics.record("call.finished", id: id, fields: fields)
        endedUUIDs[id] = .now
        if let uuid = call.record?.callUUID.flatMap(UUID.init(uuidString:)) { endedUUIDs[uuid] = .now }
        if let record = call.record { ended[CallIdentity(record, line: call.line)] = .now }
        if recordings.activeCallID == id { recordings.stop(audio: audio) }
        if mediaID == id { stopMedia() }
        access[id] = nil
        revision += 1
        if report { system.ended(id, reason: reason) }
        if calls.isEmpty { presented = false; mediaAttempts = 0 }
        updateSystemLabels()
    }

    func stopDemo() async {
        guard demo else { return }
        demoStopped = true
        let tasks = monitors.values.map(\.task) + Array(bootstraps.values)
        for task in tasks { task.cancel() }
        monitors.removeAll()
        bootstraps.removeAll()
        for task in tasks { await task.value }
        await recordings.load()
        await recordings.stop(audio: audio)?.value
        await recordings.playback.stop()?.value
        stopMedia()
        calls.removeAll()
        presented = false
    }

    private func reset() {
        // Invitations are system calls even before their backend snapshot arrives.
        // Tombstone them before cancelling so a late result cannot recreate one.
        for id in Set(bootstraps.keys).union(reporting) { endedUUIDs[id] = .now }
        for task in bootstraps.values { task.cancel() }
        bootstraps.removeAll()
        reporting.removeAll()
        systemAudioActive = false
        for call in Array(calls.values) {
            let revision = access[call.id]
            finish(call.id, reason: .failed, report: false, cause: "provider_reset")
            Task { [weak self] in
                guard let self, let session = try? self.session(for: call.line), session.accessRevision == revision else { return }
                do { _ = try await self.update(call, CallUpdate(clientId: self.clientID, state: "ended")) }
                catch {
                    if (error as? BackendFailure)?.code != "call_in_use" { self.failure = .local("calls.endUnconfirmed") }
                }
            }
        }
        stopMedia()
        updatePlaybackAvailability()
    }
}

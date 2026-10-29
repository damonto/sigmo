import AVFAudio
import Foundation
import Observation
import UIKit
@preconcurrency import LiveKitWebRTC

struct CallAudioRoute: Identifiable { let id, name: String }

@MainActor @Observable
final class CallAudio {
    private(set) var ready = false
    private(set) var receiveOnly = false
    private(set) var connecting = false
    private(set) var routeName = ""
    private(set) var routes: [CallAudioRoute] = []
    private(set) var microphones: [CallAudioRoute] = []
    private(set) var failure: BackendFailure?
    var onFailure: (@MainActor (BackendFailure) -> Void)?
    var onReady: (@MainActor () -> Void)?
    let capture = CallPCMTap()
    let render = CallPCMTap()

    @ObservationIgnored private var factory: LKRTCPeerConnectionFactory?
    @ObservationIgnored private var peer: LKRTCPeerConnection?
    @ObservationIgnored private var delegate: CallPeerDelegate?
    @ObservationIgnored private var input: LKRTCAudioTrack?
    @ObservationIgnored private var socket: BackendSocket?
    @ObservationIgnored private var receiveTask: Task<Void, Never>?
    @ObservationIgnored private var sendTask: Task<Void, Never>?
    @ObservationIgnored private var connectionTimeoutTask: Task<Void, Never>?
    @ObservationIgnored private var lossTask: Task<Void, Never>?
    @ObservationIgnored private var routeTask: Task<Void, Never>?
    @ObservationIgnored private var generation = UUID()
    @ObservationIgnored private var offerSent = false
    @ObservationIgnored private var remoteSet = false
    @ObservationIgnored private var localCandidates: [CallCandidate] = []
    @ObservationIgnored private var remoteCandidates: [CallCandidate] = []
    @ObservationIgnored private var captureSink: (@Sendable (CallPCM) -> Void)?
    @ObservationIgnored private var renderSink: (@Sendable (CallPCM) -> Void)?
    @ObservationIgnored private var inputEnabled = true
    @ObservationIgnored private var paused = false
    @ObservationIgnored private var local = false
    func startLocal() {
        stop()
        local = true
        receiveOnly = false
        routes = [CallAudioRoute(id: "speaker", name: callText("calls.speaker")),
                  CallAudioRoute(id: "receiver", name: callText("calls.receiver"))]
        microphones = []
        routeName = callText("calls.speaker")
        ready = true
        onReady?()
    }

    #if DEBUG
    func startFixture() { startLocal() }

    func emitFixturePCM(_ pcm: CallPCM, remote: Bool) {
        if remote { renderSink?(pcm) } else { captureSink?(pcm) }
    }
    #endif

    func emitDemoRecording() {
        let samples = (0..<24_000).map { Float(sin(2 * .pi * 440 * Double($0) / 8_000)) * 0.08 }
        renderSink?(CallPCM(samples: samples, sampleRate: 8_000, time: ProcessInfo.processInfo.systemUptime))
    }

    func prepare() throws {
        try CallWebRTC.configureAudio()
        if factory == nil { factory = CallWebRTC.factory(capture: capture, render: render) }
    }

    /// Takes ownership of the socket, including when startup is cancelled or fails.
    func start(socket: BackendSocket, ice: [CallICE], clientID: String, timeout: Duration = .seconds(20)) async throws {
        guard !Task.isCancelled else { socket.close(); throw CancellationError() }
        stop()
        let current = generation
        var started = false
        defer { if !started, generation == current { stop() } }
        self.socket = socket
        connecting = true
        failure = nil
        // A silent signaling server may never produce an ICE failure callback.
        let deadline = ContinuousClock.now.advanced(by: timeout)
        connectionTimeoutTask = Task { [weak self] in
            do { try await Task.sleep(until: deadline, clock: .continuous) } catch { return }
            guard let self, self.generation == current, !self.ready else { return }
            AppDiagnostics.record("audio.connection.timed_out", category: "audio",
                                  fields: ["offerSent": String(self.offerSent), "answerReceived": String(self.remoteSet)])
            self.fail(.local("calls.audioFailed"))
        }
        if factory == nil { factory = CallWebRTC.factory(capture: capture, render: render) }
        let configuration = LKRTCConfiguration()
        configuration.sdpSemantics = .unifiedPlan
        configuration.iceServers = ice.map { LKRTCIceServer(urlStrings: $0.urls, username: $0.username, credential: $0.credential) }
        let delegate = CallPeerDelegate { [weak self] event in
            Task { @MainActor [weak self] in
                guard let self, self.generation == current else { return }
                self.receive(event)
            }
        }
        self.delegate = delegate
        guard let peer = factory?.peerConnection(with: configuration,
            constraints: LKRTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil), delegate: delegate) else {
            throw BackendFailure.local("calls.audioFailed")
        }
        self.peer = peer
        receiveOnly = AVAudioApplication.shared.recordPermission != .granted || !AVAudioSession.sharedInstance().isInputAvailable
        if !receiveOnly, let track = factory?.audioTrack(withTrackId: "sigmo-microphone") {
            input = track
            track.isEnabled = inputEnabled
            peer.add(track, streamIds: ["sigmo-call"])
        } else {
            let options = LKRTCRtpTransceiverInit()
            options.direction = .recvOnly
            peer.addTransceiver(of: .audio, init: options)
        }
        receiveTask = Task { [weak self] in
            do {
                while !Task.isCancelled {
                    let data = try await socket.receive()
                    guard let self, self.generation == current else { return }
                    try await self.signal(JSONDecoder().decode(CallSignal.self, from: data), generation: current)
                }
            } catch {
                guard let self, self.generation == current, !Task.isCancelled else { return }
                // A connected peer owns its transport independently of the signaling socket.
                if !self.ready { self.fail(.wrapping(error)) }
            }
        }
        if let factory {
            let supported = factory.rtpSenderCapabilities(forKind: "audio").codecs
            let preferred = ["opus", "pcmu"].flatMap { name in
                supported.filter { $0.name.lowercased() == name }
            }
            for transceiver in peer.transceivers {
                try transceiver.setCodecPreferences(preferred, error: ())
            }
        }
        let offer = try await peer.offer(for: LKRTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil))
        guard generation == current, !Task.isCancelled else { throw CancellationError() }
        try await peer.setLocalDescription(offer)
        guard generation == current, !Task.isCancelled else { throw CancellationError() }
        try await socket.send(JSONEncoder().encode(CallSignal(type: "offer", clientId: clientID,
            offer: CallSDP(type: "offer", sdp: offer.sdp))))
        guard generation == current, !Task.isCancelled else { throw CancellationError() }
        offerSent = true
        for candidate in localCandidates { enqueue(CallSignal(type: "candidate", candidate: candidate)) }
        localCandidates.removeAll()
        observeRoutes()
        started = true
    }

    private func signal(_ message: CallSignal, generation current: UUID) async throws {
        guard generation == current, let peer else { return }
        switch message.type {
        case "answer":
            guard let answer = message.answer, answer.type == "answer" else { throw BackendFailure.local("api.invalidResponse") }
            try await peer.setRemoteDescription(LKRTCSessionDescription(type: .answer, sdp: answer.sdp))
            guard generation == current else { throw CancellationError() }
            remoteSet = true
            let candidates = remoteCandidates
            remoteCandidates.removeAll()
            for candidate in candidates {
                guard generation == current else { throw CancellationError() }
                try await add(candidate, to: peer)
            }
        case "candidate":
            guard let candidate = message.candidate else { return }
            if remoteSet { try await add(candidate, to: peer) }
            else { remoteCandidates.append(candidate) }
        case "error":
            throw BackendFailure(status: message.code == "call_in_use" ? 409 : 503,
                code: message.code ?? "call_media_unavailable", message: callText("calls.audioFailed"))
        default: break
        }
    }

    private func add(_ candidate: CallCandidate, to peer: LKRTCPeerConnection) async throws {
        try await peer.add(LKRTCIceCandidate(sdp: candidate.candidate,
            sdpMLineIndex: candidate.sdpMLineIndex ?? 0, sdpMid: candidate.sdpMid))
    }

    private func receive(_ event: CallPeerEvent) {
        switch event {
        case .candidate(let candidate):
            if offerSent { enqueue(CallSignal(type: "candidate", candidate: candidate)) }
            else { localCandidates.append(candidate) }
        case .connected:
            connectionTimeoutTask?.cancel()
            connectionTimeoutTask = nil
            lossTask?.cancel()
            lossTask = nil
            ready = true
            connecting = false
            onReady?()
        case .disconnected:
            guard lossTask == nil else { return }
            connecting = true
            let current = generation
            lossTask = Task { [weak self] in
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
                guard let self, self.generation == current else { return }
                self.fail(.local("calls.audioFailed"))
            }
        case .failed: fail(.local("calls.audioFailed"))
        }
    }

    private func enqueue(_ message: CallSignal) {
        let previous = sendTask
        let current = generation
        guard let socket else { return }
        sendTask = Task { [weak self] in
            await previous?.value
            guard let self, self.generation == current, !Task.isCancelled else { return }
            do { try await socket.send(JSONEncoder().encode(message)) }
            catch { if !Task.isCancelled, !self.ready { self.fail(.wrapping(error)) } }
        }
    }

    private func fail(_ error: BackendFailure) {
        AppDiagnostics.record(error, category: "audio", operation: "audio.failed")
        stop()
        failure = error
        onFailure?(error)
    }

    func stop() {
        local = false
        generation = UUID()
        receiveTask?.cancel(); receiveTask = nil
        sendTask?.cancel(); sendTask = nil
        connectionTimeoutTask?.cancel(); connectionTimeoutTask = nil
        lossTask?.cancel(); lossTask = nil
        routeTask?.cancel(); routeTask = nil
        socket?.close(); socket = nil
        peer?.close(); peer = nil
        if paused { CallWebRTC.setPaused(false); paused = false }
        delegate = nil
        input = nil
        offerSent = false; remoteSet = false
        localCandidates.removeAll(); remoteCandidates.removeAll()
        ready = false; connecting = false
    }

    func setInput(enabled: Bool) {
        inputEnabled = enabled
        input?.isEnabled = enabled
        capture.setSilenced(!enabled || receiveOnly)
    }

    func record(capture: (@Sendable (CallPCM) -> Void)?, render: (@Sendable (CallPCM) -> Void)?,
                onFailure: (@Sendable () -> Void)? = nil) {
        captureSink = capture; renderSink = render
        self.capture.configure(silenced: !inputEnabled || receiveOnly, sink: capture, onFailure: onFailure)
        self.render.configure(silenced: false, sink: render, onFailure: onFailure)
    }

    func recordingInterrupted() {
        capture.markDiscontinuity()
        render.markDiscontinuity()
    }

    func setPaused(_ paused: Bool) {
        guard !local, self.paused != paused else { return }
        self.paused = paused
        CallWebRTC.setPaused(paused)
        recordingInterrupted()
    }

    func selectRoute(_ id: String) {
        if local {
            if let route = routes.first(where: { $0.id == id }) { routeName = route.name }
            return
        }
        let session = AVAudioSession.sharedInstance()
        do {
            try session.overrideOutputAudioPort(id == "speaker" ? .speaker : .none)
            if id != "speaker" {
                try session.setPreferredInput(session.availableInputs?.first { $0.uid == id }
                    ?? session.availableInputs?.first { $0.portType == .builtInMic })
            }
            refreshRoutes()
        } catch {
            AppDiagnostics.record(error, category: "audio", operation: "route.failed")
            failure = .wrapping(error)
        }
    }

    func selectMicrophone(_ id: String) {
        guard !local else { return }
        do { try AVAudioSession.sharedInstance().setPreferredInput(AVAudioSession.sharedInstance().availableInputs?.first { $0.uid == id }) }
        catch {
            AppDiagnostics.record(error, category: "audio", operation: "microphone.failed")
            failure = .wrapping(error)
        }
        refreshRoutes()
    }

    func refreshRoutes() {
        guard !local else { return }
        let session = AVAudioSession.sharedInstance()
        microphones = (session.availableInputs ?? []).map { CallAudioRoute(id: $0.uid, name: $0.portName) }
        routes = [CallAudioRoute(id: "speaker", name: callText("calls.speaker"))]
        if UIDevice.current.userInterfaceIdiom == .phone { routes.insert(CallAudioRoute(id: "receiver", name: callText("calls.receiver")), at: 0) }
        routes += (session.availableInputs ?? []).filter { $0.portType != .builtInMic }.map { CallAudioRoute(id: $0.uid, name: $0.portName) }
        routeName = session.currentRoute.outputs.map(\.portName).joined(separator: ", ")
    }

    private func observeRoutes() {
        refreshRoutes()
        routeTask = Task { [weak self] in
            for await _ in NotificationCenter.default.notifications(named: AVAudioSession.routeChangeNotification) {
                guard !Task.isCancelled else { return }
                self?.refreshRoutes()
            }
        }
    }
}

private enum CallPeerEvent: Sendable { case candidate(CallCandidate), connected, disconnected, failed }

// Only value snapshots cross from the SDK's signaling thread to the main actor.
private final class CallPeerDelegate: NSObject, LKRTCPeerConnectionDelegate, Sendable {
    let emit: @Sendable (CallPeerEvent) -> Void
    init(emit: @escaping @Sendable (CallPeerEvent) -> Void) { self.emit = emit }
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange stateChanged: LKRTCSignalingState) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didAdd stream: LKRTCMediaStream) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didRemove stream: LKRTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: LKRTCPeerConnection) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange newState: LKRTCIceGatheringState) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didRemove candidates: [LKRTCIceCandidate]) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didOpen dataChannel: LKRTCDataChannel) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didGenerate candidate: LKRTCIceCandidate) {
        emit(.candidate(CallCandidate(candidate: candidate.sdp, sdpMid: candidate.sdpMid, sdpMLineIndex: candidate.sdpMLineIndex)))
    }
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange newState: LKRTCIceConnectionState) {
        switch newState {
        case .connected, .completed: emit(.connected)
        case .disconnected: emit(.disconnected)
        case .failed: emit(.failed)
        default: break
        }
    }
}

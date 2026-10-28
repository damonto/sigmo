import AVFAudio
import XCTest
@preconcurrency import LiveKitWebRTC
@testable import Sigmo

@MainActor
final class CallPCMTransportTests: XCTestCase {
    func testNativeOpusTransportsBothDirectionsWithoutMicrophoneHardware() async throws {
        try await transport(codec: "opus", clockRate: 48000)
    }

    func testNativePCMUTransportsBothDirectionsWithoutMicrophoneHardware() async throws {
        try await transport(codec: "PCMU", clockRate: 8000)
    }

    private func transport(codec: String, clockRate: Int) async throws {
        let firstDevice = CallTestAudioDevice(frequency: 440)
        let secondDevice = CallTestAudioDevice(frequency: 880)
        let firstFactory = LKRTCPeerConnectionFactory(encoderFactory: nil, decoderFactory: nil, audioDevice: firstDevice)
        let secondFactory = LKRTCPeerConnectionFactory(encoderFactory: nil, decoderFactory: nil, audioDevice: secondDevice)
        let delegate = CallTestPeerDelegate()
        let configuration = LKRTCConfiguration()
        configuration.sdpSemantics = .unifiedPlan
        configuration.iceServers = []
        let constraints = LKRTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        let first = try XCTUnwrap(firstFactory.peerConnection(with: configuration, constraints: constraints, delegate: delegate))
        let second = try XCTUnwrap(secondFactory.peerConnection(with: configuration, constraints: constraints, delegate: delegate))
        defer { first.close(); second.close() }
        for (factory, peer) in [(firstFactory, first), (secondFactory, second)] {
            peer.add(factory.audioTrack(withTrackId: UUID().uuidString), streamIds: ["synthetic"])
            let codecs = factory.rtpSenderCapabilities(forKind: "audio").codecs.filter { $0.name.lowercased() == codec.lowercased() }
            XCTAssertFalse(codecs.isEmpty)
            for transceiver in peer.transceivers {
                // Swift keeps the error label to disambiguate the deprecated overload.
                try transceiver.setCodecPreferences(codecs, error: ())
            }
        }
        try await first.setLocalDescription(first.offer(for: constraints))
        try await gather(first)
        try await second.setRemoteDescription(try XCTUnwrap(first.localDescription))
        try await second.setLocalDescription(second.answer(for: constraints))
        try await gather(second)
        let answer = try XCTUnwrap(second.localDescription)
        XCTAssertTrue(answer.sdp.lowercased().contains("\(codec.lowercased())/\(clockRate)"))
        try await first.setRemoteDescription(answer)
        for _ in 0..<500 {
            firstDevice.tick(); secondDevice.tick()
            if firstDevice.receivedFrames > 10 && secondDevice.receivedFrames > 10 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertGreaterThan(firstDevice.receivedFrames, 10, "880 Hz remote PCM should reach the first device")
        XCTAssertGreaterThan(secondDevice.receivedFrames, 10, "440 Hz remote PCM should reach the second device")
    }

    func testManualAudioFollowsActivationAndSurvivesReconfiguration() throws {
        try CallWebRTC.configureAudio()
        XCTAssertTrue(LKRTCAudioSession.sharedInstance().useManualAudio)
        CallWebRTC.activate(AVAudioSession.sharedInstance())
        defer { CallWebRTC.deactivate(AVAudioSession.sharedInstance()) }
        XCTAssertTrue(LKRTCAudioSession.sharedInstance().isAudioEnabled)
        try CallWebRTC.configureAudio()
        XCTAssertTrue(LKRTCAudioSession.sharedInstance().isAudioEnabled)
        CallWebRTC.deactivate(AVAudioSession.sharedInstance())
        XCTAssertFalse(LKRTCAudioSession.sharedInstance().isAudioEnabled)
    }

    func testPauseStopsBothAudioDirectionsUntilResume() throws {
        try CallWebRTC.configureAudio()
        CallWebRTC.activate(AVAudioSession.sharedInstance())
        defer {
            CallWebRTC.setPaused(false)
            CallWebRTC.deactivate(AVAudioSession.sharedInstance())
        }
        CallWebRTC.setPaused(true)
        XCTAssertFalse(LKRTCAudioSession.sharedInstance().isAudioEnabled)
        try CallWebRTC.configureAudio()
        XCTAssertFalse(LKRTCAudioSession.sharedInstance().isAudioEnabled)
        CallWebRTC.setPaused(false)
        XCTAssertTrue(LKRTCAudioSession.sharedInstance().isAudioEnabled)
    }

    private func gather(_ peer: LKRTCPeerConnection) async throws {
        for _ in 0..<300 {
            if peer.iceGatheringState == .complete { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTFail("Local ICE gathering did not complete")
    }
}

// The SDK's documented injectable ADM feeds signed 16-bit PCM. Its delegate
// dispatch keeps all callbacks on one native thread; no real microphone, audio
// route, carrier, signaling service, STUN or TURN server is used by this test.
private final class CallTestAudioDevice: NSObject, LKRTCAudioDevice, @unchecked Sendable {
    let deviceInputSampleRate = 48_000.0, deviceOutputSampleRate = 48_000.0
    let inputIOBufferDuration = 0.01, outputIOBufferDuration = 0.01
    let inputNumberOfChannels = 1, outputNumberOfChannels = 1
    let inputLatency = 0.0, outputLatency = 0.0
    private(set) var isInitialized = false
    private(set) var isPlayoutInitialized = false
    private(set) var isRecordingInitialized = false
    private(set) var isPlaying = false
    private(set) var isRecording = false
    private var delegate: (any LKRTCAudioDeviceDelegate)?
    private let lock = NSLock()
    private var audibleFrames = 0
    private var sample = 0
    private let frequency: Double
    var receivedFrames: Int { lock.withLock { audibleFrames } }

    init(frequency: Double) { self.frequency = frequency }
    func initialize(with delegate: any LKRTCAudioDeviceDelegate) -> Bool {
        lock.withLock { self.delegate = delegate }
        isInitialized = true
        return true
    }
    func terminateDevice() -> Bool { lock.withLock { delegate = nil }; isInitialized = false; return true }
    func initializePlayout() -> Bool { isPlayoutInitialized = true; return true }
    func initializeRecording() -> Bool { isRecordingInitialized = true; return true }
    func startPlayout() -> Bool { isPlaying = true; return true }
    func stopPlayout() -> Bool { isPlaying = false; return true }
    func startRecording() -> Bool { isRecording = true; return true }
    func stopRecording() -> Bool { isRecording = false; return true }

    func tick() {
        guard let delegate = lock.withLock({ delegate }) else { return }
        delegate.dispatchSync { [self] in
            var timestamp = AudioTimeStamp()
            timestamp.mSampleTime = Double(sample)
            timestamp.mFlags = .sampleTimeValid
            var flags: AudioUnitRenderActionFlags = []
            if isPlaying {
                var output = [Int16](repeating: 0, count: 480)
                output.withUnsafeMutableBytes { bytes in
                    var buffer = AudioBufferList(mNumberBuffers: 1, mBuffers: AudioBuffer(mNumberChannels: 1, mDataByteSize: 960, mData: bytes.baseAddress))
                    _ = delegate.getPlayoutData(&flags, &timestamp, 0, 480, &buffer)
                }
                if output.contains(where: { abs(Int($0)) > 100 }) { lock.withLock { audibleFrames += 1 } }
            }
            if isRecording {
                var input = (0..<480).map { offset in Int16(sin(2 * .pi * frequency * Double(sample + offset) / 48_000) * 8000) }
                input.withUnsafeMutableBytes { bytes in
                    var buffer = AudioBufferList(mNumberBuffers: 1, mBuffers: AudioBuffer(mNumberChannels: 1, mDataByteSize: 960, mData: bytes.baseAddress))
                    _ = delegate.deliverRecordedData(&flags, &timestamp, 0, 480, &buffer, nil, nil)
                }
            }
            sample += 480
        }
    }
}

private final class CallTestPeerDelegate: NSObject, LKRTCPeerConnectionDelegate, Sendable {
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange stateChanged: LKRTCSignalingState) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didAdd stream: LKRTCMediaStream) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didRemove stream: LKRTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: LKRTCPeerConnection) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange newState: LKRTCIceGatheringState) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didChange newState: LKRTCIceConnectionState) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didRemove candidates: [LKRTCIceCandidate]) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didOpen dataChannel: LKRTCDataChannel) {}
    func peerConnection(_ peerConnection: LKRTCPeerConnection, didGenerate candidate: LKRTCIceCandidate) {}
}

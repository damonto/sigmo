import AVFAudio
import Foundation
import Synchronization
@preconcurrency import LiveKitWebRTC

/// WebRTC's audio thread only publishes PCM. The queue owns callbacks and drains
/// the bounded buffer; recorder locks, allocations and disk work stay off audio.
final class CallPCMTap: NSObject, LKRTCAudioCustomProcessingDelegate, @unchecked Sendable {
    private let queue: DispatchQueue
    private let timer: DispatchSourceTimer
    private let buffer: CallPCMBuffer
    private let processInfo = ProcessInfo.processInfo
    private let recording = Atomic<UInt>(0)
    private let overflow = Atomic<UInt>(0)
    private let sampleRate = Atomic<Int>(48_000)
    private let silenced = Atomic<Bool>(false)
    private let discontinuity = Atomic<Bool>(true)
    // Accessed only on queue. A generation prevents a callback in flight at Stop
    // from delivering old audio to the next recording.
    private var generation: UInt = 0
    private var sink: (@Sendable (CallPCM) -> Void)?
    private var onFailure: (@Sendable () -> Void)?

    init(buffer: CallPCMBuffer = CallPCMBuffer(),
         queue: DispatchQueue = DispatchQueue(label: "com.damon.sigmo.pcm-tap", qos: .userInitiated)) {
        self.buffer = buffer
        self.queue = queue
        timer = DispatchSource.makeTimerSource(queue: queue)
        super.init()
        timer.setEventHandler { [weak self] in self?.drain() }
        timer.schedule(deadline: .distantFuture)
        timer.resume()
    }

    deinit { timer.cancel() }

    @MainActor func configure(silenced: Bool, sink: (@Sendable (CallPCM) -> Void)?, onFailure: (@Sendable () -> Void)? = nil) {
        queue.sync {
            recording.store(0, ordering: .releasing)
            drain()
            generation += 1
            self.sink = sink
            self.onFailure = onFailure
            self.silenced.store(silenced, ordering: .relaxed)
            discontinuity.store(true, ordering: .releasing)
            if sink != nil {
                recording.store(generation, ordering: .releasing)
                timer.schedule(deadline: .now(), repeating: .milliseconds(10))
            } else {
                timer.schedule(deadline: .distantFuture)
            }
        }
    }

    func setSilenced(_ value: Bool) {
        silenced.store(value, ordering: .relaxed)
    }

    func audioProcessingInitialize(sampleRate: Int, channels: Int) {
        self.sampleRate.store(sampleRate, ordering: .relaxed)
        markDiscontinuity()
    }

    func audioProcessingProcess(audioBuffer: LKRTCAudioBuffer) {
        process(frameCount: audioBuffer.frames, channelCount: audioBuffer.channels) {
            audioBuffer.rawBuffer(forChannel: $0)
        }
    }

    // The nonescaping channel access also lets tests exercise the native boundary
    // with borrowed PCM, without using a microphone or manufacturing SDK objects.
    func process(frameCount: Int, channelCount: Int, channelData: (Int) -> UnsafeMutablePointer<Float>) {
        let current = recording.load(ordering: .acquiring)
        guard current != 0, overflow.load(ordering: .acquiring) != current,
              frameCount > 0, channelCount > 0 else { return }
        let header = CallPCMBuffer.Header(recording: current,
            sampleRate: Double(sampleRate.load(ordering: .relaxed)), time: processInfo.systemUptime,
            discontinuity: discontinuity.exchange(false, ordering: .acquiringAndReleasing))
        let muted = silenced.load(ordering: .relaxed)
        let accepted = buffer.append(count: frameCount, header: header) { mono in
            for frame in mono.indices { mono[frame] = 0 }
            guard !muted else { return }
            let scale = 1 / (Float(channelCount) * 32768)
            for channel in 0..<channelCount {
                let source = channelData(channel)
                for frame in mono.indices { mono[frame] += source[frame] * scale }
            }
        }
        if !accepted { overflow.store(current, ordering: .releasing) }
    }

    private func drain() {
        for _ in 0..<buffer.capacity {
            guard let frame = buffer.pop() else { break }
            if frame.recording == generation { sink?(frame.pcm) }
        }
        guard sink != nil, overflow.load(ordering: .acquiring) == generation else { return }
        recording.store(0, ordering: .releasing)
        timer.schedule(deadline: .distantFuture)
        sink = nil
        let failure = onFailure
        onFailure = nil
        failure?()
    }

    func markDiscontinuity() {
        discontinuity.store(true, ordering: .releasing)
    }

    func audioProcessingRelease() { markDiscontinuity() }
}

@MainActor
enum CallWebRTC {
    private(set) static var isActive = false
    static func factory(capture: CallPCMTap, render: CallPCMTap) -> LKRTCPeerConnectionFactory {
        return LKRTCPeerConnectionFactory(audioDeviceModuleType: .platformDefault,
            bypassVoiceProcessing: false, encoderFactory: nil, decoderFactory: nil,
            audioProcessingModule: processing(capture: capture, render: render))
    }

    static func processing(capture: CallPCMTap, render: CallPCMTap) -> LKRTCDefaultAudioProcessingModule {
        let processing = LKRTCDefaultAudioProcessingModule()
        // M150's adapter initializer ignores its delegate argument. The setters
        // bind the native callbacks; constructor arguments alone produce empty CAFs.
        processing.capturePostProcessingDelegate = capture
        processing.renderPreProcessingDelegate = render
        return processing
    }

    static func configureAudio() throws {
        let rtc = LKRTCAudioSession.sharedInstance()
        rtc.useManualAudio = true
        rtc.isAudioEnabled = isActive
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetoothHFP])
    }

    static func activate(_ session: AVAudioSession) {
        isActive = true
        let rtc = LKRTCAudioSession.sharedInstance()
        rtc.audioSessionDidActivate(session)
        rtc.isAudioEnabled = true
    }

    static func deactivate(_ session: AVAudioSession) {
        isActive = false
        let rtc = LKRTCAudioSession.sharedInstance()
        rtc.isAudioEnabled = false
        rtc.audioSessionDidDeactivate(session)
    }
}

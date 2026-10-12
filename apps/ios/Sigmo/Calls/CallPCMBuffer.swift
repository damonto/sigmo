import Foundation
import Synchronization

/// An owned audio frame; WebRTC's borrowed buffers must not escape its callback.
struct CallPCM: Sendable {
    let samples: [Float]
    let sampleRate: Double
    // Callback time anchors a new segment; it is not a per-buffer media clock.
    let time: TimeInterval
    let discontinuity: Bool

    init(samples: [Float], sampleRate: Double, time: TimeInterval, discontinuity: Bool = false) {
        self.samples = samples
        self.sampleRate = sampleRate
        self.time = time
        self.discontinuity = discontinuity
    }
}

/// One serialized WebRTC producer and one serial-queue consumer per tap.
/// Release/acquire publication keeps each slot exclusively owned while in use.
/// The producer only copies into preallocated storage; allocation happens in pop().
final class CallPCMBuffer: @unchecked Sendable {
    struct Header: Sendable {
        let recording: UInt
        let sampleRate: Double
        let time: TimeInterval
        let discontinuity: Bool
    }

    private final class Slot {
        let samples: UnsafeMutableBufferPointer<Float>
        var count = 0
        var header = Header(recording: 0, sampleRate: 0, time: 0, discontinuity: false)

        init(frameCapacity: Int) {
            samples = .allocate(capacity: frameCapacity)
            samples.initialize(repeating: 0)
        }

        deinit {
            samples.deinitialize()
            samples.deallocate()
        }
    }

    let capacity: Int
    let frameCapacity: Int
    private let slots: [Slot]
    private let readIndex = Atomic<Int>(0)
    private let writeIndex = Atomic<Int>(0)

    // WebRTC's APM exposes at most 10ms at 48kHz. Reject unexpected formats
    // instead of allocating or truncating a frame on the audio thread.
    init(capacity: Int = 200, frameCapacity: Int = 480) {
        precondition(capacity > 0 && frameCapacity > 0)
        self.capacity = capacity
        self.frameCapacity = frameCapacity
        slots = (0..<capacity).map { _ in Slot(frameCapacity: frameCapacity) }
    }

    func append(count: Int, header: Header, fill: (UnsafeMutableBufferPointer<Float>) -> Void) -> Bool {
        guard count > 0, count <= frameCapacity else { return false }
        let write = writeIndex.load(ordering: .relaxed)
        guard write - readIndex.load(ordering: .acquiring) < capacity else { return false }
        let slot = slots[write % capacity]
        fill(UnsafeMutableBufferPointer(rebasing: slot.samples[..<count]))
        slot.count = count
        slot.header = header
        writeIndex.store(write + 1, ordering: .releasing)
        return true
    }

    func pop() -> (recording: UInt, pcm: CallPCM)? {
        let read = readIndex.load(ordering: .relaxed)
        guard read < writeIndex.load(ordering: .acquiring) else { return nil }
        let slot = slots[read % capacity]
        let header = slot.header
        let pcm = CallPCM(samples: Array(slot.samples[..<slot.count]), sampleRate: header.sampleRate,
            time: header.time, discontinuity: header.discontinuity)
        readIndex.store(read + 1, ordering: .releasing)
        return (header.recording, pcm)
    }
}

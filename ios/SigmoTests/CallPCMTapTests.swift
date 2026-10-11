import Synchronization
import XCTest
@testable import Sigmo

@MainActor
final class CallPCMTapTests: XCTestCase {
    func testBufferFullAndWraparoundPreserveOwnedSamples() throws {
        let buffer = CallPCMBuffer(capacity: 2, frameCapacity: 4)
        let header = CallPCMBuffer.Header(recording: 1, sampleRate: 48_000, time: 1000, discontinuity: false)
        XCTAssertTrue(buffer.append(count: 4, header: header) { $0.update(repeating: 0.1) })
        XCTAssertTrue(buffer.append(count: 4, header: header) { $0.update(repeating: 0.2) })
        XCTAssertFalse(buffer.append(count: 4, header: header) { _ in XCTFail("A full slot must not be overwritten") })
        let first = try XCTUnwrap(buffer.pop())
        XCTAssertTrue(buffer.append(count: 4, header: header) { $0.update(repeating: 0.3) })
        XCTAssertEqual(first.pcm.samples, Array(repeating: 0.1, count: 4))
        XCTAssertEqual(try XCTUnwrap(buffer.pop()).pcm.samples, Array(repeating: 0.2, count: 4))
        XCTAssertEqual(try XCTUnwrap(buffer.pop()).pcm.samples, Array(repeating: 0.3, count: 4))
        XCTAssertNil(buffer.pop())
        for count in [0, 5] {
            XCTAssertFalse(buffer.append(count: count, header: header) { _ in XCTFail("Invalid frame size") })
        }
    }

    func testTapCopiesBorrowedAudioBeforeReturningAndDrainsOnStop() {
        let queue = DispatchQueue(label: "CallPCMTapTests.copy")
        let tap = CallPCMTap(buffer: CallPCMBuffer(capacity: 4, frameCapacity: 4), queue: queue)
        let received = PCMEvents()
        tap.configure(silenced: false, sink: { received.append($0) }, onFailure: { XCTFail("Capture audio") })
        tap.audioProcessingInitialize(sampleRate: 16_000, channels: 2)
        queue.suspend()
        var input: [Float] = [16_384, -16_384, 8192, -8192]
        input.withUnsafeMutableBufferPointer { samples in
            tap.process(frameCount: 4, channelCount: 2) { _ in samples.baseAddress! }
        }
        input = Array(repeating: 8192, count: 4)
        tap.setSilenced(true)
        input.withUnsafeMutableBufferPointer { samples in
            tap.process(frameCount: 4, channelCount: 1) { _ in samples.baseAddress! }
        }
        tap.setSilenced(false)
        tap.audioProcessingRelease()
        tap.audioProcessingInitialize(sampleRate: 8000, channels: 1)
        input.withUnsafeMutableBufferPointer { samples in
            tap.process(frameCount: 4, channelCount: 1) { _ in samples.baseAddress! }
            samples.update(repeating: 0)
        }
        XCTAssertTrue(received.frames.isEmpty, "No sink runs on the producer while its worker is suspended")
        queue.resume()
        tap.configure(silenced: false, sink: nil)
        XCTAssertEqual(received.frames.map(\.samples), [[0.5, -0.5, 0.25, -0.25], [0, 0, 0, 0], [0.25, 0.25, 0.25, 0.25]])
        XCTAssertEqual(received.frames.map(\.sampleRate), [16_000, 16_000, 8000])
        XCTAssertEqual(received.frames.map(\.discontinuity), [true, false, true])
    }

    func testOldCallbackCannotEnterANewRecording() throws {
        let tap = CallPCMTap(buffer: CallPCMBuffer(capacity: 4, frameCapacity: 4))
        let old = PCMEvents(), new = PCMEvents()
        tap.configure(silenced: false, sink: { old.append($0) })
        let entered = DispatchSemaphore(value: 0)
        let resume = DispatchSemaphore(value: 0)
        let finished = DispatchSemaphore(value: 0)
        DispatchQueue.global().async {
            var input = Array(repeating: Float(16_384), count: 4)
            input.withUnsafeMutableBufferPointer { samples in
                tap.process(frameCount: 4, channelCount: 1) { _ in
                    entered.signal()
                    XCTAssertEqual(resume.wait(timeout: .now() + 5), .success)
                    return samples.baseAddress!
                }
            }
            finished.signal()
        }
        defer { resume.signal() }
        XCTAssertEqual(entered.wait(timeout: .now() + 5), .success)
        tap.configure(silenced: false, sink: { new.append($0) })
        resume.signal()
        XCTAssertEqual(finished.wait(timeout: .now() + 5), .success)
        var input = Array(repeating: Float(8192), count: 4)
        input.withUnsafeMutableBufferPointer { samples in
            tap.process(frameCount: 4, channelCount: 1) { _ in samples.baseAddress! }
        }
        tap.configure(silenced: false, sink: nil)
        XCTAssertTrue(old.frames.isEmpty)
        XCTAssertEqual(new.frames.map(\.samples), [[0.25, 0.25, 0.25, 0.25]])
    }

    func testTapOverflowLeavesRecordingRecoverable() async throws {
        let directory = URL.temporaryDirectory.appending(path: "TapTests-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let queue = DispatchQueue(label: "CallPCMTapTests.overflow")
        let tap = CallPCMTap(buffer: CallPCMBuffer(capacity: 1), queue: queue)
        let failures = PCMEvents()
        let writer = try CallRecordingWriter(directory: directory, stem: "overflow") { failures.fail() }
        tap.configure(silenced: false, sink: { writer.append($0, remote: true) }, onFailure: { writer.fail() })
        queue.suspend()
        var input = Array(repeating: Float(8192), count: 480)
        input.withUnsafeMutableBufferPointer { samples in
            for _ in 0..<3 {
                tap.process(frameCount: 480, channelCount: 1) { _ in samples.baseAddress! }
            }
        }
        queue.resume()
        tap.configure(silenced: false, sink: nil)
        do {
            _ = try await writer.finish()
            XCTFail("An overflowed capture must not be committed")
        } catch {
            XCTAssertEqual(error as? CallRecordingError, .writeFailed)
        }
        XCTAssertEqual(failures.failureCount, 1)
        let duration = try await CallRecordingWriter.recover(directory: directory, stem: "overflow")
        XCTAssertGreaterThan(duration, 0)
    }

    func testBufferPublicationAcrossProducerAndConsumerThreads() {
        let buffer = CallPCMBuffer(capacity: 16, frameCapacity: 8)
        let received = PCMEvents()
        let group = DispatchGroup()
        let deadline = Date.now.addingTimeInterval(5)
        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            for frame in 0..<2000 {
                let header = CallPCMBuffer.Header(recording: 1, sampleRate: 48_000, time: Double(frame), discontinuity: false)
                while !buffer.append(count: 8, header: header, fill: { $0.update(repeating: Float(frame)) }) {
                    guard Date.now < deadline else { XCTFail("Producer stalled"); return }
                    Thread.sleep(forTimeInterval: 0.0001)
                }
            }
        }
        group.enter()
        DispatchQueue.global().async {
            defer { group.leave() }
            var count = 0
            while count < 2000 {
                guard Date.now < deadline else { XCTFail("Consumer stalled"); return }
                guard let frame = buffer.pop() else { Thread.sleep(forTimeInterval: 0.0001); continue }
                received.append(frame.pcm)
                count += 1
            }
        }
        XCTAssertEqual(group.wait(timeout: .now() + 6), .success)
        XCTAssertEqual(received.frames.count, 2000)
        for (index, frame) in received.frames.enumerated() {
            XCTAssertEqual(frame.time, Double(index))
            XCTAssertEqual(frame.samples, Array(repeating: Float(index), count: 8))
        }
    }
}

private final class PCMEvents: Sendable {
    private let state = Mutex((frames: [CallPCM](), failures: 0))
    var frames: [CallPCM] { state.withLock { $0.frames } }
    var failureCount: Int { state.withLock { $0.failures } }
    func append(_ pcm: CallPCM) { state.withLock { $0.frames.append(pcm) } }
    func fail() { state.withLock { $0.failures += 1 } }
}

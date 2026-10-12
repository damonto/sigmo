import XCTest
@testable import Sigmo

private actor ConcurrencyProbe {
    var count = 0
    var peak = 0
    var waiters: [CheckedContinuation<Void, Never>] = []
    var arrival: CheckedContinuation<Void, Never>?
    var released = false

    func operation() async {
        count += 1
        peak = max(peak, count)
        if count == 3 { arrival?.resume(); arrival = nil }
        if !released { await withCheckedContinuation { waiters.append($0) } }
        count -= 1
    }
    func waitUntilFull() async {
        if count == 3 { return }
        await withCheckedContinuation { arrival = $0 }
    }
    func release() {
        released = true
        for waiter in waiters { waiter.resume() }
        waiters = []
    }
    func maximum() -> Int { peak }
}

final class RefreshGateTests: XCTestCase {
    func testRefreshConcurrencyIsBoundedAndCancellationDoesNotLosePermits() async throws {
        let gate = RefreshGate()
        let probe = ConcurrencyProbe()
        let tasks = (0..<8).map { _ in Task { try await gate.run { await probe.operation() } } }
        await probe.waitUntilFull()
        let canceled = Task { try await gate.run { XCTFail("Canceled queued work should not run") } }
        canceled.cancel()
        await probe.release()
        for task in tasks { try await task.value }
        do { try await canceled.value; XCTFail("expected cancellation") } catch is CancellationError { }
        let maximum = await probe.maximum()
        XCTAssertEqual(maximum, 3)
        let value = try await gate.run { 42 }
        XCTAssertEqual(value, 42)
    }
}

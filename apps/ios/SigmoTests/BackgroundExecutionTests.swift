import UIKit
import XCTest
@testable import Sigmo

@MainActor
final class BackgroundExecutionTests: XCTestCase {
    func testEndingAnAssertionIsIdempotentAndIgnoresLateExpiration() throws {
        let assertions = BackgroundAssertions()
        let execution = try XCTUnwrap(BackgroundExecution.begin(name: "interaction", access: assertions.access) {
            XCTFail("An ended assertion must not interrupt another operation")
        })
        let expire = try XCTUnwrap(assertions.expire)
        execution.end()
        execution.end()
        expire()
        XCTAssertEqual(assertions.ended, [assertions.identifier])
    }

    func testCompletionAndFailureReleaseAssertion() async throws {
        for fails in [false, true] {
            let assertions = BackgroundAssertions()
            do {
                let value = try await BackgroundExecution.run(name: "request", access: assertions.access) {
                    if fails { throw URLError(.timedOut) }
                    return "response"
                }
                XCTAssertFalse(fails)
                XCTAssertEqual(value, "response")
            } catch {
                XCTAssertTrue(fails)
                XCTAssertEqual((error as? URLError)?.code, .timedOut)
            }
            XCTAssertEqual(assertions.begun, 1)
            XCTAssertEqual(assertions.ended, [assertions.identifier])
        }
    }

    func testExpirationEndsAssertionBeforeUncooperativeWorkReturns() async throws {
        let assertions = BackgroundAssertions()
        let gate = SettingsRequestGate()
        let task = Task {
            try await BackgroundExecution.run(name: "request", access: assertions.access) {
                await gate.wait()
                return "late response"
            }
        }
        await gate.waitForCaller()
        let expire = try XCTUnwrap(assertions.expire)
        expire()
        XCTAssertEqual(assertions.ended, [assertions.identifier])
        await gate.release()
        do {
            _ = try await task.value
            XCTFail("Expired work must not publish a late response")
        } catch is CancellationError {} catch { XCTFail("Unexpected error: \(error)") }
        XCTAssertEqual(assertions.ended, [assertions.identifier])
    }

    func testCallerCancellationReleasesAssertion() async {
        let assertions = BackgroundAssertions()
        let started = expectation(description: "request started")
        let cancelled = expectation(description: "request cancelled")
        let task = Task {
            try await BackgroundExecution.run(name: "request", access: assertions.access) {
                try await withTaskCancellationHandler {
                    started.fulfill()
                    try await Task.sleep(for: .seconds(60))
                } onCancel: { cancelled.fulfill() }
            }
        }
        await fulfillment(of: [started], timeout: 2)
        task.cancel()
        await fulfillment(of: [cancelled], timeout: 2)
        do { try await task.value; XCTFail("Expected cancellation") }
        catch is CancellationError {} catch { XCTFail("Unexpected error: \(error)") }
        XCTAssertEqual(assertions.ended, [assertions.identifier])
    }

    func testUnavailableAssertionDoesNotPreventForegroundRequest() async throws {
        let assertions = BackgroundAssertions()
        assertions.identifier = .invalid
        let value = try await BackgroundExecution.run(name: "request", access: assertions.access) { 42 }
        XCTAssertEqual(value, 42)
        XCTAssertTrue(assertions.ended.isEmpty)
    }

    func testImmediateExpirationDoesNotStartRequestOrLeakAssertion() async {
        let assertions = BackgroundAssertions()
        assertions.expiresImmediately = true
        do {
            try await BackgroundExecution.run(name: "request", access: assertions.access) {
                XCTFail("An expired request must not start")
            }
            XCTFail("Expected cancellation")
        } catch is CancellationError {} catch { XCTFail("Unexpected error: \(error)") }
        XCTAssertEqual(assertions.ended, [assertions.identifier])
    }
}

@MainActor
final class BackgroundAssertions {
    var identifier = UIBackgroundTaskIdentifier(rawValue: 1)
    var begun = 0
    var ended: [UIBackgroundTaskIdentifier] = []
    var expire: (@MainActor @Sendable () -> Void)?
    var expiresImmediately = false

    var access: BackgroundExecution.Access {
        .init(begin: { _, expire in
            self.begun += 1
            self.expire = expire
            if self.expiresImmediately { expire() }
            return self.identifier
        }, end: { identifier in
            self.ended.append(identifier)
            self.expire = nil
        })
    }
}

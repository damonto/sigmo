import SwiftUI
import XCTest
@testable import Sigmo

@MainActor
final class ForegroundTaskTests: XCTestCase {
    func testBackgroundIdentityChangeDoesNotLoseRefreshWhileOldTaskFinishes() async throws {
        let probe = ForegroundProbe()
        let window = host(probe)
        defer { window.isHidden = true; window.rootViewController = nil }
        try await wait { probe.started == 1 }
        await probe.gate.waitForCaller()
        probe.phase = .background
        try await wait { probe.renderedPhase == .background }
        probe.identity += 1
        try await wait { probe.renderedIdentity == 1 }
        probe.phase = .active
        try await wait { probe.renderedPhase == .active }
        await probe.gate.release()
        try await wait { probe.started == 2 && probe.finished == 2 }
        XCTAssertEqual(probe.cancelled, 1)
    }

    func testInactiveSceneDefersNewWorkWithoutCancellingPendingWork() async throws {
        let probe = ForegroundProbe()
        probe.phase = .inactive
        let window = host(probe)
        defer { window.isHidden = true; window.rootViewController = nil }
        try await wait { probe.renderedPhase == .inactive }
        XCTAssertEqual(probe.started, 0)
        probe.phase = .active
        try await wait { probe.started == 1 }
        await probe.gate.waitForCaller()
        probe.phase = .inactive
        try await wait { probe.renderedPhase == .inactive }
        await probe.gate.release()
        try await wait { probe.finished == 1 }
        XCTAssertEqual(probe.cancelled, 0)
    }

    func testBackgroundAndQuickReturnPreserveInFlightRefresh() async throws {
        let probe = ForegroundProbe()
        let window = host(probe)
        defer { window.isHidden = true; window.rootViewController = nil }
        try await wait { probe.started == 1 }
        await probe.gate.waitForCaller()
        probe.phase = .background
        try await wait { probe.renderedPhase == .background }
        probe.phase = .active
        try await wait { probe.renderedPhase == .active }
        XCTAssertEqual(probe.started, 1)
        await probe.gate.release()
        try await wait { probe.finished == 1 }
        XCTAssertEqual(probe.cancelled, 0)

        probe.phase = .background
        try await wait { probe.renderedPhase == .background }
        XCTAssertEqual(probe.started, 1)
        probe.phase = .active
        try await wait { probe.finished == 2 }
        XCTAssertEqual(probe.started, 2)
    }

    func testBackgroundEntryDefersRefreshUntilForeground() async throws {
        let probe = ForegroundProbe()
        probe.phase = .background
        let window = host(probe)
        defer { window.isHidden = true; window.rootViewController = nil }
        try await wait { probe.renderedPhase == .background }
        XCTAssertEqual(probe.started, 0)
        probe.phase = .active
        try await wait { probe.started == 1 }
        await probe.gate.waitForCaller()
        await probe.gate.release()
        try await wait { probe.finished == 1 }
        XCTAssertEqual(probe.cancelled, 0)
    }

    func testIdentityChangesAndViewRemovalStillCancel() async throws {
        for removeView in [false, true] {
            let probe = ForegroundProbe()
            let window = host(probe)
            defer { window.isHidden = true; window.rootViewController = nil }
            try await wait { probe.started == 1 }
            await probe.gate.waitForCaller()
            if removeView { window.rootViewController = nil }
            else { probe.identity += 1; try await wait { probe.started == 2 } }
            await probe.gate.release()
            try await wait { probe.cancelled == 1 }
        }
    }

    private func host(_ probe: ForegroundProbe) -> UIWindow {
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 390, height: 844))
        window.rootViewController = UIHostingController(rootView: ForegroundProbeView(probe: probe))
        window.makeKeyAndVisible()
        return window
    }

    private func wait(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while !condition(), ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        guard condition() else {
            XCTFail("View state did not arrive before the deadline")
            throw URLError(.timedOut)
        }
    }
}

@MainActor @Observable
private final class ForegroundProbe {
    var phase = ScenePhase.active
    var renderedPhase: ScenePhase?
    var identity = 0
    var renderedIdentity: Int?
    var started = 0
    var finished = 0
    var cancelled = 0
    let gate = SettingsRequestGate()

    func refresh() async {
        started += 1
        if started == 1 { await gate.wait() }
        if Task.isCancelled { cancelled += 1 }
        finished += 1
    }
}

private struct ForegroundProbeView: View {
    let probe: ForegroundProbe

    var body: some View {
        Color.clear
            .foregroundTask(id: probe.identity) { await probe.refresh() }
            .onChange(of: probe.phase, initial: true) { _, phase in probe.renderedPhase = phase }
            .onChange(of: probe.identity, initial: true) { _, identity in probe.renderedIdentity = identity }
            .environment(\.scenePhase, probe.phase)
    }
}

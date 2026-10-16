import Synchronization
import XCTest
@testable import Sigmo

@MainActor
final class DiagnosticLogStoreTests: XCTestCase {
    private var directory: URL!
    private var suite: String!
    private var defaults: UserDefaults!
    private var stores: [DiagnosticLogStore] = []

    override func setUp() async throws {
        directory = .temporaryDirectory.appending(path: "DiagnosticTests-\(UUID())")
        suite = "com.damon.sigmo.diagnostics.tests.\(UUID())"
        defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
    }

    override func tearDown() async throws {
        for store in stores { _ = try? await store.snapshot() }
        stores.removeAll()
        defaults.removePersistentDomain(forName: suite)
        if FileManager.default.fileExists(atPath: directory.path) { try FileManager.default.removeItem(at: directory) }
    }

    private func makeStore(maximumFileBytes: Int = 1024 * 1024, maximumFiles: Int = 5,
                           console: @escaping @Sendable (DiagnosticLevel, String) -> Void = { _, _ in }) -> DiagnosticLogStore {
        let store = DiagnosticLogStore(directory: directory, defaults: defaults, maximumFileBytes: maximumFileBytes,
                                       maximumFiles: maximumFiles, console: console)
        stores.append(store)
        return store
    }

    func testDisabledByDefaultDoesNotCreateFilesOrWriteConsole() async throws {
        let console = Mutex<[String]>([])
        let store = makeStore { _, line in console.withLock { $0.append(line) } }
        XCTAssertFalse(store.isEnabled)
        XCTAssertNil(store.generation)
        store.record("ignored", category: "app")
        let snapshot = try await store.snapshot()
        XCTAssertTrue(snapshot.isEmpty)
        XCTAssertTrue(console.withLock { $0.isEmpty })
        XCTAssertFalse(FileManager.default.fileExists(atPath: directory.path))
    }

    func testPreferenceAndLogsSurviveReopeningAndDisablingKeepsExport() async throws {
        let first = makeStore()
        await first.setEnabled(true)
        first.record("before.restart", category: "app")
        let saved = try await first.snapshot()
        XCTAssertEqual(try diagnosticEvents(saved).map(\.event), ["before.restart"])
        XCTAssertEqual(try directory.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup, true)

        let reopened = makeStore()
        XCTAssertTrue(reopened.isEnabled)
        let restored = try await reopened.snapshot()
        XCTAssertEqual(restored, saved)
        await reopened.setEnabled(false)
        reopened.record("ignored", category: "app")
        let disabled = try await reopened.snapshot()
        XCTAssertEqual(disabled, saved)
        XCTAssertFalse(makeStore().isEnabled)
    }

    func testRotationRetainsNewestEventsInOrderWithinBothLimits() async throws {
        let store = makeStore(maximumFileBytes: 512)
        await store.setEnabled(true)
        for index in 0..<100 {
            store.record("rotation", category: "app", fields: ["index": String(index), "padding": String(repeating: "x", count: 150)])
        }
        let snapshot = try await store.snapshot()
        let events = try diagnosticEvents(snapshot)
        let indices = try events.map { try XCTUnwrap(Int($0.fields["index"] ?? "")) }
        XCTAssertEqual(indices.last, 99)
        XCTAssertEqual(indices, indices.sorted())
        XCTAssertLessThan(events.count, 100)
        let files = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.fileSizeKey])
        XCTAssertEqual(files.count, 5)
        for file in files { XCTAssertLessThanOrEqual(try Data(contentsOf: file).count, 512) }
        let summary = await store.summary()
        XCTAssertEqual(summary.eventCount, events.count)
        XCTAssertEqual(summary.byteCount, snapshot.count)
        XCTAssertLessThanOrEqual(summary.byteCount, 5 * 512)
    }

    func testConcurrentWritersProduceCompleteIndependentEvents() async throws {
        let store = makeStore()
        await store.setEnabled(true)
        await withTaskGroup(of: Void.self) { group in
            for index in 0..<200 {
                group.addTask { store.record("concurrent", category: "app", fields: ["index": String(index)]) }
            }
        }
        let events = try diagnosticEvents(await store.snapshot())
        XCTAssertEqual(events.count, 200)
        XCTAssertEqual(Set(events.compactMap { $0.fields["index"] }).count, 200)
    }

    func testSlowConsoleDoesNotBlockCallersAndDisablingDrainsTheActiveWrite() async throws {
        let started = expectation(description: "Console write started")
        let release = DispatchSemaphore(value: 0)
        let finished = Mutex(false)
        let store = makeStore { _, _ in
            started.fulfill()
            _ = release.wait(timeout: .now() + 10)
        }
        defer { release.signal() }
        await store.setEnabled(true)
        store.record("inflight", category: "app")
        await fulfillment(of: [started], timeout: 2)

        let before = ContinuousClock.now
        XCTAssertTrue(store.isEnabled)
        store.record("queued", category: "app")
        XCTAssertLessThan(before.duration(to: .now), .seconds(2))
        let disabling = Task {
            await store.setEnabled(false)
            finished.withLock { $0 = true }
        }
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        while store.isEnabled, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertFalse(store.isEnabled)
        XCTAssertFalse(finished.withLock { $0 })
        release.signal()
        await disabling.value
        store.record("disabled", category: "app")
        let events = try diagnosticEvents(await store.snapshot())
        XCTAssertEqual(events.map(\.event), ["inflight"])
        XCTAssertFalse(makeStore().isEnabled)
    }

    func testSummaryTracksAppendsRotationAndClearing() async throws {
        let store = makeStore(maximumFileBytes: 512, maximumFiles: 2)
        await store.setEnabled(true)
        let empty = await store.summary()
        XCTAssertEqual(empty.eventCount, 0)
        for event in ["first", "second"] {
            store.record(event, category: "app")
            let summary = await store.summary()
            let snapshot = try await store.snapshot()
            XCTAssertEqual(summary.eventCount, try diagnosticEvents(snapshot).count)
            XCTAssertEqual(summary.byteCount, snapshot.count)
        }
        for index in 0..<20 {
            store.record("rotation", category: "app", fields: ["index": String(index), "padding": String(repeating: "x", count: 150)])
        }
        let rotated = await store.summary()
        let snapshot = try await store.snapshot()
        XCTAssertEqual(rotated.eventCount, try diagnosticEvents(snapshot).count)
        XCTAssertEqual(rotated.byteCount, snapshot.count)
        XCTAssertLessThanOrEqual(rotated.byteCount, 1024)
        try await store.clear()
        let cleared = await store.summary()
        XCTAssertEqual(cleared.eventCount, 0)
        XCTAssertEqual(cleared.byteCount, 0)
        XCTAssertTrue(store.isEnabled)
    }

    func testClearingDropsAnErrorStillBeingClassified() async throws {
        let store = makeStore()
        await store.setEnabled(true)
        let started = expectation(description: "Error classification started")
        let release = DispatchSemaphore(value: 0)
        let paused = Mutex(false)
        let error = PausingDiagnosticError {
            let first = paused.withLock { value in
                if value { return false }
                value = true
                return true
            }
            if first {
                started.fulfill()
                _ = release.wait(timeout: .now() + 10)
            }
        }
        try await withThrowingTaskGroup(of: Void.self) { group in
            defer { release.signal() }
            group.addTask { store.record(error, category: "app", operation: "old.failed") }
            await fulfillment(of: [started], timeout: 2)
            try await store.clear()
            release.signal()
            try await group.waitForAll()
        }
        store.record("current", category: "app")
        let events = try diagnosticEvents(await store.snapshot())
        XCTAssertEqual(events.map(\.event), ["current"])
    }

    func testChangesObserveWritesAndClearingAndCanBeCancelled() async throws {
        let store = makeStore()
        await store.setEnabled(true)
        let initial = expectation(description: "Initial summary")
        let written = expectation(description: "Saved event")
        let cleared = expectation(description: "Cleared events")
        let observer = Task {
            var phase = 0
            for await _ in store.changes() {
                let summary = await store.summary()
                if phase == 0, summary.eventCount == 0 { phase = 1; initial.fulfill() }
                else if phase == 1, summary.eventCount == 1 { phase = 2; written.fulfill() }
                else if phase == 2, summary.eventCount == 0 { phase = 3; cleared.fulfill() }
            }
        }
        await fulfillment(of: [initial], timeout: 2)
        store.record("observed", category: "app")
        await fulfillment(of: [written], timeout: 2)
        try await store.clear()
        await fulfillment(of: [cleared], timeout: 2)
        observer.cancel()
        await observer.value
    }

    func testOldRequestsCannotWriteAfterDisableOrClearAndSnapshotsStayImmutable() async throws {
        let console = Mutex<[String]>([])
        let store = makeStore { _, line in console.withLock { $0.append(line) } }
        await store.setEnabled(true)
        let old = try XCTUnwrap(DiagnosticRequest(store: store, service: .backend, method: "GET", path: ["app"]))
        let saved = try await store.snapshot()
        await store.setEnabled(false)
        old.completed(BackendHTTPResponse(data: Data(), status: 200))
        let disabled = try await store.snapshot()
        XCTAssertEqual(disabled, saved)

        await store.setEnabled(true)
        old.completed(BackendHTTPResponse(data: Data(), status: 200))
        let enabledAgain = try await store.snapshot()
        XCTAssertEqual(enabledAgain, saved)
        let pending = try XCTUnwrap(DiagnosticRequest(store: store, service: .backend, method: "POST", path: ["app"]))
        for _ in 0..<100 { store.record("queued", category: "app") }
        try await store.clear()
        let countAfterClear = console.withLock { $0.count }
        pending.failed(URLError(.timedOut))
        let cleared = try await store.snapshot()
        XCTAssertTrue(cleared.isEmpty)
        XCTAssertTrue(store.isEnabled)
        XCTAssertEqual(console.withLock { $0.count }, countAfterClear)
        XCTAssertFalse(saved.isEmpty)

        store.record("new", category: "app")
        let new = try diagnosticEvents(await store.snapshot())
        XCTAssertEqual(new.map(\.event), ["new"])
    }

    func testWriteFailureIsReportedAndNextSuccessfulWriteRecovers() async throws {
        try FileManager.default.createDirectory(at: directory.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("blocks directory".utf8).write(to: directory)
        let store = makeStore()
        await store.setEnabled(true)
        store.record("cannot.write", category: "app")
        let failed = await store.summary()
        XCTAssertTrue(failed.storageFailed)
        try FileManager.default.removeItem(at: directory)
        store.record("recovered", category: "app")
        let recovered = await store.summary()
        XCTAssertFalse(recovered.storageFailed)
        XCTAssertEqual(recovered.eventCount, 1)
    }

    func testInterruptedFinalLineIsIgnoredAndRepairedBeforeAppending() async throws {
        let store = makeStore()
        await store.setEnabled(true)
        store.record("complete", category: "app")
        let complete = try await store.snapshot()
        try (complete + Data("{\"event\":\"interrupted".utf8)).write(to: directory.appending(path: "diagnostics-0.log"))
        let recovered = try await store.snapshot()
        XCTAssertEqual(recovered, complete)
        store.record("next", category: "app")
        let events = try diagnosticEvents(await store.snapshot())
        XCTAssertEqual(events.map(\.event), ["complete", "next"])
    }

    func testOversizedUnicodeAndNewlinesStayBoundedAndParseAsOneEvent() async throws {
        let store = makeStore(maximumFileBytes: 512)
        await store.setEnabled(true)
        store.record(String(repeating: "🪵", count: 1000), category: "app",
                     fields: ["large": String(repeating: "秘密\n", count: 1000)])
        let snapshot = try await store.snapshot()
        XCTAssertLessThanOrEqual(snapshot.count, 512)
        XCTAssertEqual(try diagnosticEvents(snapshot).count, 1)
        let summary = await store.summary()
        XCTAssertFalse(summary.storageFailed)
    }

    func testExportIncludesMetadataAndUsesAnImmutableSnapshotWhileDisabled() async throws {
        let store = makeStore()
        await store.setEnabled(true)
        store.record("exported.event", category: "app")
        _ = try await store.snapshot()
        await store.setEnabled(false)
        let document = try await DiagnosticLogDocument.snapshot(from: store)
        try await store.clear()
        let text = String(decoding: document.data, as: UTF8.self)
        XCTAssertTrue(text.contains("Bundle: \(AppDiagnostics.bundleIdentifier)"))
        XCTAssertTrue(text.contains("Version: \(AppDiagnostics.version) (\(AppDiagnostics.build))"))
        XCTAssertTrue(text.contains("iOS:"))
        XCTAssertTrue(text.contains("Exported:"))
        XCTAssertTrue(text.contains("exported.event"))
        XCTAssertFalse(store.isEnabled)
        let cleared = try await store.snapshot()
        XCTAssertTrue(cleared.isEmpty)
    }
}

private struct PausingDiagnosticError: CustomNSError {
    let onClassification: @Sendable () -> Void
    static var errorDomain: String { "com.damon.sigmo.diagnostic-fixture" }
    var errorCode: Int { onClassification(); return 1 }
    var errorUserInfo: [String: Any] { [:] }
}

func diagnosticEvents(_ data: Data) throws -> [DiagnosticEvent] {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .iso8601
    return try data.split(separator: 10).map { try decoder.decode(DiagnosticEvent.self, from: Data($0)) }
}

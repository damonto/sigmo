import Foundation
import Synchronization

enum DiagnosticLevel: String, Codable, Sendable { case info, error }

struct DiagnosticEvent: Codable, Sendable {
    var timestamp = Date()
    var level = DiagnosticLevel.info
    var category: String
    var event: String
    var fields: [String: String] = [:]
}

struct DiagnosticLogSummary: Sendable {
    var eventCount = 0
    var byteCount = 0
    var storageFailed = false
}

/// The queue owns file I/O. The gate only orders enqueueing and invalidates pending
/// events; callers never wait for file I/O while holding or acquiring it.
final class DiagnosticLogStore: Sendable {
    static let enabledKey = "diagnostics.loggingEnabled"
    private struct Gate {
        var enabled: Bool
        var generation: UInt64 = 0
        var storageFailed = false
        var summary: DiagnosticLogSummary?
        var observers: [UUID: AsyncStream<Void>.Continuation] = [:]
    }

    // UserDefaults is documented as thread-safe, but its Sendable annotation
    // differs between Foundation implementations. The queue serializes our writes.
    private struct Preferences: @unchecked Sendable {
        let defaults: UserDefaults?
    }

    private let gate: Mutex<Gate>
    private let preferences: Preferences
    private let queue = DispatchQueue(label: "com.damon.sigmo.diagnostics", qos: .utility)
    private let directory: URL
    private let maximumFileBytes: Int
    private let maximumFiles: Int
    private let console: @Sendable (DiagnosticLevel, String) -> Void

    init(directory: URL, defaults: UserDefaults?, maximumFileBytes: Int = 1024 * 1024, maximumFiles: Int = 5,
         reset: Bool = false, console: @escaping @Sendable (DiagnosticLevel, String) -> Void = { _, _ in }) {
        self.directory = directory
        preferences = Preferences(defaults: defaults)
        self.maximumFileBytes = max(512, maximumFileBytes)
        self.maximumFiles = max(1, maximumFiles)
        self.console = console
        gate = Mutex(Gate(enabled: defaults?.bool(forKey: Self.enabledKey) ?? false))
        if reset {
            // Fixture cleanup precedes all subsequent file operations on this queue.
            queue.async {
                do { try self.removeFiles() }
                catch { self.gate.withLock { $0.storageFailed = true } }
            }
        }
    }

    var isEnabled: Bool { gate.withLock { $0.enabled } }
    var generation: UInt64? { gate.withLock { $0.enabled ? $0.generation : nil } }

    /// Persists the preference and drains any active write without blocking the caller.
    func setEnabled(_ enabled: Bool) async {
        await withCheckedContinuation { continuation in
            gate.withLock { state in
                let changed = state.enabled != enabled
                if changed {
                    state.enabled = enabled
                    state.generation &+= 1
                }
                queue.async {
                    if changed { self.preferences.defaults?.set(enabled, forKey: Self.enabledKey) }
                    continuation.resume()
                }
            }
        }
    }

    /// Callers supply fixed event names and explicitly selected, non-sensitive fields.
    func record(_ event: String, category: String, level: DiagnosticLevel = .info,
                fields: [String: String] = [:], generation: UInt64? = nil) {
        gate.withLock { state in
            guard state.enabled, generation == nil || generation == state.generation else { return }
            let current = state.generation
            let value = DiagnosticEvent(level: level, category: category, event: event, fields: fields)
            queue.async {
                guard self.accepts(current) else { return }
                do {
                    let data = try self.encode(value)
                    guard self.accepts(current) else { return }
                    let rotated = try self.append(data)
                    self.gate.withLock { state in
                        state.storageFailed = false
                        if rotated { state.summary = nil }
                        else if var summary = state.summary {
                            summary.eventCount += 1
                            summary.byteCount += data.count
                            state.summary = summary
                        }
                    }
                    if self.accepts(current) {
                        self.console(level, String(decoding: data.dropLast(), as: UTF8.self))
                    }
                    self.notifyObservers()
                } catch {
                    // Logging must not fail a request or recursively log its own I/O error.
                    self.storageFailed()
                }
            }
        }
    }

    func record(_ error: any Error, category: String, operation: String, backendID: UUID? = nil) {
        guard let generation else { return }
        var fields = DiagnosticError.fields(error)
        fields["backendID"] = backendID?.uuidString
        record(operation, category: category, level: DiagnosticError.isCancellation(error) ? .info : .error,
               fields: fields, generation: generation)
    }

    func summary() async -> DiagnosticLogSummary {
        await withCheckedContinuation { continuation in
            queue.async {
                do {
                    if self.gate.withLock({ $0.summary == nil }) { _ = try self.readSnapshot() }
                    let summary = self.gate.withLock { state in
                        var summary = state.summary ?? DiagnosticLogSummary()
                        summary.storageFailed = state.storageFailed
                        return summary
                    }
                    continuation.resume(returning: summary)
                } catch {
                    self.storageFailed()
                    continuation.resume(returning: DiagnosticLogSummary(storageFailed: true))
                }
            }
        }
    }

    /// Coalesces changes so a slow observer refreshes the latest counts once.
    func changes() -> AsyncStream<Void> {
        let id = UUID()
        return AsyncStream(bufferingPolicy: .bufferingNewest(1)) { continuation in
            continuation.onTermination = { [weak self] _ in
                self?.gate.withLock { $0.observers[id] = nil }
            }
            gate.withLock { $0.observers[id] = continuation }
            continuation.yield(())
        }
    }

    /// Returns an immutable snapshot, independent of later writes or clearing.
    func snapshot() async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            queue.async {
                do {
                    continuation.resume(returning: try self.readSnapshot())
                } catch {
                    self.storageFailed()
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    func clear() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            gate.withLock { state in
                state.generation &+= 1
                queue.async {
                    do {
                        try self.removeFiles()
                        self.gate.withLock { state in
                            state.storageFailed = false
                            state.summary = DiagnosticLogSummary()
                        }
                        self.notifyObservers()
                        continuation.resume()
                    } catch {
                        self.storageFailed()
                        continuation.resume(throwing: error)
                    }
                }
            }
        }
    }

    private func accepts(_ generation: UInt64) -> Bool {
        gate.withLock { $0.enabled && $0.generation == generation }
    }

    private func storageFailed() {
        let changed = gate.withLock { state in
            let changed = !state.storageFailed
            state.storageFailed = true
            state.summary = nil
            return changed
        }
        if changed { notifyObservers() }
    }

    private func notifyObservers() {
        let observers = gate.withLock { Array($0.observers.values) }
        for observer in observers { observer.yield(()) }
    }

    private func readSnapshot() throws -> Data {
        var data = Data()
        for index in (0..<maximumFiles).reversed() { data.append(try readFile(file(index))) }
        let summary = DiagnosticLogSummary(eventCount: data.reduce(0) { $0 + ($1 == 10 ? 1 : 0) }, byteCount: data.count)
        gate.withLock { $0.summary = summary }
        return data
    }

    private func file(_ index: Int) -> URL { directory.appending(path: "diagnostics-\(index).log") }

    private func encode(_ event: DiagnosticEvent) throws -> Data {
        var event = event
        event.event = String(decoding: event.event.utf8.prefix(96), as: UTF8.self)
        event.category = String(decoding: event.category.utf8.prefix(32), as: UTF8.self)
        event.fields = event.fields.sorted { $0.key < $1.key }.prefix(16).reduce(into: [:]) { fields, item in
            fields[String(decoding: item.key.utf8.prefix(32), as: UTF8.self)] = String(decoding: item.value.utf8.prefix(192), as: UTF8.self)
        }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        var data = try encoder.encode(event)
        while data.count + 1 > min(4096, maximumFileBytes), let key = event.fields.keys.sorted().last {
            event.fields.removeValue(forKey: key)
            data = try encoder.encode(event)
        }
        guard data.count + 1 <= maximumFileBytes else { throw CocoaError(.fileWriteUnknown) }
        data.append(10)
        return data
    }

    private func prepareDirectory() throws {
        if !FileManager.default.fileExists(atPath: directory.path) {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication])
        }
        // Retry exclusion for an existing directory after a previous setup failure.
        var url = directory
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
    }

    /// Returns whether rotation invalidated the cached counts.
    private func append(_ data: Data) throws -> Bool {
        try prepareDirectory()
        let url = file(0)
        if FileManager.default.fileExists(atPath: url.path) {
            let handle = try FileHandle(forUpdating: url)
            defer { try? handle.close() }
            var size = try handle.seekToEnd()
            if size > 0 {
                try handle.seek(toOffset: size - 1)
                if try handle.read(upToCount: 1) != Data([10]) {
                    // A terminated process may leave an incomplete final event.
                    size = UInt64(try readFile(url).count)
                    try handle.truncate(atOffset: size)
                }
            }
            if size + UInt64(data.count) <= UInt64(maximumFileBytes) {
                try handle.seek(toOffset: size)
                try handle.write(contentsOf: data)
                return false
            }
        }
        if maximumFiles > 1 {
            for index in stride(from: maximumFiles - 1, through: 1, by: -1) {
                let destination = file(index), source = file(index - 1)
                if FileManager.default.fileExists(atPath: destination.path) { try FileManager.default.removeItem(at: destination) }
                if FileManager.default.fileExists(atPath: source.path) { try FileManager.default.moveItem(at: source, to: destination) }
            }
        }
        try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        return true
    }

    private func readFile(_ url: URL) throws -> Data {
        let handle: FileHandle
        do { handle = try FileHandle(forReadingFrom: url) }
        catch let error as CocoaError where error.code == .fileReadNoSuchFile || error.code == .fileNoSuchFile { return Data() }
        defer { try? handle.close() }
        let data = try handle.read(upToCount: maximumFileBytes) ?? Data()
        guard let end = data.lastIndex(of: 10) else { return Data() }
        return data.prefix(through: end)
    }

    private func removeFiles() throws {
        for index in 0..<maximumFiles {
            let url = file(index)
            if FileManager.default.fileExists(atPath: url.path) { try FileManager.default.removeItem(at: url) }
        }
    }
}

import Foundation

// Shared by this app's sessions, so a manual refresh also respects the limit.
actor RefreshGate {
    private var active: Set<UUID> = []
    private var waiting: [(UUID, CheckedContinuation<UUID, Error>)] = []
    private let limit: Int

    init(limit: Int = 3) { self.limit = max(1, limit) }

    func run<Value: Sendable>(_ operation: @Sendable () async throws -> Value) async throws -> Value {
        let id = try await acquire()
        defer { release(id) }
        try Task.checkCancellation()
        return try await operation()
    }

    private func acquire() async throws -> UUID {
        let id = UUID()
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                if active.count < limit {
                    active.insert(id)
                    continuation.resume(returning: id)
                } else {
                    waiting.append((id, continuation))
                }
            }
        } onCancel: {
            Task { await self.cancelWaiter(id) }
        }
    }

    private func cancelWaiter(_ id: UUID) {
        guard let index = waiting.firstIndex(where: { $0.0 == id }) else { return }
        waiting.remove(at: index).1.resume(throwing: CancellationError())
    }

    private func release(_ id: UUID) {
        active.remove(id)
        guard !waiting.isEmpty else { return }
        let next = waiting.removeFirst()
        active.insert(next.0)
        next.1.resume(returning: next.0)
    }
}

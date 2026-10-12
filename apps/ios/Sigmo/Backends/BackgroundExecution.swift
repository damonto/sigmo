import UIKit

// A task assertion gives an in-flight operation time to finish after backgrounding.
// It is finite: expiration must cancel the work and release the assertion promptly.
@MainActor
final class BackgroundExecution {
    @MainActor struct Access {
        let begin: (String, @escaping @MainActor @Sendable () -> Void) -> UIBackgroundTaskIdentifier
        let end: (UIBackgroundTaskIdentifier) -> Void

        static let system = Access(
            begin: { UIApplication.shared.beginBackgroundTask(withName: $0, expirationHandler: $1) },
            end: { UIApplication.shared.endBackgroundTask($0) }
        )
    }

    private enum State {
        case starting
        case active(UIBackgroundTaskIdentifier)
        case ended
    }

    private let access: Access
    private var state = State.starting

    private init(access: Access) { self.access = access }

    // Streams can scope an assertion to a finite interaction without keeping
    // their idle observation alive. The owner must call end() when it finishes.
    static func begin(
        name: String, access: Access = .system,
        expirationHandler: @escaping @MainActor @Sendable () -> Void
    ) -> BackgroundExecution? {
        let execution = BackgroundExecution(access: access)
        let identifier = access.begin(name) {
            if case .ended = execution.state { return }
            expirationHandler()
            execution.end()
        }
        // The system can expire the assertion before begin returns its identifier.
        if case .ended = execution.state {
            if identifier != .invalid { access.end(identifier) }
            return nil
        }
        execution.state = .active(identifier)
        return execution
    }

    static func run<Value: Sendable>(
        name: String, access: Access = .system,
        operation: @escaping @Sendable () async throws -> Value
    ) async throws -> Value {
        try Task.checkCancellation()
        let task = Task {
            try Task.checkCancellation()
            let value = try await operation()
            try Task.checkCancellation()
            return value
        }
        let execution = begin(name: name, access: access) { task.cancel() }
        defer { execution?.end() }
        return try await withTaskCancellationHandler {
            let value = try await task.value
            try Task.checkCancellation()
            return value
        } onCancel: { task.cancel() }
    }

    func end() {
        let previous = state
        state = .ended
        guard case .active(let identifier) = previous, identifier != .invalid else { return }
        access.end(identifier)
    }
}

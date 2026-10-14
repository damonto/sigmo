import Foundation
import Observation

// Owns transitions between demo sessions, independently of sheets and call UI.
@MainActor @Observable
final class DemoPresentation: Identifiable {
    enum Action { case receiveMessage, receiveCall, reset }

    let id = UUID()
    private(set) var session: DemoSession
    private(set) var isBusy = false
    private(set) var isClosing = false
    var failure: BackendFailure?
    @ObservationIgnored private var operation: Task<Void, Never>?
    @ObservationIgnored private var closing: Task<Void, Never>?

    init() throws { session = try DemoSession() }

    func perform(_ action: Action) async {
        guard !isBusy, !isClosing else { return }
        isBusy = true
        failure = nil
        let current = session
        let task = Task {
            do {
                try Task.checkCancellation()
                switch action {
                case .receiveMessage:
                    try await current.receiveMessage()
                case .receiveCall:
                    try await current.receiveCall()
                case .reset:
                    // Construct first so a creation failure leaves the current
                    // session usable. A cancelled reset also disposes its replacement.
                    let next = try DemoSession()
                    await current.stop()
                    if Task.isCancelled || isClosing {
                        await next.stop()
                    } else {
                        session = next
                    }
                }
            } catch {
                if !Task.isCancelled && !isClosing { failure = .wrapping(error) }
            }
        }
        operation = task
        await task.value
        operation = nil
        isBusy = false
    }

    func close() async {
        if let closing { await closing.value; return }
        isClosing = true
        let pending = operation
        pending?.cancel()
        let task = Task {
            await pending?.value
            await session.stop()
        }
        closing = task
        await task.value
    }
}

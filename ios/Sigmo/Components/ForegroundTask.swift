import SwiftUI

extension View {
    // Refresh on entry and return to the foreground without cancelling a pending
    // request on either transition. View removal and identity changes still cancel.
    func foregroundTask<ID: Equatable>(id: ID, action: @escaping @MainActor () async -> Void) -> some View {
        modifier(ForegroundTask(id: id, action: action))
    }

    func foregroundTask(action: @escaping @MainActor () async -> Void) -> some View {
        foregroundTask(id: false, action: action)
    }
}

private struct ForegroundTask<ID: Equatable>: ViewModifier {
    let id: ID
    let action: @MainActor () async -> Void
    @Environment(\.scenePhase) private var scenePhase
    @State private var revision = 0
    @State private var running: UUID?

    private struct Identity: Equatable {
        let id: ID
        let revision: Int
    }

    func body(content: Content) -> some View {
        content
            .task(id: Identity(id: id, revision: revision)) {
                guard !Task.isCancelled else { return }
                let request = UUID()
                running = request
                defer { if running == request { running = nil } }
                // Even a deferred task replaces the previous identity's ownership.
                // Otherwise a slow cancellation can suppress the next foreground refresh.
                guard scenePhase == .active else { return }
                await action()
            }
            .onChange(of: scenePhase) { previous, next in
                if previous != .active, next == .active, running == nil {
                    revision += 1
                }
            }
    }
}

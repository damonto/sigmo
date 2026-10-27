import Foundation
import Observation

@MainActor @Observable
final class HistoryAssignmentState {
    enum LoadState: Equatable {
        case idle, loading, loaded
        case failed(BackendFailure)
    }

    let session: BackendSession
    let sourceID: String
    var selectedID = ""
    var failure: BackendFailure?
    private(set) var lines: [CommunicationLine] = []
    private(set) var loadState: LoadState = .idle
    private(set) var busy = false
    @ObservationIgnored private var loadRevision = 0

    init(session: BackendSession, sourceID: String) {
        self.session = session
        self.sourceID = sourceID
    }

    var source: CommunicationLine? {
        lines.first { $0.id == sourceID && $0.deletionState == "deleted" && !$0.modemId.isEmpty }
    }
    var candidates: [CommunicationLine] {
        guard let source else { return [] }
        return lines.filter {
            $0.modemId == source.modemId && $0.present && $0.deletionState.isEmpty && $0.id != sourceID
        }.sorted {
            if $0.active != $1.active { return $0.active }
            return $0.displayLabel.localizedStandardCompare($1.displayLabel) == .orderedAscending
        }
    }
    var selected: CommunicationLine? { candidates.first { $0.id == selectedID } }
    var canSubmit: Bool { loadState == .loaded && !busy && session.canAccessDevices && selected != nil }
    var modemTitle: String? {
        guard let source else { return nil }
        return session.modems.first { $0.id == source.modemId }?.displayName ?? source.modemId
    }

    func load() async {
        guard !busy, !Task.isCancelled else { return }
        loadRevision += 1
        let current = loadRevision
        loadState = .loading
        do {
            let lines = try await session.protectedRequest { api, token in
                try await api.communicationLines(token: token)
            }
            guard current == loadRevision else { return }
            try Task.checkCancellation()
            self.lines = lines
            let available = candidates
            if !available.contains(where: { $0.id == selectedID }) {
                selectedID = available.count == 1 ? available[0].id : ""
            }
            loadState = .loaded
        } catch {
            guard current == loadRevision else { return }
            loadState = Task.isCancelled || error is CancellationError ? .idle : .failed(.wrapping(error))
        }
    }

    func assign() async -> Bool {
        guard canSubmit, let target = selected?.id, !Task.isCancelled else { return false }
        let source = sourceID
        busy = true
        failure = nil
        defer { busy = false }
        do {
            try await session.protectedRequest { api, token in
                try await api.assignHistory(source: source, target: target, token: token)
            }
            session.invalidateCommunicationLines()
            await session.refreshUnreadCounts()
            return !Task.isCancelled
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
            return false
        }
    }
}

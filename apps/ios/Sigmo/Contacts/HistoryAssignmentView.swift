import SwiftUI

struct HistoryAssignmentSection: View {
    let session: BackendSession
    var showsBackend = false
    @State private var lines: [CommunicationLine] = []
    @State private var selected: CommunicationLine?
    @State private var failure: BackendFailure?
    var body: some View {
        Section {
            ForEach(lines.filter { $0.deletionState == "deleted" }) { line in
                Button { selected = line } label: {
                    Label {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(line.title)
                            if showsBackend { Text(session.configuration.name).font(.caption).foregroundStyle(.secondary) }
                        }
                    } icon: { Image(systemName: "simcard") }
                    .badge(Text("communication.assign"))
                }
            }
        }
        .task { await load() }
        .sheet(item: $selected, onDismiss: { Task { await load() } }) { line in HistoryAssignmentView(session: session, sourceID: line.id) }
        .detailErrorAlert($failure)
    }
    private func load() async {
        do { lines = try await session.communicationLines() }
        catch { if !Task.isCancelled { failure = .wrapping(error) } }
    }
}

struct HistoryAssignmentView: View {
    let session: BackendSession
    let sourceID: String
    @Environment(\.dismiss) private var dismiss
    @State private var lines: [CommunicationLine] = []
    @State private var target = ""
    @State private var busy = false
    @State private var failure: BackendFailure?
    private var source: CommunicationLine? { lines.first { $0.id == sourceID } }
    private var candidates: [CommunicationLine] { lines.filter { $0.present && $0.deletionState.isEmpty && $0.id != sourceID } }
    var body: some View {
        NavigationStack {
            Form {
                Section { Text("communication.assignDescription"); if let source { Text(source.title); Text("\(source.messageCount) \(detailText("communication.messages")) · \(source.callCount) \(detailText("communication.calls"))") } }
                Section("communication.chooseLine") {
                    Picker("communication.line", selection: $target) {
                        Text("communication.chooseLine").tag("")
                        ForEach(candidates) { line in Text(line.title + (line.active ? "" : " · " + detailText("communication.inactive"))).tag(line.id) }
                    }
                    if candidates.isEmpty { Text("communication.noLines") }
                }
                Button("communication.assign") { Task { await assign() } }.disabled(busy || target.isEmpty)
            }
            .navigationTitle("communication.assign").navigationBarTitleDisplayMode(.inline)
            .accessibilityIdentifier("history-assignment-" + sourceID)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("communication.later") { dismiss() }.disabled(busy).accessibilityIdentifier("historyAssignmentLater") } }
            .interactiveDismissDisabled(busy)
            .detailErrorAlert($failure)
            .task {
                do { lines = try await session.protectedRequest { api, token in try await api.communicationLines(token: token) } }
                catch { if !Task.isCancelled { failure = .wrapping(error) } }
            }
        }
    }
    private func assign() async {
        guard !busy else { return }
        let source = sourceID, target = target
        busy = true
        defer { busy = false }
        do {
            try await session.protectedRequest { api, token in try await api.assignHistory(source: source, target: target, token: token) }
            session.invalidateCommunicationLines()
            await session.refreshUnreadCounts()
            dismiss()
        } catch { failure = .wrapping(error) }
    }
}

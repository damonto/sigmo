import SwiftUI

struct CallDetailView: View {
    let record: CallRecord
    let history: CallHistoryState
    @Environment(CallsState.self) private var calls
    @State private var messageDraft: MessagesState?
    @State private var replacementAction: String?

    private var line: CallLine? { history.line }
    private var canMessage: Bool { history.canMessage(record) }

    var body: some View {
        CallHistoryDetail(record: record, session: history.session, modemID: history.modemID,
                          lineTitle: record.lineLabel ?? history.session.modems.first { $0.id == history.modemID }?.communicationLabel, region: line?.region, canMessage: canMessage,
                          message: { perform("message") }, callback: { perform("call") })
        .confirmationDialog("communication.useCurrentLine", isPresented: Binding(get: { replacementAction != nil }, set: { if !$0 { replacementAction = nil } })) {
            Button("communication.useCurrentLine") { if let action = replacementAction { run(action) }; replacementAction = nil }
            Button("common.cancel", role: .cancel) { replacementAction = nil }
        }
        .sheet(item: $messageDraft) { draft in
            NavigationStack { NewMessageView(state: draft) { _ in messageDraft = nil } }
        }
        .onChange(of: canMessage) { _, available in if !available { dismissMessage() } }
        .onChange(of: history.session.accessRevision) { _, _ in dismissMessage() }
    }

    private func perform(_ action: String) {
        if record.profileId != line?.profileID { replacementAction = action }
        else { run(action) }
    }
    private func run(_ action: String) {
        if action == "message" { messageDraft = history.messageDraft(for: record) }
        else if let line, calls.available(line) {
            Task { await calls.dial(number: record.number, line: line) }
        } else { calls.callbackNumber = record.number }
    }

    private func dismissMessage() {
        messageDraft?.clear()
        messageDraft = nil
    }
}

import SwiftUI

struct CallsView: View {
    @Bindable var state: CallHistoryState
    @Binding var selected: CallRecord?
    @Binding var dialing: Bool
    @Binding var recordingsPresented: Bool
    @Environment(CallsState.self) private var calls
    @Environment(\.scenePhase) private var scenePhase
    @State private var deleting: CallRecord?
    @State private var pendingDeletion: CallRecord?
    @State private var replacementCall: CallRecord?
    @State private var pendingDial: (number: String, line: CallLine)?

    private struct Refresh: Equatable { let access: Int; let line: String? }

    var body: some View {
        List {
            Section {
                Picker("calls.filter", selection: $state.missedOnly) {
                    Text("calls.all").tag(false)
                    Text("calls.missed").tag(true)
                }.pickerStyle(.segmented).listRowSeparator(.hidden)
                    .accessibilityIdentifier("calls-filter")
                if let line = state.line, !calls.available(line) {
                    Label("calls.upgrade", systemImage: "info.circle").font(.footnote).foregroundStyle(.secondary)
                }
            }
            if !state.loaded && state.loading {
                ProgressView("calls.loading").frame(maxWidth: .infinity)
            }
            ForEach(state.visibleItems) { record in
                CallHistoryRow(record: record, session: state.session, region: state.line?.region,
                               source: record.lineLabel ?? state.session.modems.first { $0.id == state.modemID }?.communicationLabel,
                               select: { selected = record }, callback: { callback(record) })
                .swipeActions(allowsFullSwipe: false) {
                    if record.terminal { Button("common.delete", systemImage: "trash", role: .destructive) { deleting = record } }
                }
            }
        }
        .listStyle(.plain)
        .overlay {
            if state.loaded && state.visibleItems.isEmpty && state.failure == nil {
                ContentUnavailableView(state.search.isEmpty ? "calls.empty" : "calls.noResults", systemImage: "phone", description: Text("calls.emptyDescription"))
                    .padding(.top, 65).allowsHitTesting(false)
            }
        }
        .refreshable { await state.load() }
        .detailErrorAlert($state.failure)
        .foregroundTask(id: Refresh(access: state.session.accessRevision, line: state.line?.id)) {
            await state.load()
        }
        .task(id: calls.revision) { if scenePhase == .active { await state.load() } }
        .task(id: state.search) {
            do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            if scenePhase == .active { await state.load() }
        }
        .task(id: pendingDeletion?.id) {
            guard let record = pendingDeletion, !Task.isCancelled else { return }
            _ = await state.delete(record)
            if !Task.isCancelled, pendingDeletion?.id == record.id { pendingDeletion = nil }
        }
        .onChange(of: state.session.accessRevision) { _, _ in clear() }
        .onChange(of: state.line?.id) { _, _ in clear() }
        .onDisappear { pendingDeletion = nil; deleting = nil }
        .confirmationDialog("communication.useCurrentLine", isPresented: Binding(get: { replacementCall != nil }, set: { if !$0 { replacementCall = nil } })) {
            Button("communication.useCurrentLine") { if let record = replacementCall { dialCurrent(record) }; replacementCall = nil }
            Button("common.cancel", role: .cancel) { replacementCall = nil }
        }
        .sheet(isPresented: $dialing, onDismiss: {
            // Finish the sheet transition before presenting the global call screen.
            guard let selection = pendingDial else { return }
            pendingDial = nil
            Task { await calls.dial(number: selection.number, line: selection.line) }
        }) {
            if let line = state.line {
                CallDialpad(line: line, session: state.session) { pendingDial = ($0, line) }
            }
        }
        .sheet(isPresented: $recordingsPresented) {
            NavigationStack { CallRecordingsView() }
        }
        .alert("calls.deleteTitle", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } })) {
            Button("common.delete", role: .destructive) {
                pendingDeletion = deleting
                deleting = nil
            }
            Button("common.cancel", role: .cancel) { deleting = nil }
        } message: { Text("calls.deleteExplanation") }
        .disabled(state.deleting || pendingDeletion != nil)
        .accessibilityIdentifier("calls-list")
    }

    private func clear() {
        state.clear()
        selected = nil
        pendingDial = nil
        dialing = false
        pendingDeletion = nil
        deleting = nil
    }

    private func callback(_ record: CallRecord) {
        if record.profileId != state.line?.profileID { replacementCall = record }
        else { dialCurrent(record) }
    }
    private func dialCurrent(_ record: CallRecord) {
        if let line = state.line, calls.available(line) {
            Task { await calls.dial(number: record.number, line: line) }
        } else { calls.callbackNumber = record.number }
    }
}

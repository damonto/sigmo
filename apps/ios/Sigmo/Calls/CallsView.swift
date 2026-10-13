import SwiftUI

struct CallsView: View {
    @Bindable var state: CallHistoryState
    @Binding var selected: CallRecord?
    @Binding var dialing: Bool
    @Binding var recordingsPresented: Bool
    @Environment(CallsState.self) private var calls
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.scenePhase) private var scenePhase
    @State private var deleting: CallRecord?
    @State private var pendingDeletion: CallRecord?
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
                HStack(spacing: 16) {
                    Button { selected = record } label: {
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Image(systemName: record.direction == "incoming" ? "phone.arrow.down.left" : "phone.arrow.up.right")
                                .foregroundStyle(.secondary).accessibilityHidden(true)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(calls.contacts.name(for: record.number, region: state.line?.region ?? "")
                                    ?? PhoneNumberDisplay.format(record.number, regionCode: state.line?.region ?? ""))
                                    .font(.headline).foregroundStyle(record.missed ? .red : .primary)
                                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
                                    .minimumScaleFactor(0.85)
                                metadata(for: record)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }.contentShape(Rectangle())
                    }.buttonStyle(.plain).accessibilityIdentifier("call-record-\(record.id)")
                    Button { callback(record) } label: {
                        Image(systemName: "phone.fill")
                            .frame(minWidth: 44, minHeight: 44)
                            .background(Color(uiColor: .secondarySystemBackground), in: Circle())
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("calls.callback"))
                    .accessibilityValue(PhoneNumberDisplay.format(record.number, regionCode: state.line?.region))
                    .disabled(!record.terminal || calls.hasCall)
                }
                .padding(.vertical, 4)
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

    private func metadata(for record: CallRecord) -> some View {
        let route = Text(record.terminal ? record.routeLabel : record.stateLabel).font(.subheadline)
        let date = Text(record.date, format: .dateTime.month(.abbreviated).day().hour().minute()).font(.caption)

        // Keep the timestamp from competing with the number for horizontal space.
        return ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                route
                Spacer(minLength: 0)
                date
            }
            VStack(alignment: .leading, spacing: 4) {
                route
                date
            }
        }
        .foregroundStyle(.secondary)
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
        if let line = state.line, record.profileId == line.profileID, calls.available(line) {
            Task { await calls.dial(number: record.number, line: line) }
        } else { calls.callbackNumber = record.number }
    }
}

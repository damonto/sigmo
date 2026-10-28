import SwiftUI

struct HistoryAssignmentSection: View {
    let session: BackendSession
    let lines: [CommunicationLine]
    var showsBackend = false
    let reload: () async -> Void
    @State private var selected: CommunicationLine?
    @State private var reloadID = 0
    var body: some View {
        Section {
            ForEach(lines.filter(\.hasAssignableHistory)) { line in
                Button { selected = line } label: {
                    Label {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(line.title)
                            if showsBackend { Text(session.configuration.name).font(.caption).foregroundStyle(.secondary) }
                        }
                    } icon: { Image(systemName: "simcard") }
                    .badge(Text("communication.assign"))
                }
                .accessibilityIdentifier("historyAssignmentSource-" + line.id)
            }
        }
        .sheet(item: $selected, onDismiss: { reloadID += 1 }) { line in
            HistoryAssignmentView(session: session, sourceID: line.id)
        }
        .task(id: reloadID) {
            guard reloadID > 0 else { return }
            await reload()
        }
    }
}

struct HistoryAssignmentView: View {
    @State private var state: HistoryAssignmentState
    @State private var reloadID = 0
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(session: BackendSession, sourceID: String) {
        _state = State(initialValue: HistoryAssignmentState(session: session, sourceID: sourceID))
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let modem = state.modemTitle {
                    Label(modem, systemImage: "antenna.radiowaves.left.and.right")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal)
                        .padding(.top, 8)
                        .accessibilityIdentifier("historyAssignmentModem")
                }
                List { content }
                    .listStyle(.insetGrouped)
                    .environment(\.defaultMinListRowHeight, 44)
                    .accessibilityIdentifier("history-assignment-" + state.sourceID)
                    .disabled(state.busy)
            }
            .background(Color(uiColor: .systemGroupedBackground))
            .safeAreaInset(edge: .bottom, spacing: 0) { confirmation }
            .navigationTitle("communication.assign")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("communication.later") { dismiss() }
                        .disabled(state.busy)
                        .accessibilityIdentifier("historyAssignmentLater")
                }
            }
            .interactiveDismissDisabled(state.busy)
            .detailErrorAlert($state.failure)
            .task(id: reloadID) { await state.load() }
            .onChange(of: state.session.accessRevision) { _, _ in dismiss() }
        }
    }

    @ViewBuilder private var content: some View {
        switch state.loadState {
        case .idle, .loading:
            Section {
                HStack(spacing: 12) {
                    ProgressView()
                    Text("communication.loading").foregroundStyle(.secondary)
                }
                .accessibilityIdentifier("historyAssignmentLoading")
            }
        case .failed(let failure):
            Section {
                Text(failure.message).foregroundStyle(.secondary)
                retryButton
            }
        case .loaded:
            if let source = state.source {
                sourceSection(source)
                candidatesSection
            } else {
                Section {
                    Text("communication.assignmentSourceUnavailable").foregroundStyle(.secondary)
                    retryButton
                }
            }
        }
    }

    private func sourceSection(_ source: CommunicationLine) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 12) {
                Label {
                    lineIdentity(source)
                } icon: {
                    Image(systemName: "simcard").foregroundStyle(.secondary)
                }
                Divider()
                Text(String.localizedStringWithFormat(detailText("communication.assignmentCounts"), source.messageCount, source.callCount))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 4)
        } header: {
            Text("communication.assignmentSource")
        }
    }

    private var candidatesSection: some View {
        let candidates = state.candidates
        return Section {
            if candidates.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text("communication.assignmentNoLines")
                    Text("communication.assignmentLaterHint")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 8)
                .accessibilityIdentifier("historyAssignmentEmpty")
            } else {
                ForEach(candidates) { line in
                    if candidates.count == 1 {
                        candidateRow(line)
                            .accessibilityElement(children: .combine)
                            .accessibilityAddTraits(.isSelected)
                            .accessibilityIdentifier("historyAssignmentLine-" + line.id)
                    } else {
                        Button { state.selectedID = line.id } label: { candidateRow(line) }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(state.selectedID == line.id ? .isSelected : [])
                            .accessibilityIdentifier("historyAssignmentLine-" + line.id)
                    }
                }
            }
        } header: {
            Text("communication.assignmentChooseLine")
        } footer: {
            Text("communication.assignmentSameModem")
        }
    }

    private func candidateRow(_ line: CommunicationLine) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                lineIdentity(line)
                if dynamicTypeSize.isAccessibilitySize { lineStatus(line) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !dynamicTypeSize.isAccessibilitySize { lineStatus(line) }
            Image(systemName: state.selectedID == line.id ? "checkmark.circle.fill" : "circle")
                .font(.title3)
                .foregroundStyle(state.selectedID == line.id ? Color.accentColor : Color(uiColor: .tertiaryLabel))
                .accessibilityHidden(true)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private func lineStatus(_ line: CommunicationLine) -> some View {
        Text(line.active ? LocalizedStringKey("communication.assignmentActive") : LocalizedStringKey("communication.assignmentInactive"))
            .font(.caption)
            .foregroundStyle(.secondary)
    }

    private func lineIdentity(_ line: CommunicationLine) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(lineTitle(line)).foregroundStyle(.primary)
            if !line.label.isEmpty, !line.number.isEmpty {
                PhoneNumberText(number: line.number, regionCode: line.region)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .multilineTextAlignment(.leading)
        .fixedSize(horizontal: false, vertical: true)
    }

    private func lineTitle(_ line: CommunicationLine) -> String {
        if !line.label.isEmpty { return line.label }
        if !line.number.isEmpty { return PhoneNumberDisplay.format(line.number, regionCode: line.region) }
        return String(format: detailText("communication.assignmentUnnamedLine"), String(line.id.suffix(6)))
    }

    private var retryButton: some View {
        // The page owns reloads so replacing the retry row with a spinner cannot cancel them.
        Button("communication.assignmentRetry") { reloadID += 1 }
            .accessibilityIdentifier("historyAssignmentRetry")
    }

    private var confirmation: some View {
        VStack(spacing: 12) {
            Text("communication.assignmentPreserved")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("historyAssignmentPreserved")
            AsyncActionButton(id: "historyAssignmentConfirm", action: {
                if await state.assign() { dismiss() }
            }) {
                Text(state.selected.map { String(format: detailText("communication.assignmentConfirm"), lineTitle($0)) }
                     ?? detailText("communication.assignmentChooseLine"))
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(!state.canSubmit)
        }
        .padding(.horizontal, 20)
        .padding(.top, 12)
        .padding(.bottom, 8)
        .background(Color(uiColor: .systemGroupedBackground))
    }
}

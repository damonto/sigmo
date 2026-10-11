import SwiftUI

struct CallDetailView: View {
    let record: CallRecord
    let history: CallHistoryState
    @Environment(CallsState.self) private var calls
    @State private var messageDraft: MessagesState?

    private var line: CallLine? { history.line }
    private var canMessage: Bool { history.canMessage(record) }

    private var summaryLabel: LocalizedStringKey {
        if record.missed { return "calls.missedCall" }
        return record.direction == "incoming" ? "calls.incomingCall" : "calls.outgoingCall"
    }

    var body: some View {
        List {
            Section {
                contactHeader
                    .listRowInsets(EdgeInsets(top: 0, leading: 0, bottom: 4, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
            callSummary
            lineDetails
            recordings
        }
        .listStyle(.insetGrouped)
        .sheet(item: $messageDraft) { draft in
            NavigationStack {
                NewMessageView(state: draft) { _ in messageDraft = nil }
            }
        }
        .onChange(of: canMessage) { _, available in
            if !available { dismissMessage() }
        }
        .onChange(of: history.session.accessRevision) { _, _ in dismissMessage() }
        .onDisappear { calls.recordings.stopPlayback() }
        .navigationTitle("calls.details")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var callSummary: some View {
        let duration = record.duration()
        return Section {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: record.direction == "incoming" ? "phone.arrow.down.left" : "phone.arrow.up.right")
                    .font(.title3)
                    .foregroundStyle(record.missed ? .red : .secondary)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 6) {
                    Text(record.date, format: .dateTime.hour().minute())
                        .font(.headline)
                    Text(summaryLabel)
                        .foregroundStyle(record.missed ? .red : .primary)
                    if !duration.isEmpty {
                        LabeledContent("calls.duration", value: duration)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.vertical, 4)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("call-detail-event")
            LabeledContent("calls.status", value: record.stateLabel)
            if !record.reason.isEmpty && record.reason != "normal" {
                LabeledContent("calls.reason", value: record.reasonLabel)
            }
        } header: {
            Text(record.date, format: .dateTime.year().month(.wide).day())
                .textCase(nil)
        }
    }

    private var lineDetails: some View {
        Section {
            LabeledContent("calls.route", value: record.routeLabel)
            if let line {
                VStack(alignment: .leading, spacing: 5) {
                    Text("calls.line").font(.subheadline).foregroundStyle(.secondary)
                    Text(line.label)
                    Text(line.backendName).font(.subheadline).foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
            if let profile = record.profileId {
                VStack(alignment: .leading, spacing: 5) {
                    Text("calls.sim").font(.subheadline).foregroundStyle(.secondary)
                    Text(profile).font(.subheadline).textSelection(.enabled)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private var recordings: some View {
        let line = history.line
        let entries = calls.recordings.items.filter {
            $0.backendID == line?.backendID && $0.modemID == line?.modemID
                && $0.callID == record.id && $0.profileID == record.profileId
                && ($0.backendInstanceID == nil || $0.backendInstanceID == record.backendId)
                && ($0.callUUID == nil || $0.callUUID == record.callUUID)
        }
        return Section("calls.recordings") {
            if entries.isEmpty { Text("calls.noRecordings").foregroundStyle(.secondary) }
            ForEach(entries) { CallRecordingRow(entry: $0) }
        }
    }

    private var contactHeader: some View {
        let contactName = calls.contacts.name(for: record.number, region: line?.region ?? "")
        return VStack(spacing: 20) {
            VStack(spacing: 14) {
                Image(systemName: "person.fill")
                    .font(.system(size: 44, weight: .medium))
                    .foregroundStyle(.white)
                    .frame(width: 96, height: 96)
                    .background(.gray.gradient, in: Circle())
                    .accessibilityHidden(true)
                VStack(spacing: 5) {
                    if let contactName { Text(contactName).font(.title2.weight(.semibold)) }
                    Text(PhoneNumberDisplay.format(record.number, regionCode: line?.region ?? ""))
                        .font(contactName == nil ? .title2.weight(.semibold) : .subheadline)
                        .foregroundStyle(contactName == nil ? .primary : .secondary)
                        .textSelection(.enabled)
                        .accessibilityIdentifier("call-detail-number")
                }
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            }
            GlassEffectContainer(spacing: 8) {
                HStack(alignment: .top, spacing: 24) {
                    CallDetailAction(title: "calls.dial", symbol: "phone.fill", identifier: "call-detail-call", action: callback)
                        .disabled(calls.hasCall || !record.terminal || record.number.isEmpty)
                    CallDetailAction(title: "calls.message", symbol: "message.fill", identifier: "call-detail-message") {
                        messageDraft = history.messageDraft(for: record)
                    }
                    .disabled(!canMessage)
                }
                .frame(maxWidth: 320)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 8)
    }

    private func callback() {
        if let line, record.profileId == line.profileID, calls.available(line) {
            Task { await calls.dial(number: record.number, line: line) }
        } else { calls.callbackNumber = record.number }
    }

    private func dismissMessage() {
        messageDraft?.clear()
        messageDraft = nil
    }
}

private struct CallDetailAction: View {
    let title: LocalizedStringKey
    let symbol: String
    let identifier: String
    let action: () -> Void
    @Environment(\.isEnabled) private var isEnabled
    @ScaledMetric(relativeTo: .title3) private var symbolSize = 22

    var body: some View {
        VStack(spacing: 10) {
            Button(action: action) {
                Image(systemName: symbol)
                    .font(.system(size: min(symbolSize, 32), weight: .semibold))
                    .frame(width: 52, height: 52)
            }
            .buttonStyle(.glass)
            .buttonBorderShape(.circle)
            .accessibilityLabel(Text(title))
            .accessibilityIdentifier(identifier)
            Text(title)
                .font(.subheadline)
                .foregroundStyle(isEnabled ? .primary : .secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityHidden(true)
        }
        .frame(maxWidth: .infinity)
    }
}

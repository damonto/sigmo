import SwiftUI

// Both entry points render the same history. Their parents own line selection and requests.
struct CallHistoryDetail: View {
    let record: CallRecord
    let session: BackendSession
    let modemID: String
    var lineTitle: String?
    var region: String?
    let canMessage: Bool
    let message: () -> Void
    let callback: () -> Void
    @Environment(CallsState.self) private var calls

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
            NavigationLink { SaveContactNumberView(session: session, number: record.number) } label: {
                Label("communication.addContact", systemImage: "person.crop.circle.badge.plus")
            }
        }
        .listStyle(.insetGrouped)
        .task { await calls.recordings.load() }
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
            if let lineTitle, !lineTitle.isEmpty {
                VStack(alignment: .leading, spacing: 5) {
                    Text("calls.line").font(.subheadline).foregroundStyle(.secondary)
                    Text(lineTitle)
                }
                .accessibilityElement(children: .combine)
            }
            if record.historyProfileId != nil && record.historyProfileId != record.profileId {
                Text("communication.originalLine").font(.caption).foregroundStyle(.secondary)
            }

        }
    }

    private var recordings: some View {
        let entries = calls.recordings.items(for: session.id).filter {
            $0.modemID == modemID
                && $0.callID == record.id && $0.profileID == record.profileId
                && ($0.backendInstanceID == nil || $0.backendInstanceID == record.backendId)
                && ($0.callUUID == nil || $0.callUUID == record.callUUID)
        }
        return Group {
            if !entries.isEmpty {
                Section("calls.recordings") {
                    ForEach(entries) { CallRecordingRow(entry: $0) }
                }
            }
        }
    }

    private var contactHeader: some View {
        let name = calls.name(number: record.number, session: session, region: region)
        let contactName: String? = name == PhoneNumberDisplay.format(record.number, regionCode: region) ? nil : name
        return VStack(spacing: 20) {
            VStack(spacing: 14) {
                ContactAvatarView(session: session, contact: session.contact(for: record.number), fallback: record.number, size: 96)
                VStack(spacing: 5) {
                    if let contactName { Text(contactName).font(.title2.weight(.semibold)) }
                    Text(PhoneNumberDisplay.format(record.number, regionCode: region ?? ""))
                        .font(contactName == nil ? .title2.weight(.semibold) : .subheadline)
                        .foregroundStyle(contactName == nil ? .primary : .secondary)
                        .textSelection(.enabled)
                        .accessibilityIdentifier("call-detail-number")
                }
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            }
            ContactActionButtons(canMessage: canMessage,
                                 canCall: !calls.hasCall && record.terminal && !record.number.isEmpty,
                                 messageIdentifier: "call-detail-message", callIdentifier: "call-detail-call",
                                 message: message, call: callback)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 8)
    }

}

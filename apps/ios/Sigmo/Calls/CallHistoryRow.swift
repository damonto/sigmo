import SwiftUI

struct CallHistoryRow: View {
    let record: CallRecord
    let session: BackendSession
    var region: String?
    var source: String?
    let select: () -> Void
    let callback: () -> Void
    @Environment(CallsState.self) private var calls
    @Environment(\.calendar) private var calendar
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 16) {
            Button(action: select) {
                HStack(alignment: .center, spacing: 12) {
                    ContactAvatarView(session: session, contact: session.contact(for: record.number), fallback: record.number, size: 42)
                    VStack(alignment: .leading, spacing: 4) {
                        if dynamicTypeSize.isAccessibilitySize {
                            name
                            route
                            timestamp
                        } else {
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                name
                                Spacer(minLength: 0)
                                timestamp.fixedSize(horizontal: true, vertical: false)
                            }
                            route
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityIdentifier("call-record-\(record.id)")
            Button(action: callback) {
                Image(systemName: "phone.fill")
                    .frame(minWidth: 44, minHeight: 44)
                    .background(Color(uiColor: .secondarySystemBackground), in: Circle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("calls.callback"))
            .accessibilityValue(PhoneNumberDisplay.format(record.number, regionCode: region))
            .disabled(!record.terminal || calls.hasCall)
        }
        .padding(.vertical, 4)
        .alignmentGuide(.listRowSeparatorLeading) { _ in 54 }
    }

    private var name: some View {
        Text(calls.name(number: record.number, session: session, region: region))
            .font(.headline).foregroundStyle(record.missed ? .red : .primary)
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
    }

    private var route: some View {
        HStack(alignment: .firstTextBaseline, spacing: 4) {
            Image(systemName: record.direction == "incoming" ? "phone.arrow.down.left" : "phone.arrow.up.right")
                .font(.caption)
                .accessibilityHidden(true)
            Text(routeDescription)
                .font(.subheadline)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
        }
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: false, vertical: true)
    }

    private var routeDescription: String {
        let route = record.terminal ? record.routeLabel : record.stateLabel
        guard let source, !source.isEmpty else { return route }
        return "\(route) · \(source)"
    }

    private var timestamp: some View {
        Text(record.date, format: dateStyle)
            .font(.caption).foregroundStyle(.secondary)
    }

    private var dateStyle: Date.FormatStyle {
        if calendar.isDateInToday(record.date) {
            return .dateTime.hour().minute()
        }
        if calendar.isDate(record.date, equalTo: .now, toGranularity: .year) {
            return .dateTime.month(.abbreviated).day()
        }
        return .dateTime.year().month(.abbreviated).day()
    }
}

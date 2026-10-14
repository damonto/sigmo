import SwiftUI

struct MessageConversationRow: View {
    let session: BackendSession
    let participant: String
    let preview: String
    let timestamp: Date?
    let unread: Bool
    var region: String?
    var line: String?
    var showsDisclosure = true
    @Environment(CallsState.self) private var calls
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        HStack(spacing: 12) {
            ContactAvatarView(session: session, contact: session.contact(for: participant), fallback: participant)
                .overlay(alignment: .leading) {
                    if unread { Circle().fill(.blue).frame(width: 8, height: 8).offset(x: -14) }
                }
            VStack(alignment: .leading, spacing: 5) {
                HStack(alignment: .firstTextBaseline) {
                    Text(calls.name(number: participant, session: session, region: region))
                        .font(.headline).lineLimit(typeSize.isAccessibilitySize ? nil : 1)
                    Spacer(minLength: 8)
                    if let timestamp {
                        Text(timestamp, format: .dateTime.month(.abbreviated).day().hour().minute())
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                Text(preview).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                if let line, !line.isEmpty {
                    Text(line).font(.caption2).foregroundStyle(.secondary).lineLimit(typeSize.isAccessibilitySize ? nil : 1)
                }
            }
            if showsDisclosure {
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(unread ? String(localized: "messages.unread") : "")
    }
}

import SwiftUI

// Presentation data keeps API-specific read ownership out of the shared timeline.
struct MessageTimelineItem: Identifiable {
    let id: Int64
    let text: String
    let timestamp: Date?
    let status: String
    let incoming: Bool
    var source: String?
    var reassigned = false

    init(_ message: SMSMessage) {
        id = message.id; text = message.text; timestamp = message.timestamp
        status = message.status; incoming = message.incoming
    }

    init(_ message: CommunicationMessage, source: String) {
        id = message.id; text = message.text; timestamp = CallRecord.date(message.timestamp)
        status = message.status; incoming = message.incoming
        self.source = source
        reassigned = message.historyProfileId != message.profileId
    }
}

struct MessageTimeline: View {
    let messages: [MessageTimelineItem]
    var loading = false
    var older: (() -> Void)?
    var visibilityThreshold = 0.5
    let visible: (Int64, Bool) -> Void
    let refresh: () async -> Void
    @State private var atBottom = true

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 8) {
                    if loading && messages.isEmpty { ProgressView("messages.loading").padding() }
                    if let older {
                        Button("communication.older", action: older).disabled(loading).padding()
                    }
                    ForEach(Array(messages.enumerated()), id: \.element.id) { index, message in
                        if let date = message.timestamp, startsDay(at: index) {
                            Text(date, format: .dateTime.year().month(.abbreviated).day())
                                .font(.caption.weight(.semibold)).foregroundStyle(.secondary).padding(.vertical, 12)
                        }
                        MessageBubble(message: message)
                            .id(message.id)
                            .onScrollVisibilityChange(threshold: visibilityThreshold) { visible(message.id, $0) }
                    }
                    Color.clear.frame(height: 1).id("bottom")
                        .onScrollVisibilityChange { atBottom = $0 }
                }
                .padding(.horizontal, 14).padding(.vertical, 8)
            }
            .defaultScrollAnchor(.bottom)
            .refreshable { await refresh() }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: messages.last?.id) { old, _ in
                if old == nil || atBottom { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .overlay(alignment: .bottomTrailing) {
                if !atBottom && !messages.isEmpty {
                    Button("messages.latest", systemImage: "arrow.down") { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
                        .buttonStyle(.glass).buttonBorderShape(.capsule).padding()
                }
            }
        }
    }

    private func startsDay(at index: Int) -> Bool {
        guard index > 0, let date = messages[index].timestamp, let previous = messages[index - 1].timestamp else { return true }
        return !Calendar.current.isDate(date, inSameDayAs: previous)
    }
}

private struct MessageBubble: View {
    let message: MessageTimelineItem
    private static let linkDetector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)

    private var attributed: AttributedString {
        var value = AttributedString(message.text)
        guard let detector = Self.linkDetector else { return value }
        for match in detector.matches(in: message.text, range: NSRange(message.text.startIndex..., in: message.text)) {
            if let range = Range(match.range, in: message.text), let attributedRange = Range(range, in: value), let url = match.url {
                value[attributedRange].link = url
            }
        }
        return value
    }

    var body: some View {
        HStack {
            if !message.incoming { Spacer(minLength: 44) }
            VStack(alignment: message.incoming ? .leading : .trailing, spacing: 3) {
                Text(attributed).textSelection(.enabled)
                    .padding(.horizontal, 13).padding(.vertical, 9)
                    .foregroundStyle(message.incoming ? Color.primary : .white)
                    .tint(message.incoming ? .blue : .white)
                    .background(message.incoming ? Color(uiColor: .secondarySystemBackground) : Color.green,
                                in: RoundedRectangle(cornerRadius: 20))
                    .contextMenu { Button("common.copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = message.text } }
                HStack(spacing: 5) {
                    if let timestamp = message.timestamp { Text(timestamp, format: .dateTime.hour().minute()) }
                    if !message.incoming { Text(status) }
                }.font(.caption2).foregroundStyle(.secondary).padding(.horizontal, 4)
                if let source = message.source {
                    Text(source).font(.caption2).foregroundStyle(.secondary).padding(.horizontal, 4)
                }
                if message.reassigned {
                    Text("communication.originalLine").font(.caption2).foregroundStyle(.secondary).padding(.horizontal, 4)
                }
            }
            if message.incoming { Spacer(minLength: 44) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("message-\(message.id)")
    }

    private var status: String {
        switch message.status {
        case "sent", "stored-sent": String(localized: "messages.status.sent")
        case "delivered": String(localized: "messages.status.delivered")
        case "failed", "delivery-failed": String(localized: "messages.status.failed")
        case "stored-unsent", "pending", "sending": String(localized: "messages.status.pending")
        default: message.status
        }
    }
}


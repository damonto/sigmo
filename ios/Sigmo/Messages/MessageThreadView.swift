import SwiftUI

struct MessageThreadView: View {
    @Bindable var state: MessageThreadState
    var inline = false
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.nativePush) private var push
    @State private var pendingSend: UUID?
    @State private var visibleMessageID: Int64?
    @State private var atBottom = true

    private var identity: PushConversation {
        PushConversation(backendID: push?.backendID(sessionID: state.session.id) ?? state.badge?.backendId ?? "", conversationID: state.conversation.id)
    }

    private struct ReadIdentity: Equatable {
        let messageID: Int64?
        let loading: Bool
        let active: Bool
    }

    var body: some View {
        Group {
            if inline {
                content
                    .safeAreaBar(edge: .top, spacing: 0) {
                        conversationTitle.padding(.horizontal).padding(.vertical, 8)
                    }
            } else {
                content
                    .navigationTitle(state.conversation.participant)
                    .navigationSubtitle(state.conversation.lineDescription)
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbarBackgroundVisibility(.automatic, for: .navigationBar)
                    .toolbar(.hidden, for: .tabBar)
            }
        }
        .task(id: scenePhase == .background) {
            guard scenePhase != .background else { return }
            await state.load()
        }
        .task(id: push?.eventRevision) { if scenePhase == .active { await state.load() } }
        .task(id: pendingSend) {
            guard let pendingSend, !Task.isCancelled else { return }
            await state.send()
            if self.pendingSend == pendingSend { self.pendingSend = nil }
        }
        .task(id: ReadIdentity(messageID: visibleMessageID, loading: state.loading, active: scenePhase == .active)) {
            guard scenePhase == .active, !state.loading, let id = visibleMessageID else { return }
            await state.markRead(through: id)
        }
        .onAppear { push?.visibleConversation = identity }
        .onChange(of: identity) { _, value in push?.visibleConversation = value }
        .onDisappear { if push?.visibleConversation == identity { push?.visibleConversation = nil }; pendingSend = nil }
        .onChange(of: scenePhase) { _, value in if value == .background { pendingSend = nil } }
        .task(id: state.badge) {
            if let value = state.badge {
                push?.record(value, sessionID: state.session.id)
                await push?.removeNotifications(backendID: value.backendId, conversationID: state.conversation.id)
            }
        }
    }

    private var content: some View {
        VStack(spacing: 0) {
            if let failure = state.failure {
                FailureView(failure: failure).padding(.horizontal)
            }
            timeline
        }
        .background(Color(uiColor: .systemBackground))
        .safeAreaBar(edge: .bottom, spacing: 0) {
            VStack(spacing: 4) {
                if !state.canSend {
                    Text("messages.inactiveLine")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal)
                }
                MessageComposer(text: $state.draft, sending: state.sending, enabled: state.canSend) {
                    pendingSend = UUID()
                }
            }
        }
    }

    private var conversationTitle: some View {
        VStack(spacing: 2) {
            Text(state.conversation.participant)
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("message-participant")
            Text(state.conversation.lineDescription)
                .font(.caption)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("message-line")
        }
        .lineLimit(1)
    }

    private var timeline: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 8) {
                    if state.loading && !state.loaded { ProgressView("messages.loading").padding() }
                    ForEach(Array(state.messages.enumerated()), id: \.element.id) { index, message in
                        if index == 0 || !Calendar.current.isDate(message.timestamp, inSameDayAs: state.messages[index-1].timestamp) {
                            Text(message.timestamp, format: .dateTime.year().month(.abbreviated).day())
                                .font(.caption.weight(.semibold)).foregroundStyle(.secondary).padding(.vertical, 12)
                        }
                        SMSBubble(message: message)
                            .id(message.id)
                            .onScrollVisibilityChange(threshold: 0.1) { visible in
                                if visible { visibleMessageID = max(visibleMessageID ?? 0, message.id) }
                            }
                    }
                    Color.clear.frame(height: 1).id("bottom")
                        .onScrollVisibilityChange { visible in atBottom = visible }
                }.padding(.horizontal, 14).padding(.vertical, 8)
            }
            .defaultScrollAnchor(.bottom)
            .refreshable { await state.load() }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: state.messages.last?.id) { old, _ in
                if old == nil || atBottom { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .overlay(alignment: .bottomTrailing) {
                if !atBottom && !state.messages.isEmpty {
                    Button("messages.latest", systemImage: "arrow.down") { withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
                        .buttonStyle(.glass).buttonBorderShape(.capsule).padding()
                }
            }
        }
    }
}

private struct SMSBubble: View {
    let message: SMSMessage
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
                    Text(message.timestamp, format: .dateTime.hour().minute())
                    if !message.incoming { Text(status) }
                }.font(.caption2).foregroundStyle(.secondary).padding(.horizontal, 4)
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

struct MessageComposer: View {
    @Binding var text: String
    let sending: Bool
    let enabled: Bool
    let send: () -> Void

    private var canSend: Bool {
        enabled && !sending && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        GlassEffectContainer(spacing: 8) {
            HStack(alignment: .bottom, spacing: 10) {
                TextField("messages.textPlaceholder", text: $text, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1...6)
                    .padding(.horizontal, 16).padding(.vertical, 12)
                    .frame(minHeight: 44)
                    .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 26))
                    .disabled(sending)
                    .accessibilityLabel("messages.textPlaceholder")
                    .accessibilityIdentifier("message-input")
                sendButton
            }
        }
        .padding(.horizontal, 16).padding(.vertical, 8)
    }

    private var sendButton: some View {
        Button(action: send) {
            Image(systemName: "arrow.up")
                .font(.body.weight(.semibold))
                .frame(minWidth: 20, minHeight: 20)
                // Preserve the scaled button size while showing progress.
                .opacity(sending ? 0 : 1)
                .overlay {
                    if sending { ProgressView() }
                }
        }
        .buttonStyle(.glassProminent)
        .buttonBorderShape(.circle)
        .controlSize(.large)
        .tint(.green)
        .frame(minWidth: 44, minHeight: 44)
        .disabled(!canSend)
        .accessibilityLabel("messages.send")
        .accessibilityIdentifier("message-send")
    }
}

struct NewMessageView: View {
    @Bindable var state: MessagesState
    let completed: (SMSConversation?) -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @FocusState private var recipientFocused: Bool
    @State private var pendingSend: UUID?
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("messages.to").foregroundStyle(.secondary)
                TextField("messages.recipient", text: $state.newRecipient)
                    .keyboardType(.phonePad).textContentType(.telephoneNumber)
                    .autocorrectionDisabled().focused($recipientFocused)
                    .accessibilityIdentifier("message-recipient")
            }.padding(.horizontal).padding(.top)
            Divider()
            if let modem = state.modem { Text(modem.number.isEmpty ? modem.carrier : modem.number).font(.caption).foregroundStyle(.secondary).padding(.horizontal) }
            if let failure = state.failure { FailureView(failure: failure).padding(.horizontal) }
            Spacer()
        }
        .safeAreaBar(edge: .bottom, spacing: 0) {
            MessageComposer(text: $state.newDraft, sending: state.sending, enabled: !state.newRecipient.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) { pendingSend = UUID() }
        }
        .navigationTitle("messages.new").navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } } }
        .task { recipientFocused = true }
        .task(id: pendingSend) {
            guard let pendingSend, !Task.isCancelled else { return }
            let conversation = await state.sendNew()
            guard self.pendingSend == pendingSend, !Task.isCancelled else { return }
            self.pendingSend = nil
            if state.newDraft.isEmpty { completed(conversation) }
        }
        .onDisappear { pendingSend = nil }
        .onChange(of: scenePhase) { _, value in if value == .background { pendingSend = nil } }
    }
}

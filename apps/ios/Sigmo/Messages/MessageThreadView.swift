import SwiftUI

struct MessageThreadView: View {
    @Bindable var state: MessageThreadState
    var inline = false
    @Environment(CallsState.self) private var calls
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.nativePush) private var push
    @State private var confirmCall = false
    @State private var pendingSend: UUID?
    @State private var visibleMessageID: Int64?

    private var regionCode: String? {
        state.modem?.sim.identifier == state.conversation.profileId ? state.modem?.sim.regionCode : nil
    }

    private var identity: PushConversation {
        PushConversation(backendID: push?.backendID(sessionID: state.session.id) ?? state.badge?.backendId ?? "", conversationID: state.conversation.id)
    }

    private var sendingLine: String {
        if state.sendingProfileID != state.conversation.profileId,
           let modem = state.modem, modem.sim.identifier == state.sendingProfileID {
            return modem.communicationLabel
        }
        return state.conversation.lineDescription(regionCode: regionCode)
    }

    private struct ReadIdentity: Equatable {
        let messageID: Int64?
        let loading: Bool
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
                    .navigationTitle(calls.name(number: state.conversation.participant, session: state.session, region: regionCode))
                    .toolbar { ToolbarItem(placement: .principal) { conversationTitle } }
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbarBackgroundVisibility(.automatic, for: .navigationBar)
                    .toolbar(.hidden, for: .tabBar)
            }
        }
        .confirmationDialog("communication.useCurrentLine", isPresented: $confirmCall) {
            Button("communication.useCurrentLine") { dialCurrent() }
            Button("common.cancel", role: .cancel) { }
        }
        .toolbar {
            if state.session.capabilities?.contains("nativeCalls") == true {
                Button("communication.dial", systemImage: "phone") {
                    if state.canUseActive { confirmCall = true } else { dialCurrent() }
                }.disabled(calls.hasCall || state.modem == nil)
            }
            NavigationLink { SaveContactNumberView(session: state.session, number: state.conversation.participant) } label: { Label("communication.addContact", systemImage: "person.crop.circle.badge.plus") }
        }
        .detailErrorAlert(state.failure)
        .foregroundTask {
            await state.load()
        }
        .task(id: push?.eventRevision) { if scenePhase == .active { await state.load() } }
        .task(id: pendingSend) {
            guard let pendingSend, !Task.isCancelled else { return }
            await state.send()
            if self.pendingSend == pendingSend { self.pendingSend = nil }
        }
        .foregroundTask(id: ReadIdentity(messageID: visibleMessageID, loading: state.loading)) {
            guard !state.loading, let id = visibleMessageID else { return }
            await state.markRead(through: id)
        }
        .onAppear { push?.visibleConversation = identity }
        .onChange(of: identity) { _, value in push?.visibleConversation = value }
        .onDisappear { if push?.visibleConversation == identity { push?.visibleConversation = nil }; pendingSend = nil }
        .task(id: state.badge) {
            if let value = state.badge {
                push?.record(value, sessionID: state.session.id)
                await push?.removeNotifications(backendID: value.backendId, conversationID: state.conversation.id)
            }
        }
        .task(id: state.snapshotBadge) {
            if let value = state.snapshotBadge { push?.record(value, sessionID: state.session.id) }
        }
    }

    private func dialCurrent() {
        guard let modem = state.modem else { return }
        Task { await calls.dial(number: state.conversation.participant, line: CallLine(session: state.session, modem: modem)) }
    }

    private var content: some View {
        VStack(spacing: 0) {
            timeline
        }
        .background(Color(uiColor: .systemBackground))
        .safeAreaBar(edge: .bottom, spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                if !state.canSend {
                    Text("messages.inactiveLine")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 16)
                }
                MessageComposer(text: $state.draft, sending: state.sending, enabled: state.canSend) {
                    pendingSend = UUID()
                }
            }
        }
    }

    private var conversationTitle: some View {
        VStack(spacing: 2) {
            Text(calls.name(number: state.conversation.participant, session: state.session, region: regionCode))
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("message-participant")
            if state.canUseActive {
                Menu {
                    Button { state.useActiveProfile() } label: {
                        Text("\(detailText("communication.useCurrentLine")) · \(state.modem?.communicationLabel ?? "")")
                    }
                        .accessibilityIdentifier("message-use-current-sim")
                } label: {
                    HStack(spacing: 4) {
                        Text(sendingLine)
                            .accessibilityIdentifier("message-line")
                        Image(systemName: "chevron.down")
                    }.font(.caption)
                }
                .accessibilityIdentifier("message-line-picker")
            } else {
                Text(sendingLine)
                    .font(.caption).foregroundStyle(.secondary)
                    .accessibilityIdentifier("message-line")
            }
        }
        .lineLimit(1)
    }

    private var timeline: some View {
        MessageTimeline(messages: state.messages.map(MessageTimelineItem.init), loading: state.loading && !state.loaded,
                        older: state.cursor.isEmpty ? nil : { Task { await state.load(more: true) } },
                        visibilityThreshold: 0.1, visible: { id, visible in
                            if visible { visibleMessageID = max(visibleMessageID ?? 0, id) }
                        }, refresh: { await state.load() })
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
    @FocusState private var recipientFocused: Bool
    @State private var pendingSend: UUID?
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("messages.to").foregroundStyle(.secondary)
                TextField("messages.recipient", text: $state.newRecipient)
                    .keyboardType(.phonePad).textContentType(.telephoneNumber)
                    .autocorrectionDisabled().focused($recipientFocused)
                    .disabled(state.sending)
                    .accessibilityIdentifier("message-recipient")
            }.padding(.horizontal).padding(.top)
            Divider()
            if let modem = state.modem {
                Text(modem.number.isEmpty ? modem.carrier : PhoneNumberDisplay.format(modem.number, regionCode: modem.sim.regionCode))
                    .font(.caption).foregroundStyle(.secondary).padding(.horizontal)
            }
            Spacer()
        }
        .safeAreaBar(edge: .bottom, spacing: 0) {
            MessageComposer(text: $state.newDraft, sending: state.sending, enabled: !state.newRecipient.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) { pendingSend = UUID() }
        }
        .navigationTitle("messages.new").navigationBarTitleDisplayMode(.inline)
        .toolbar {
            NavigationLink { SaveContactNumberView(session: state.session, number: state.newRecipient) } label: { Label("communication.addContact", systemImage: "person.crop.circle.badge.plus") }
        }
        .detailErrorAlert(state.failure)
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
    }
}

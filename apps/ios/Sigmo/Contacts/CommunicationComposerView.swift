import SwiftUI

private struct CommunicationActionSheet: ViewModifier {
    @Binding var action: BackendCommunicationAction?
    var sent: () -> Void
    @Environment(CallsState.self) private var calls
    @State private var pending: (String, CallLine)?
    func body(content: Content) -> some View {
        content.sheet(item: $action, onDismiss: {
            guard let pending else { return }
            self.pending = nil
            Task { await calls.dial(number: pending.0, line: pending.1) }
        }) { action in
            CommunicationComposerView(session: action.session, action: action.action, sent: sent) { pending = ($0, $1) }
        }
    }
}

extension View {
    func communicationActions(_ action: Binding<CommunicationAction?>, session: BackendSession, sent: @escaping () -> Void = {}) -> some View {
        communicationActions(Binding(
            get: { action.wrappedValue.map { BackendCommunicationAction(session: session, action: $0) } },
            set: { action.wrappedValue = $0?.action }
        ), sent: sent)
    }

    func communicationActions(_ action: Binding<BackendCommunicationAction?>, sent: @escaping () -> Void = {}) -> some View {
        modifier(CommunicationActionSheet(action: action, sent: sent))
    }
}

struct CommunicationComposerView: View {
    let session: BackendSession
    var sent: () -> Void
    var dial: (String, CallLine) -> Void
    @State private var state: CommunicationComposerState
    @State private var ussd: CallUSSD?
    @Environment(\.dismiss) private var dismiss
    @Environment(CallsState.self) private var calls
    init(session: BackendSession, action: CommunicationAction, sent: @escaping () -> Void, dial: @escaping (String, CallLine) -> Void) {
        self.session = session; self.sent = sent; self.dial = dial
        _state = State(initialValue: CommunicationComposerState(session: session, action: action))
    }
    var body: some View {
        NavigationStack {
            Group {
                if state.calling {
                    ScrollView {
                        VStack(spacing: 16) {
                            if let ussd {
                                Text("\(ussd.line.backendName) · \(ussd.line.label)").font(.subheadline).foregroundStyle(.secondary)
                                CallServiceView(ussd: ussd)
                            } else {
                                CommunicationModemPicker(state: state)
                                CallDialpadSurface(number: $state.number,
                                                   canDial: state.canSubmit && CallDigits.callable(state.number) && !calls.hasCall,
                                                   dial: { Task { await submit() } })
                            }
                        }
                        .padding(24)
                    }
                    .background(Color(uiColor: .systemBackground))
                } else {
                    VStack(alignment: .leading, spacing: 16) {
                        HStack {
                            Text("messages.to").foregroundStyle(.secondary)
                            TextField("communication.number", text: $state.number)
                                .keyboardType(.phonePad).textContentType(.telephoneNumber)
                        }
                        Divider()
                        CommunicationModemPicker(state: state)
                        Spacer()
                    }
                    .padding(.horizontal, 16).padding(.top, 12)
                    .safeAreaBar(edge: .bottom, spacing: 0) {
                        MessageComposer(text: $state.text, sending: state.busy, enabled: state.selected != nil && !state.number.isEmpty) {
                            Task { await submit() }
                        }
                    }
                }
            }
            .navigationTitle(ussd != nil ? LocalizedStringKey("calls.ussd") : state.calling ? LocalizedStringKey("calls.keypad") : LocalizedStringKey("communication.newMessage"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.cancel", systemImage: "xmark") { dismiss() }.labelStyle(.iconOnly).disabled(state.busy)
                }
            }
            .interactiveDismissDisabled(state.busy)
            .detailErrorAlert($state.failure)
            .task {
                await state.load()
            }
            .onChange(of: session.accessRevision) { _, _ in ussd?.close(); dismiss() }
            .onDisappear { ussd?.close() }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }
    private func submit() async {
        if state.calling {
            guard let modem = await state.currentModem() else { return }
            let callLine = CallLine(session: session, modem: modem)
            if CallDigits.isService(state.number) {
                let service = CallUSSD(session: session, line: callLine)
                ussd = service
                service.send(state.number, initial: true)
                return
            }
            dial(CallDigits.normalize(state.number), callLine)
            dismiss()
        } else if await state.send() { sent(); dismiss() }
    }
}

struct CommunicationModemPicker: View {
    @Bindable var state: CommunicationComposerState
    var compact = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if state.availableModems.count > 1 {
                Menu {
                    Picker("communication.chooseModem", selection: Binding(get: { state.selectedID }, set: { state.selectModem($0) })) {
                        Text("communication.chooseModem").tag("")
                        ForEach(state.availableModems) { modem in
                            Text("\(state.session.configuration.name) · \(modem.communicationLabel)").tag(modem.id)
                        }
                    }
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "server.rack").foregroundStyle(.blue)
                        Text("\(state.session.configuration.name) · \(state.selected?.communicationLabel ?? detailText("communication.chooseModem"))")
                            .lineLimit(1)
                        Image(systemName: "chevron.up.chevron.down").font(.caption).foregroundStyle(.secondary)
                    }.font(compact ? .caption : .subheadline).contentShape(Rectangle())
                }
                .buttonStyle(.plain).disabled(state.busy)
                .accessibilityIdentifier("communication-modem-picker")
            } else if let modem = state.selected {
                Text(state.fixedModemID == nil ? "\(state.session.configuration.name) · \(modem.communicationLabel)" : modem.communicationLabel).font(compact ? .caption : .subheadline).lineLimit(1).foregroundStyle(.secondary)
            }
            if !compact && state.unavailable && state.selectedID.isEmpty {
                Text("communication.modemUnavailable").font(.footnote).foregroundStyle(.secondary)
            } else if state.availableModems.isEmpty {
                Text("communication.noModems").font(.footnote).foregroundStyle(.secondary)
            }
        }
    }
}

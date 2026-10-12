import SwiftUI

struct CallPresentation: ViewModifier {
    var isActive = true
    @Environment(CallsState.self) private var calls
    @State private var pendingCallback: (number: String, line: CallLine)?

    func body(content: Content) -> some View {
        VStack(spacing: 0) {
            if isActive, let call = calls.primary {
                Button { calls.presented = true } label: {
                    HStack(spacing: 10) {
                        Image(systemName: calls.incoming == nil ? "phone.fill" : "phone.arrow.down.left.fill")
                        Text(calls.name(call)).font(.callout.weight(.semibold)).lineLimit(1)
                        Spacer(minLength: 4)
                        if calls.incoming != nil { Text("calls.incoming").font(.callout) }
                        else {
                            TimelineView(.periodic(from: .now, by: 1)) { context in
                                Text(call.record?.duration(at: context.date) ?? callText("calls.state.dialing")).font(.callout).monospacedDigit()
                            }
                        }
                        Image(systemName: "chevron.up")
                    }.padding(.horizontal, 18).frame(minHeight: 44).foregroundStyle(.white)
                        .background(.green.gradient)
                }.buttonStyle(.plain).accessibilityLabel(Text("calls.returnToCall"))
                    .accessibilityIdentifier("calls-return")
            }
            content
        }
        // Only the visible host presents UI; the call itself remains app-owned.
        .fullScreenCover(isPresented: Binding(
            get: { isActive && calls.presented },
            set: { if isActive { calls.presented = $0 } }
        )) { CallScreen() }
        .sheet(isPresented: Binding(get: { isActive && calls.callbackNumber != nil }, set: { if isActive && !$0 { calls.callbackNumber = nil } }), onDismiss: {
            guard let selection = pendingCallback else { return }
            pendingCallback = nil
            Task { await calls.dial(number: selection.number, line: selection.line) }
        }) {
            if let number = calls.callbackNumber {
                CallCallbackView(number: number) { pendingCallback = (number, $0) }
            }
        }
        .alert("calls.actionFailed", isPresented: Binding(get: { isActive && calls.failure != nil && !calls.hasCall }, set: { if isActive && !$0 { calls.failure = nil } })) {
            Button("common.done", role: .cancel) { calls.failure = nil }
        } message: { Text(calls.failure?.message ?? "") }
    }
}

private struct CallCallbackView: View {
    let number: String
    let dial: (CallLine) -> Void
    @Environment(CallsState.self) private var calls
    @Environment(\.dismiss) private var dismiss
    @State private var checking = true

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(number).font(.title2).textSelection(.enabled)
                    Text("calls.chooseLineHint").font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(calls.availableLines) { line in
                    Button { call(line) } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(line.label)
                            Text(line.backendName).font(.subheadline).foregroundStyle(.secondary)
                        }
                    }.disabled(checking || calls.hasCall)
                }
                if checking { ProgressView() }
                else if calls.availableLines.isEmpty { Text("calls.lineUnavailable").foregroundStyle(.secondary) }
            }
            .navigationTitle("calls.chooseLine")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { calls.callbackNumber = nil; dismiss() } } }
            .task {
                // Cold launch may still be restoring credentials and the current SIM.
                await calls.store.refreshAll()
                guard !Task.isCancelled else { return }
                checking = false
                if let line = calls.callbackLine(for: number), !calls.hasCall { call(line) }
            }
        }
    }

    private func call(_ line: CallLine) {
        dial(line)
        calls.callbackNumber = nil
        dismiss()
    }
}

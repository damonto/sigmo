import SwiftUI

struct DemoView: View {
    @Bindable var presentation: DemoPresentation
    let liveCalls: CallsState
    @State private var showingOptions = false
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        let session = presentation.session
        VStack(spacing: 0) {
            HStack {
                Button { showingOptions = true } label: {
                    Label("demo.title", systemImage: "play.rectangle")
                        .font(.subheadline.weight(.semibold))
                }
                .accessibilityIdentifier("demoOptions")
                .disabled(presentation.isBusy || presentation.isClosing)
                Spacer()
                Button("demo.exit") {
                    close()
                }
                .accessibilityIdentifier("exitDemo")
                .disabled(presentation.isClosing)
            }
            .padding(.horizontal).padding(.vertical, 8)
            .background(Color.accentColor.opacity(0.08))
            HomeView()
        }
        .id(session.id)
        .environment(session.store)
        .environment(session.calls)
        .environment(\.nativePush, nil)
        .environment(\.demoSession, session)
        .defaultAppStorage(session.defaults)
        .interactiveDismissDisabled()
        .foregroundTask(id: session.id) { await session.store.refreshAll() }
        .task(id: session.id) { await session.calls.run() }
        .onChange(of: scenePhase, initial: true) { _, phase in
            session.store.setForeground(phase == .active)
            session.calls.foreground = phase == .active
            if phase != .active { session.calls.recordings.stopPlayback() }
        }
        .sheet(isPresented: $showingOptions) {
            NavigationStack {
                List {
                    Section {
                        Text("demo.description")
                        Text("demo.inputs").font(.footnote).foregroundStyle(.secondary)
                    }
                    Section {
                        Button("demo.receiveMessage") { perform(.receiveMessage) }
                            .accessibilityIdentifier("demoReceiveMessage")
                        Button("demo.receiveCall") { perform(.receiveCall) }
                            .disabled(session.calls.hasCall)
                            .accessibilityIdentifier("demoReceiveCall")
                        Button("demo.reset", role: .destructive) { perform(.reset) }
                            .accessibilityIdentifier("resetDemo")
                    }
                    .disabled(presentation.isBusy || presentation.isClosing)
                }
                .navigationTitle("demo.title")
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button("common.done") { showingOptions = false } } }
            }
        }
        .detailErrorAlert($presentation.failure)
        .onChange(of: liveCalls.hasCall, initial: true) { _, active in
            if active { close() }
        }
    }

    private func perform(_ action: DemoPresentation.Action) {
        showingOptions = false
        Task { await presentation.perform(action) }
    }

    private func close() {
        showingOptions = false
        Task { await presentation.close(); dismiss() }
    }
}

import SwiftUI

struct CallScreen: View {
    @Environment(CallsState.self) private var calls

    var body: some View {
        NavigationStack {
            Group {
                if let call = calls.primary {
                    ActiveCallContent(call: call)
                        // A second call must not inherit the previous call's keypad or digits.
                        .id(call.id)
                } else { ContentUnavailableView("calls.ended", systemImage: "phone.down") }
            }
            .background(Color(uiColor: .systemGroupedBackground))
            .detailErrorAlert(calls.failure ?? calls.audio.failure ?? calls.recordings.failure)
            .navigationTitle("detail.tab.calls")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("calls.minimize", systemImage: "chevron.down") { calls.presented = false }
                        .accessibilityIdentifier("calls-minimize")
                }
            }
        }
    }
}

private struct ActiveCallContent: View {
    let call: ActiveCall
    @Environment(CallsState.self) private var calls
    @State private var keypad = false
    @State private var digits = ""

    var body: some View {
        Group {
            if keypad {
                keypadControls
            } else {
                GeometryReader { geometry in
                    ScrollView {
                        callControls
                            .frame(maxWidth: 460)
                            .padding(24)
                            .frame(maxWidth: .infinity, minHeight: geometry.size.height)
                    }
                }
            }
        }
        .onChange(of: calls.incoming?.id) { _, id in
            if id != nil { keypad = false }
        }
    }

    private var callControls: some View {
        VStack(spacing: 28) {
            identity.padding(.top, 24)
            Spacer(minLength: 0)
            if let incoming = calls.incoming {
                incomingControls(incoming)
            }
            if call.ownsCall {
                if calls.audio.receiveOnly {
                    Label("calls.receiveOnly", systemImage: "mic.slash")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                if calls.audio.connecting { ProgressView("calls.connectingAudio") }
                controls
            }
        }
    }

    private var identity: some View {
        VStack(spacing: 10) {
            Image(systemName: "person.crop.circle.fill")
                .font(.system(size: 84)).foregroundStyle(.secondary).accessibilityHidden(true)
            Text(calls.name(call))
                .font(.largeTitle.weight(.medium)).textSelection(.enabled)
            Text("\(call.line.backendName) · \(call.line.label)")
                .font(.subheadline).foregroundStyle(.secondary)
            TimelineView(.periodic(from: .now, by: 1)) { context in
                VStack(spacing: 4) {
                    Text(call.record?.stateLabel ?? callText("calls.state.dialing"))
                    if let record = call.record {
                        let duration = record.duration(at: context.date)
                        if !duration.isEmpty { Text(duration).monospacedDigit() }
                    }
                    if call.record?.localHold == true { Text("calls.localHold") }
                    if call.record?.remoteHold == true { Text("calls.remoteHold") }
                }.font(.body).foregroundStyle(.secondary)
            }
        }.multilineTextAlignment(.center)
    }

    private var keypadControls: some View {
        GeometryReader { geometry in
            // The keypad replaces the call identity and controls. Its bottom
            // actions never participate in a scroll view, even at large text sizes.
            if geometry.size.width > geometry.size.height {
                HStack(spacing: 24) {
                    toneKeys(compact: true)
                    VStack(spacing: 16) {
                        sentDigits
                        keypadActions
                    }
                }
                .padding(.horizontal, 24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                VStack(spacing: 16) {
                    sentDigits
                    Spacer(minLength: 0)
                    toneKeys(compact: geometry.size.height < 560)
                    Spacer(minLength: 0)
                }
                .padding(.top, 16)
                .safeAreaInset(edge: .bottom, spacing: 16) { keypadActions.padding(.bottom, 16) }
                .frame(maxWidth: 460)
                .padding(.horizontal, 24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    private var sentDigits: some View {
        Text(digits.isEmpty ? calls.name(call) : digits)
            .font(.title2.monospacedDigit()).lineLimit(1).minimumScaleFactor(0.5)
            .accessibilityLabel(Text("calls.sentDigits")).accessibilityValue(digits)
            .accessibilityIdentifier("calls-sent-digits")
    }

    private func toneKeys(compact: Bool) -> some View {
        CallKeypad(compact: compact) { digit in
            digits = String((digits + digit).suffix(32))
            Task { await calls.act(.digits(call.id, digit)) }
        }.disabled(call.record?.canSendDTMF != true)
    }

    private var keypadActions: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(alignment: .top, spacing: 12) {
                CallControl(label: "calls.hideKeypad", symbol: "circle.grid.3x3.fill", identifier: "calls-hide-keypad") { keypad = false }
                endControl
                Spacer(minLength: 0).frame(maxWidth: .infinity)
            }
        }
    }

    private func incomingControls(_ incoming: ActiveCall) -> some View {
        VStack(spacing: 16) {
            if incoming.id != call.id {
                Text("calls.secondIncoming").font(.headline)
                Text(calls.name(incoming)).font(.title3)
                Text("\(incoming.line.backendName) · \(incoming.line.label)").font(.caption).foregroundStyle(.secondary)
            }
            GlassEffectContainer(spacing: 12) {
                HStack(alignment: .top, spacing: 20) {
                    CallControl(label: "calls.decline", symbol: "phone.down.fill", prominentTint: .red, identifier: "calls-decline") {
                        Task { await calls.act(.end(incoming.id)) }
                    }
                    CallControl(label: incoming.id == call.id ? "calls.answer" : "calls.endAndAnswer", symbol: "phone.fill", prominentTint: .green, identifier: "calls-answer") {
                        Task { await calls.answer(incoming.id, endCurrent: incoming.id != call.id) }
                    }
                }.disabled(incoming.answering)
            }
        }
    }

    private var controls: some View {
        GlassEffectContainer(spacing: 12) {
            Grid(alignment: .top, horizontalSpacing: 12, verticalSpacing: 28) {
                GridRow {
                    muteControl
                    audioControl
                    holdControl
                }
                GridRow {
                    CallControl(label: "calls.keypad", symbol: "circle.grid.3x3.fill", identifier: "calls-show-keypad") { keypad = true }
                        .disabled(call.record?.canSendDTMF != true)
                    endControl
                    recordingControl
                }
            }
        }
    }

    private var muteControl: some View {
        CallControl(label: "calls.mute", symbol: call.muted ? "mic.slash.fill" : "mic.fill", selected: call.muted, identifier: "calls-mute") {
            Task { await calls.act(.mute(call.id, !call.muted)) }
        }.disabled(calls.audio.receiveOnly)
    }

    private var audioControl: some View {
        VStack(spacing: 10) {
            Menu {
                ForEach(calls.audio.routes) { route in
                    Button(route.name) { calls.audio.selectRoute(route.id) }
                }
                if !calls.audio.microphones.isEmpty {
                    Section("calls.microphone") {
                        ForEach(calls.audio.microphones) { microphone in
                            Button(microphone.name) { calls.audio.selectMicrophone(microphone.id) }
                        }
                    }
                }
            } label: { CallControlIcon(symbol: "speaker.wave.2.fill") }
                .buttonStyle(.glass).buttonBorderShape(.circle).tint(.primary)
                .accessibilityLabel(Text("calls.audio"))
                .accessibilityValue(calls.audio.routeName)
                .accessibilityIdentifier("calls-audio")
            CallControlCaption(label: "calls.audio")
        }.frame(maxWidth: .infinity).disabled(!calls.audio.ready)
    }

    private var holdControl: some View {
        let held = call.record?.localHold == true
        return CallControl(label: held ? "calls.resume" : "calls.hold", symbol: held ? "play.fill" : "pause.fill", selected: held, identifier: "calls-hold") {
            Task { await calls.act(.hold(call.id, !held)) }
        }.disabled(call.record?.connected != true)
    }

    private var recordingControl: some View {
        let recording = calls.recordings.activeID != nil
        return CallControl(label: recording ? "calls.stopRecording" : "calls.record", symbol: recording ? "stop.circle.fill" : "record.circle", selected: recording, identifier: "calls-record") {
            Task { await calls.toggleRecording() }
        }.disabled(!calls.audio.ready || call.record?.hasMedia != true || calls.recordings.busy)
    }

    private var endControl: some View {
        CallControl(label: "calls.end", symbol: "phone.down.fill", prominentTint: .red, identifier: "calls-end") {
            Task { await calls.act(.end(call.id)) }
        }.disabled(call.endRequested)
    }
}

private struct CallControl: View {
    let label: LocalizedStringKey
    let symbol: String
    var selected = false
    var prominentTint: Color?
    let identifier: String
    let action: () -> Void
    var body: some View {
        VStack(spacing: 10) {
            if let prominentTint {
                button.buttonStyle(.glassProminent).tint(prominentTint)
            } else if selected {
                button.buttonStyle(.glassProminent).tint(.accentColor)
            } else {
                button.buttonStyle(.glass).tint(.primary)
            }
            CallControlCaption(label: label)
        }.frame(maxWidth: .infinity)
    }

    private var button: some View {
        Button(action: action) { CallControlIcon(symbol: symbol) }
            .buttonBorderShape(.circle)
            .accessibilityLabel(Text(label))
            .accessibilityAddTraits(selected ? .isSelected : [])
            .accessibilityIdentifier(identifier)
    }
}

private struct CallControlIcon: View {
    let symbol: String
    @ScaledMetric(relativeTo: .title2) private var symbolSize: CGFloat = 24

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: min(symbolSize, 32), weight: .medium))
            .frame(width: 56, height: 56)
    }
}

private struct CallControlCaption: View {
    let label: LocalizedStringKey
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        Text(label).font(.callout).multilineTextAlignment(.center)
            .foregroundStyle(isEnabled ? .primary : .secondary)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityHidden(true)
    }
}

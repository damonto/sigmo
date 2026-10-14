import SwiftUI

struct CallRecordingsView: View {
    let backendID: UUID
    @Environment(CallsState.self) private var calls
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let entries = calls.recordings.items(for: backendID)
        List {
            Section {
                Text("calls.recordingsLocal").font(.footnote).foregroundStyle(.secondary)
                if let failure = calls.recordings.failure { FailureView(failure: failure) }
                if calls.recordings.busy { ProgressView("calls.savingRecording") }
            }
            ForEach(entries) { CallRecordingRow(entry: $0) }
            if entries.isEmpty { ContentUnavailableView("calls.noRecordings", systemImage: "waveform") }
        }
        .navigationTitle("calls.recordings")
        .toolbar { ToolbarItem(placement: .confirmationAction) { Button("common.done") { dismiss() } } }
        .onDisappear { calls.recordings.stopPlayback() }
    }
}

struct CallRecordingRow: View {
    let entry: CallRecording
    @Environment(CallsState.self) private var calls
    @State private var deleting = false
    @ScaledMetric(relativeTo: .title3) private var playbackSymbolSize = 22

    private var recordings: CallRecordings { calls.recordings }
    private var player: CallRecordingPlayer { recordings.playback }
    private var playing: Bool { player.selectedID == entry.id && player.state == .playing }
    private var playbackTitle: LocalizedStringKey {
        if playing { return "calls.pausePlayback" }
        if player.selectedID == entry.id, player.state == .paused { return "calls.resumePlayback" }
        return "calls.play"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 16) {
                information
                if entry.complete { playbackButton }
            }
            recordingContent
            if let failure = recordings.failure(for: entry.id) {
                FailureView(failure: failure)
            }
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("recording-row-\(entry.id)")
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if recordings.activeID != entry.id {
                // The destructive role belongs to confirmation, so opening the
                // alert cannot optimistically remove the row from the list.
                Button { deleting = true } label: { Image(systemName: "trash") }
                    .tint(.red)
                    .accessibilityLabel(Text("common.delete"))
                    .disabled(recordings.busy)
                    .accessibilityIdentifier("recording-delete-\(entry.id)")
            }
        }
        .alert("calls.deleteRecording", isPresented: $deleting) {
            Button("common.delete", role: .destructive) { recordings.delete(entry) }
            Button("common.cancel", role: .cancel) {}
        } message: { Text("calls.deleteRecordingExplanation") }
    }

    private var information: some View {
        VStack(alignment: .leading, spacing: 4) {
            PhoneNumberText(number: entry.number, regionCode: entry.regionCode).font(.headline)
                .accessibilityIdentifier("recording-number-\(entry.id)")
            Text(entry.line).font(.subheadline).foregroundStyle(.secondary)
            Text(entry.createdAt, format: .dateTime).font(.caption).foregroundStyle(.secondary)
            if entry.receiveOnly {
                Label("calls.recordingReceiveOnly", systemImage: "mic.slash")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var recordingContent: some View {
        if entry.complete {
            CallRecordingProgress(entry: entry, player: player)
        } else if recordings.activeID == entry.id {
            Text("calls.recording").font(.caption)
        } else if recordings.savingID == entry.id || recordings.recoveringID == entry.id {
            ProgressView("calls.savingRecording").accessibilityIdentifier("recording-saving")
        } else {
            Text("calls.recordingPending").font(.caption)
            AsyncActionButton(title: "calls.recoverRecording", minimumLabelHeight: nil) {
                await recordings.recover(entry.id)
            }
            .disabled(recordings.busy || recordings.activeID != nil)
        }
    }

    private var playbackButton: some View {
        Button {
            if playing {
                player.pause()
            } else {
                player.play(entry, url: recordings.url(for: entry))
            }
        } label: {
            Group {
                if player.selectedID == entry.id, player.state == .preparing {
                    ProgressView()
                } else {
                    Image(systemName: playing ? "pause.fill" : "play.fill")
                        .font(.system(size: min(playbackSymbolSize, 32)))
                }
            }.frame(width: 44, height: 44)
        }
        .buttonStyle(.glass)
        .buttonBorderShape(.circle)
        .accessibilityLabel(Text(playbackTitle))
        .accessibilityIdentifier("recording-toggle-\(entry.id)")
        .disabled(!player.allowed || (player.selectedID == entry.id && player.state == .preparing))
    }
}

private struct CallRecordingProgress: View {
    let entry: CallRecording
    let player: CallRecordingPlayer
    @State private var scrubTime: Double?

    private var selected: Bool { player.selectedID == entry.id }
    private var duration: Double { max(0, selected ? player.duration : entry.duration) }
    private var time: Double {
        guard selected else { return 0 }
        return min(duration, max(0, scrubTime ?? player.position))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Slider(value: position, in: 0...max(duration, 0.001), onEditingChanged: scrub)
                .accessibilityLabel(Text("calls.playbackPosition"))
                .accessibilityValue(Text("\(timestamp(time)) / \(timestamp(duration))"))
                .accessibilityIdentifier("recording-progress-\(entry.id)")
                .disabled(!selected || !player.allowed || player.state == .preparing)
            HStack {
                Text(timestamp(time)).accessibilityIdentifier("recording-elapsed-\(entry.id)")
                Spacer()
                Text(timestamp(duration)).accessibilityIdentifier("recording-duration-\(entry.id)")
            }.font(.caption).monospacedDigit().foregroundStyle(.secondary)
        }
        .onChange(of: selected) { _, _ in scrubTime = nil }
    }

    private var position: Binding<Double> {
        Binding(get: { time }, set: { value in
            guard selected else { return }
            if scrubTime != nil {
                scrubTime = value
            } else {
                // VoiceOver adjustments do not always send editing callbacks.
                player.seek(to: value)
            }
        })
    }

    private func scrub(_ editing: Bool) {
        guard selected else {
            scrubTime = nil
            return
        }
        if editing {
            scrubTime = player.position
            return
        }
        guard let target = scrubTime else { return }
        scrubTime = nil
        player.seek(to: target)
    }

    private func timestamp(_ value: Double) -> String {
        Duration.seconds(max(0, value)).formatted(.time(pattern: .minuteSecond))
    }
}

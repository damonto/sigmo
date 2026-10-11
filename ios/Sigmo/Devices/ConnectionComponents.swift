import SwiftUI

struct ConnectionAction<Value>: Identifiable {
    let id = UUID()
    let value: Value
}

struct ConnectionLifecycle: ViewModifier {
    let scope: ProfileConnectionScope
    var refreshID = 0
    let reload: @MainActor () async -> Void
    let cancelAction: @MainActor () -> Void
    @Environment(\.dismiss) private var dismiss

    private struct Identity: Equatable {
        let available: Bool
        let refresh: Int
    }

    func body(content: Content) -> some View {
        content
            .environment(\.defaultMinListRowHeight, 44)
            .navigationBarTitleDisplayMode(.inline)
            .disabled(!scope.available)
            .foregroundTask(id: Identity(available: scope.available, refresh: refreshID)) {
                guard scope.available else { return }
                await reload()
            }
            .onChange(of: scope.available) { _, available in
                if !available { cancelAction(); dismiss() }
            }
            .onDisappear { cancelAction() }
    }
}

struct ConnectionReadFailure: View {
    let failure: BackendFailure?
    let retry: () -> Void

    var body: some View {
        if let failure {
            Section {
                Text(failure.message).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                RetryButton { retry() }
                    .accessibilityIdentifier("retryConnectionRead")
            }
        }
    }
}

struct ConnectionRadioNotice: View {
    let scope: ProfileConnectionScope

    var body: some View {
        if scope.detail.modem?.airplaneMode == true {
            Section { Label("connection.airplaneHint", systemImage: "airplane") }
        }
    }
}

struct ConnectionProgress: View {
    var title = "connection.updating"
    var body: some View {
        HStack(spacing: 12) {
            ProgressView()
            Text(detailText(title)).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

struct ConnectionInitialValue: View {
    var title = "connection.status"
    let loading: Bool

    var body: some View {
        if loading { ConnectionProgress(title: "connection.loading") }
        else { DetailValue(title: title, value: detailText("common.unknown")) }
    }
}

enum ConnectionFormat {
    static func enabled(_ value: Bool?) -> String {
        guard let value else { return detailText("common.unknown") }
        return detailText(value ? "connection.on" : "connection.off")
    }

    static func bytes(_ value: UInt64?) -> String? {
        value.map { ByteCountFormatter.string(fromByteCount: Int64(min($0, UInt64(Int64.max))), countStyle: .binary) }
    }

    static func duration(_ seconds: Int64) -> String {
        let seconds = max(0, seconds)
        return String(format: "%lld:%02lld:%02lld", seconds / 3600, seconds % 3600 / 60, seconds % 60)
    }
}

struct ConnectionDurationRow: View {
    let seconds: Int64?
    let observedAt: Date
    let running: Bool

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { timeline in
            let elapsed = running ? Int64(max(0, timeline.date.timeIntervalSince(observedAt))) : 0
            DetailValue(title: "connection.duration", value: seconds.map { ConnectionFormat.duration($0 + elapsed) })
        }
    }
}

struct ConnectionSecretValue: View {
    let title: String
    let value: String?
    @State private var revealed = false

    var body: some View {
        LabeledContent {
            if let value, !value.isEmpty {
                HStack {
                    Text(revealed ? value : "••••••••")
                        .fixedSize(horizontal: false, vertical: true)
                    Button { revealed.toggle() } label: {
                        Image(systemName: revealed ? "eye.slash" : "eye")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text(revealed ? "connection.hidePassword" : "connection.showPassword"))
                }
                .contextMenu {
                    Button("detail.copy", systemImage: "document.on.document") { UIPasteboard.general.string = value }
                }
            } else { Text("—") }
        } label: { Text(detailText(title)) }
    }
}

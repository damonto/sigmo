import SwiftUI

struct LineConnectionSection: View {
    let state: ModemDetailState
    let profileID: String?

    var body: some View {
        Section {
            NavigationLink {
                NetworkSelectionView(detail: state, profileID: profileID)
            } label: {
                LabeledContent("connection.networkSelection", value: state.modem?.networkSelectionName ?? detailText("common.unknown"))
            }
            .accessibilityIdentifier("profileNetworkSelection")
            NavigationLink {
                InternetView(detail: state, profileID: profileID)
            } label: {
                LabeledContent("connection.internet", value: state.modem?.internetConnected.map {
                    detailText($0 ? "common.connected" : "common.disconnected")
                } ?? detailText("common.unknown"))
            }
            .accessibilityIdentifier("profileInternet")
            if state.canUseWiFi {
                NavigationLink {
                    WiFiCallingView(detail: state, profileID: profileID)
                } label: {
                    LabeledContent("capability.wifiCalling", value: ConnectionFormat.enabled(state.modem?.wifiCallingEnabled))
                }
                .accessibilityIdentifier("profileWiFiCalling")
            }
            if state.session.capabilities?.contains("volte") == true {
                NavigationLink {
                    VoLTEView(detail: state, profileID: profileID)
                } label: {
                    LabeledContent("capability.volte", value: ConnectionFormat.enabled(state.modem?.volteEnabled))
                }
                .accessibilityIdentifier("profileVoLTE")
            }
        }
        .disabled(state.needsPIN)
    }
}

struct ConnectionAction<Value>: Identifiable {
    let id = UUID()
    let value: Value
}

struct ConnectionLifecycle: ViewModifier {
    let scope: ProfileConnectionScope
    var refreshID = 0
    let reload: @MainActor () async -> Void
    let cancelAction: @MainActor () -> Void
    var observe: (@MainActor () async -> Void)? = nil
    @Environment(\.dismiss) private var dismiss

    private struct Identity: Equatable {
        let available: Bool
        let refresh: Int
    }

    private struct ObservationIdentity: Equatable {
        let available: Bool
        let foreground: Bool
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
            .task(id: ObservationIdentity(available: scope.available, foreground: scope.detail.session.foreground)) {
                guard scope.available, scope.detail.session.foreground else { return }
                await observe?()
            }
            .onChange(of: scope.available) { _, available in
                if !available { cancelAction(); dismiss() }
            }
            .onDisappear { cancelAction() }
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

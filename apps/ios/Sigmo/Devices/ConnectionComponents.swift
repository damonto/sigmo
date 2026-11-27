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
            .contentMargins(.top, 8, for: .scrollContent)
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

struct ConnectionDuration<Content: View>: View {
    let seconds: Int64?
    let observedAt: Date
    @ViewBuilder var content: (String?) -> Content

    var body: some View {
        if let seconds {
            TimelineView(.periodic(from: .now, by: 1)) { timeline in
                let elapsed = Int64(max(0, timeline.date.timeIntervalSince(observedAt)))
                let (duration, overflow) = seconds.addingReportingOverflow(elapsed)
                content(ConnectionFormat.duration(overflow ? .max : duration))
            }
        } else {
            content(nil)
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

struct ConnectionCardHeader<Title: View>: View {
    let status: ConnectionStatus
    let statusIdentifier: String
    @ViewBuilder var title: () -> Title

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                title()
                Spacer(minLength: 0)
                summary
            }
            VStack(alignment: .leading, spacing: 4) {
                title()
                summary
            }
        }
    }

    @ViewBuilder private var summary: some View {
        if status == .loading {
            ProgressView()
                .controlSize(.small)
                .accessibilityLabel(Text("connection.loading"))
        } else {
            ConnectionStatusBadge(status: status)
                .accessibilityIdentifier(statusIdentifier)
        }
    }
}

private struct ConnectionStatusBadge: View {
    let status: ConnectionStatus

    private var tint: Color {
        switch status {
        case .connected, .modemManaged: .green
        case .connecting, .waitingForUplink, .setupRequired: .orange
        case .failed: .red
        default: .secondary
        }
    }

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "circle.fill")
                .imageScale(.small)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(LocalizedStringKey(status.localizationKey))
                .fixedSize(horizontal: false, vertical: true)
        }
        .font(.caption.weight(.semibold))
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(tint.opacity(0.12), in: Capsule())
    }
}

struct IMSConnectionStatusCard: View {
    let access: IMSConnectionProgressNodes.Access
    let status: ConnectionStatus
    let enabled: Bool
    let progress: IMSConnectionProgress?
    let seconds: Int64?
    let observedAt: Date
    let detail: String?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ConnectionCardHeader(status: status,
                                 statusIdentifier: access == .wifiCalling ? "wifiCallingStatus" : "volteStatus") {
                heading
            }
            if enabled {
                IMSConnectionProgressNodes(access: access, connected: status == .connected, progress: progress)
                Divider()
                    .accessibilityHidden(true)
            }
            if status != .loading {
                let layout = dynamicTypeSize >= .xxxLarge
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                    : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
                layout {
                    ConnectionDuration(seconds: status == .connected ? seconds : nil, observedAt: observedAt) { value in
                        metric("connection.duration", value: value, icon: "clock")
                            .monospacedDigit()
                    }
                    switch access {
                    case .wifiCalling: metric("connection.uplink", value: detail, icon: "network")
                    case .volte: metric("connection.dataPath", value: detail, icon: "arrow.triangle.branch")
                    }
                }
            }
        }
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
    }

    private var heading: some View {
        HStack(spacing: 6) {
            IMSServiceIcon(service: access == .wifiCalling ? .wiFiCalling : .voLTE)
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text("connection.status")
                .accessibilityAddTraits(.isHeader)
        }
        .font(.headline)
    }

    private func metric(_ title: LocalizedStringKey, value: String?, icon: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 4) {
                if !dynamicTypeSize.isAccessibilitySize {
                    Image(systemName: icon)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                }
                Text(title)
                    .lineLimit(nil)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            Text(value ?? "—")
                .font(.headline)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(title))
        .accessibilityValue(value.map { Text(verbatim: $0) } ?? Text("common.unknown"))
    }
}

struct IMSConnectionProgressNodes: View {
    enum Access {
        case wifiCalling, volte
    }

    private struct Milestone {
        let label: String
        let stages: [String]

        init(_ label: String, _ stages: [String]) {
            self.label = label
            self.stages = stages
        }
    }

    private enum Status: String {
        case pending, current, completed, failed
    }

    private struct Node {
        let label: String
        let status: Status
    }

    let access: Access
    let connected: Bool
    let progress: IMSConnectionProgress?
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let nodes = nodes
        VStack(alignment: .leading, spacing: 5) {
            let current = nodes.first { $0.status == .current || $0.status == .failed }
            ViewThatFits(in: .horizontal) {
                HStack {
                    Text("connection.imsProgress.label")
                    Spacer(minLength: 8)
                    if let current { stepSummary(current) }
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text("connection.imsProgress.label")
                    if let current { stepSummary(current) }
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .accessibilityHidden(true)
            nodeList(nodes)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Text("connection.imsProgress.label"))
    }

    private func stepSummary(_ node: Node) -> some View {
        HStack(spacing: 4) {
            Text(verbatim: node.label)
            Text(LocalizedStringKey("connection.imsProgress." + node.status.rawValue))
        }
        .fontWeight(.semibold)
        .foregroundStyle(node.status == .failed ? Color.red : Color.accentColor)
    }

    private func nodeList(_ nodes: [Node]) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                ForEach(nodes.indices, id: \.self) { index in
                    let node = nodes[index]
                    HStack(spacing: 4) {
                        nodeLabel(node).fixedSize(horizontal: true, vertical: false)
                        if index + 1 < nodes.count {
                            let reached = node.status == .completed && nodes[index + 1].status != .pending
                            Rectangle()
                                .fill(reached ? completedColor.opacity(0.46) : .secondary.opacity(0.24))
                                .frame(minWidth: 4, maxWidth: .infinity)
                                .frame(height: 1)
                                .accessibilityHidden(true)
                        }
                    }
                }
            }
            VStack(alignment: .leading, spacing: 4) {
                ForEach(nodes.indices, id: \.self) { index in
                    nodeLabel(nodes[index])
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func nodeLabel(_ node: Node) -> some View {
        HStack(spacing: 4) {
            marker(node.status)
                .frame(width: 12, height: 12)
                .accessibilityHidden(true)
            Text(verbatim: node.label)
                .font(.caption2.weight(.medium))
                .foregroundStyle(node.status == .pending ? .secondary : .primary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: node.label))
        .accessibilityValue(Text(LocalizedStringKey("connection.imsProgress." + node.status.rawValue)))
        .accessibilityIdentifier("imsProgressNode-" + node.label)
    }

    private var completedColor: Color {
        colorScheme == .dark ? .green : Color(red: 0, green: 0.48, blue: 0.32)
    }

    private func marker(_ status: Status) -> some View {
        ZStack {
            switch status {
            case .pending:
                Circle().fill(.background)
                Circle().strokeBorder(.secondary.opacity(0.35))
            case .current:
                Circle().fill(.background)
                Circle().strokeBorder(Color.accentColor)
                Circle().fill(Color.accentColor).frame(width: 6, height: 6)
            case .completed:
                Circle().fill(completedColor)
                Image(systemName: "checkmark")
                    .foregroundStyle(colorScheme == .dark ? .black : .white)
            case .failed:
                Circle().fill(.red)
                Image(systemName: "xmark").foregroundStyle(.white)
            }
        }
        .font(.system(size: 7, weight: .bold))
    }

    private var milestones: [Milestone] {
        switch access {
        case .wifiCalling:
            [
                Milestone("ePDG", ["epdg_resolve", "epdg_ike_init"]),
                Milestone("AKA", ["epdg_eap_aka"]),
                Milestone("IPsec", ["epdg_ipsec"]),
                Milestone("IMS", ["ims_transport", "ims_registration_slot", "ims_register_challenge", "ims_aka",
                                  "ims_ipsec", "ims_register_final", "ims_starting", "ims_disconnected"])
            ]
        case .volte:
            [
                Milestone("LTE", ["lte_packet_service", "volte_data_path"]),
                Milestone("IMS PDN", ["ims_pdn"]),
                Milestone("AKA", ["ims_transport", "ims_registration_slot", "ims_register_challenge", "ims_aka"]),
                Milestone("IMS", ["ims_ipsec", "ims_register_final", "ims_starting", "ims_disconnected"])
            ]
        }
    }

    private var nodes: [Node] {
        let milestones = milestones
        let ready = progress.map { $0.status == "ready" } ?? connected
        let active = activeNode(in: milestones)
        return milestones.enumerated().map { index, milestone in
            let status: Status
            if ready || index < active.index {
                status = .completed
            } else {
                status = index == active.index ? active.status : .pending
            }
            return Node(label: milestone.label, status: status)
        }
    }

    private func activeNode(in milestones: [Milestone]) -> (index: Int, status: Status) {
        guard let progress else { return (-1, .pending) }
        func index(for stage: String) -> Int? {
            milestones.firstIndex { $0.stages.contains(stage) }
        }
        if let current = index(for: progress.stage) {
            return (current, progress.failed ? .failed : .current)
        }
        // A wait keeps the last node reached in this attempt.
        for step in progress.steps.reversed() {
            guard let previous = index(for: step.stage) else { continue }
            switch step.status {
            case "failed": return (previous, .failed)
            case "completed": return (previous, .completed)
            default: return (previous, .current)
            }
        }
        return (-1, .pending)
    }
}

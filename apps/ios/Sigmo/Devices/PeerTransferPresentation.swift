import SwiftUI

// Each backend owns its subscription; this presenter serializes approval sheets
// across backends and advances only after SwiftUI finishes dismissing a sheet.
struct PeerTransferPresentation: ViewModifier {
    private struct Presentation: Identifiable {
        let session: BackendSession
        let request: PeerTransferRequest
        var id: String { "\(session.incarnationID)-\(request.id)-\(request.revision)-\(request.phase)" }
    }
    @Environment(BackendStore.self) private var store
    @State private var presented: Presentation?
    @State private var sheetOpen = false
    @State private var dismissing = false

    private var next: Presentation? {
        let sessions = store.orderedSessions()
        if let presented, sessions.contains(where: { $0 === presented.session }),
           presented.session.transferRequests.requests.contains(where: {
               $0.id == presented.request.id && $0.revision == presented.request.revision && $0.needsDecision
           }) { return presented }
        for session in sessions {
            if let request = session.transferRequests.current, request.needsDecision {
                return Presentation(session: session, request: request)
            }
        }
        for session in sessions {
            if let request = session.transferRequests.current {
                return Presentation(session: session, request: request)
            }
        }
        return nil
    }

    func body(content: Content) -> some View {
        content.background {
            ForEach(store.orderedSessions(), id: \.incarnationID) { session in
                PeerTransferRequestObserver(session: session)
            }
        }
        .onChange(of: next?.id, initial: true) { _, _ in reconcile() }
        .onChange(of: presented?.session.transferRequests.sending) { _, _ in reconcile() }
        .onChange(of: presented?.session.canAccessDevices) { _, _ in reconcile() }
        .sheet(isPresented: $sheetOpen, onDismiss: didDismiss) {
            if let presented {
                PeerTransferRequestSheet(session: presented.session, request: presented.request)
                    .interactiveDismissDisabled()
            }
        }
    }

    private func reconcile() {
        guard !dismissing else { return }
        if presented?.session.transferRequests.sending == true,
           presented?.session.canAccessDevices == true { return }
        if presented?.id == next?.id { return }
        if presented != nil {
            dismissing = true
            sheetOpen = false
            return
        }
        presented = next
        sheetOpen = next != nil
    }

    private func didDismiss() {
        presented = nil
        dismissing = false
        reconcile()
    }
}

private struct PeerTransferRequestSheet: View {
    private struct Appearance {
        let title: LocalizedStringKey
        let message: LocalizedStringKey
        let badge: LocalizedStringKey
        let symbol: String
    }

    let session: BackendSession
    let request: PeerTransferRequest
    @Environment(\.colorScheme) private var colorScheme
    @State private var sourceKey = ""
    @State private var profileID = ""
    @State private var sourceIMEI = ""
    private var sources: [PeerTransferSourceOption] { request.sources ?? [] }
    private var chosenSource: PeerTransferSourceOption? { sources.first { $0.source.key == sourceKey } }
    private var profiles: [TransferProfile] { chosenSource?.profiles.filter { $0.supported && $0.type == "esim" } ?? [] }
    private var chosenProfile: TransferProfile? { profiles.first { $0.id == profileID } }
    private var displayProfile: TransferProfile? { request.profile ?? chosenProfile }
    private var sourceName: String? { request.sourceName ?? chosenSource?.source.name }
    private var selection: PeerTransferSelection? {
        guard request.needsDecision, let source = chosenSource?.source, let profile = chosenProfile else { return nil }
        if source.requiresSourceImei && (sourceIMEI.utf8.count != 15 || !sourceIMEI.utf8.allSatisfy({ (48...57).contains($0) })) { return nil }
        return PeerTransferSelection(sourceType: source.type, sourceId: source.id, profileId: profile.id,
                                     sourceImei: source.requiresSourceImei ? sourceIMEI : nil)
    }
    private var state: PeerTransferRequestsState { session.transferRequests }
    private var running: Bool { request.phase == "running" }
    private var ended: Bool { request.phase == "ended" }
    private var completed: Bool { ended && request.outcome == "completed" }
    private var uncertain: Bool { ended && request.outcome == "uncertain" }

    private var tone: Color {
        if completed { return .green }
        if uncertain { return .orange }
        if ended { return .secondary }
        return .accentColor
    }
    private var appearance: Appearance {
        switch request.phase {
        case "authorization":
            Appearance(title: "peer.sourceSelectionTitle", message: "peer.sourceSelectionHint",
                       badge: "peer.requestBadge", symbol: "checkmark.shield")
        case "running":
            Appearance(title: "peer.runningTitle", message: "peer.running",
                       badge: "peer.runningBadge", symbol: "arrow.left.arrow.right")
        default:
            switch request.outcome {
            case "completed":
                Appearance(title: "peer.completedTitle", message: "peer.completed",
                           badge: "peer.completedBadge", symbol: "checkmark")
            case "uncertain":
                Appearance(title: "peer.uncertainTitle", message: "peer.uncertainDescription",
                           badge: "peer.uncertainBadge", symbol: "questionmark.circle")
            default:
                Appearance(title: "peer.endedTitle", message: "peer.failed",
                           badge: "peer.endedBadge", symbol: "xmark.circle")
            }
        }
    }
    private var footerHint: LocalizedStringKey {
        if request.needsDecision { return "peer.transferSessionHint" }
        return "peer.minimizeHint"
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                header
                heading
                if request.needsDecision { sourceSelection }
                profileAndDevices
                if request.needsDecision || uncertain { warningNotice }
                if running { progress }
                if ended { sessionEnded }
                requestDetails
                if request.needsDecision || running {
                    Label(footerHint, systemImage: "link")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let failure = state.failure {
                    Text(failure.message)
                        .font(.subheadline)
                        .foregroundStyle(.red)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("peerTransferFailure")
                }
            }
            .padding(.horizontal, 24)
            .padding(.top, 28)
            .padding(.bottom, 12)
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity, alignment: .center)
        }
        .scrollDismissesKeyboard(.interactively)
        .onChange(of: sourceKey) { _, _ in profileID = ""; sourceIMEI = "" }
        .accessibilityIdentifier("peerTransferRequest")
        .safeAreaInset(edge: .bottom, spacing: 0) { actions }
        .background(Color(uiColor: .systemBackground))
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .presentationCornerRadius(32)
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 16) {
            HStack(spacing: 7) {
                Image(systemName: "antenna.radiowaves.left.and.right")
                    .foregroundStyle(Color.accentColor)
                    .accessibilityHidden(true)
                Text(session.configuration.name)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .font(.caption.weight(.semibold))
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            Label(appearance.badge, systemImage: appearance.symbol)
                .font(.caption)
                .foregroundStyle(tone)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var heading: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(appearance.title)
                .font(.title.bold())
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("peerTransferHeading")
            Text(appearance.message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var profileAndDevices: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let profile = displayProfile {
                HStack(spacing: 13) {
                    if profile.icon != nil || profile.regionCode != nil {
                        ProfileAvatar(icon: profile.icon, region: profile.regionCode)
                            .accessibilityHidden(true)
                    } else {
                        Image(systemName: "simcard")
                            .font(.title2)
                            .foregroundStyle(Color.accentColor)
                            .frame(width: 43, height: 52)
                            .background(Color.accentColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
                            .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(Color.accentColor.opacity(0.2)) }
                            .accessibilityHidden(true)
                    }
                    VStack(alignment: .leading, spacing: 5) {
                        Text(profile.name)
                            .font(.title3.weight(.semibold))
                            .fixedSize(horizontal: false, vertical: true)
                            .accessibilityIdentifier("peerTransferProfile")
                        Text(identifierTail("ICCID", profile.iccid))
                            .font(.caption)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(16)
                Divider()
            }
            VStack(alignment: .leading, spacing: 8) {
                if let source = sourceName {
                    PeerTransferDeviceRow(title: "peer.fromService", name: source,
                                          symbol: "externaldrive", tint: .secondary)
                    Rectangle().fill(Color(uiColor: .separator).opacity(0.5))
                        .frame(width: 1, height: 12).padding(.leading, 14)
                        .accessibilityHidden(true)
                }
                PeerTransferDeviceRow(
                    title: "peer.target", name: request.target.name,
                    symbol: "arrow.down.to.line", tint: .accentColor,
                    detail: identifierTail("IMEI", request.target.imei) + " · " + identifierTail("EID", request.target.eid)
                )
            }
            .padding(16)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemBackground).opacity(0.5), in: RoundedRectangle(cornerRadius: 18))
        .overlay { RoundedRectangle(cornerRadius: 18).strokeBorder(Color(uiColor: .separator).opacity(0.25)) }
    }

    private var sourceSelection: some View {
        VStack(alignment: .leading, spacing: 0) {
            Menu {
                Picker("peer.sourceDevice", selection: $sourceKey) {
                    Text("peer.chooseSource").tag("")
                    ForEach(sources, id: \.source.key) { entry in
                        Text(entry.source.name).tag(entry.source.key)
                    }
                }
                .pickerStyle(.inline)
            } label: {
                selectionRow("peer.sourceDevice", value: chosenSource?.source.name, placeholder: "peer.chooseSource")
            }
            .accessibilityLabel(Text("peer.sourceDevice"))
            .accessibilityValue(chosenSource?.source.name ?? detailText("peer.chooseSource"))
            .accessibilityIdentifier("peerTransferSource")
            .disabled(state.sending || sources.isEmpty)

            Divider().padding(.horizontal, 16)

            Menu {
                Picker("peer.sourceProfile", selection: $profileID) {
                    Text("peer.chooseProfile").tag("")
                    ForEach(profiles) { profile in
                        Text(profile.name + " · " + identifierTail("ICCID", profile.iccid)).tag(profile.id)
                    }
                }
                .pickerStyle(.inline)
            } label: {
                selectionRow("peer.sourceProfile", value: chosenProfile?.name, placeholder: "peer.chooseProfile")
            }
            .accessibilityLabel(Text("peer.sourceProfile"))
            .accessibilityValue(chosenProfile?.name ?? detailText("peer.chooseProfile"))
            .accessibilityIdentifier("peerTransferProfilePicker")
            .disabled(state.sending || chosenSource == nil)

            if chosenSource?.source.requiresSourceImei == true {
                Divider().padding(.horizontal, 16)
                VStack(alignment: .leading, spacing: 6) {
                    Text("detail.sourceIMEI").font(.caption).foregroundStyle(.secondary)
                    TextField("detail.sourceIMEI", text: $sourceIMEI)
                        .keyboardType(.numberPad)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityIdentifier("peerTransferSourceIMEI")
                        .disabled(state.sending)
                }
                .padding(16)
            }
            if sources.isEmpty {
                Text("peer.noSourceOptions")
                    .font(.subheadline).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(16)
            }
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 16))
    }

    private func selectionRow(_ title: LocalizedStringKey, value: String?, placeholder: LocalizedStringKey) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.caption).foregroundStyle(.secondary)
                Group {
                    if let value { Text(value).foregroundStyle(.primary) }
                    else { Text(placeholder).foregroundStyle(.secondary) }
                }
                .font(.body)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "chevron.up.chevron.down")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
    }

    private var warningNotice: some View {
        HStack(alignment: .top, spacing: 9) {
            Image(systemName: "exclamationmark.triangle")
                .font(.subheadline).foregroundStyle(.orange)
                .frame(width: 17).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(uncertain ? "peer.checkDevicesTitle" : "peer.sourceInvalidTitle")
                    .font(.subheadline.weight(.semibold))
                Text(uncertain ? "peer.uncertain" : "peer.deletionConsent")
                    .font(.caption).foregroundStyle(.secondary)
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.orange.opacity(colorScheme == .dark ? 0.1 : 0.07), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityIdentifier("peerTransferWarning")
    }

    private var progress: some View {
        VStack(alignment: .leading, spacing: 12) {
            progressStep(title: "peer.approvalsComplete", message: "peer.approvedProfile",
                         symbol: "checkmark.circle", tint: .green)
            progressStep(title: "peer.runningTitle", message: "peer.keepConnected",
                         symbol: "circle.inset.filled", tint: .accentColor)
            progressStep(title: "peer.endAfterTransfer", message: nil,
                         symbol: "circle", tint: .secondary)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("peerTransferProgress")
    }

    private func progressStep(title: LocalizedStringKey, message: LocalizedStringKey?, symbol: String, tint: Color) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: symbol).foregroundStyle(tint)
                .font(.subheadline).frame(width: 18).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.subheadline.weight(.medium))
                if let message { Text(message).font(.caption).foregroundStyle(.secondary) }
            }
            .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var sessionEnded: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "lock.slash")
                .font(.subheadline).foregroundStyle(.secondary)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text("peer.endedTitle").font(.subheadline.weight(.medium))
                Text(uncertain ? "peer.noReplay" : "peer.ended")
                    .font(.caption).foregroundStyle(.secondary)
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    private var requestDetails: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label {
                Text(verbatim: detailText("peer.requestFrom") + " " + request.remoteAddress)
            } icon: {
                Image(systemName: "network").accessibilityHidden(true)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
            DisclosureGroup {
                VStack(alignment: .leading, spacing: 12) {
                    identifier("IMEI", value: request.target.imei)
                    identifier("EID", value: request.target.eid)
                    if let profile = displayProfile { identifier("ICCID", value: profile.iccid) }
                }
                .padding(13)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
                .padding(.top, 8)
            } label: {
                Text("peer.deviceDetails")
                    .frame(minHeight: 44, alignment: .leading)
            }
            .font(.caption)
            .tint(.secondary)
            .foregroundStyle(.secondary)
            .accessibilityIdentifier("peerTransferDetails")
        }
    }

    private func identifier(_ title: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: title).font(.caption).foregroundStyle(.secondary)
            Text(value).font(.caption.monospaced()).foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
                .accessibilityIdentifier("peerTransfer" + title)
        }
    }

    private func identifierTail(_ identifier: String, _ value: String) -> String {
        String.localizedStringWithFormat(detailText("peer.identifierTail"), identifier, String(value.suffix(4)))
    }

    private var actions: some View {
        VStack(spacing: 12) {
            if request.needsDecision {
                AsyncActionButton(id: "allowPeerTransfer", contentMaxWidth: .infinity, action: {
                    await state.decide(session: session, request: request, accept: true, selection: selection)
                }) {
                    Text("peer.allowTransfer")
                }
                .buttonStyle(.glassProminent)
                .disabled(state.sending || selection == nil)
                AsyncActionButton(id: "rejectPeerTransfer", contentMaxWidth: .infinity, action: {
                    await state.decide(session: session, request: request, accept: false)
                }) {
                    Text("peer.reject")
                }
                .buttonStyle(.glass)
                .disabled(state.sending)
            } else {
                Button { state.dismiss(request.id) } label: {
                    Text(running ? "peer.minimize" : uncertain ? "peer.acknowledge" : "common.done")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.glassProminent)
                .accessibilityIdentifier("closePeerTransferRequest")
            }
        }
        .font(.body.weight(.semibold))
        .controlSize(.large)
        .buttonBorderShape(.capsule)
        .padding(.horizontal, 38)
        .padding(.top, 16)
        .padding(.bottom, 8)
        .frame(maxWidth: 560)
        .frame(maxWidth: .infinity)
        .background(Color(uiColor: .systemBackground))
    }
}

private struct PeerTransferDeviceRow: View {
    let title: LocalizedStringKey
    let name: String
    let symbol: String
    let tint: Color
    var detail: String?

    var body: some View {
        HStack(alignment: .top, spacing: 11) {
            Image(systemName: symbol)
                .font(.subheadline)
                .foregroundStyle(tint)
                .frame(width: 28, height: 28)
                .background(tint.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 5) {
                Text(title).font(.caption).foregroundStyle(.secondary)
                Text(name).font(.subheadline.weight(.medium))
                if let detail {
                    Text(detail).font(.caption).monospacedDigit().foregroundStyle(.secondary)
                }
            }
            .fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct PeerTransferRequestObserver: View {
    let session: BackendSession
    private var identity: String {
        "\(session.accessRevision)-\(session.foreground)-\(session.canAccessDevices)-\(session.capabilities?.contains("esimTransfer") == true)"
    }
    var body: some View {
        Color.clear.frame(width: 0, height: 0)
            .task(id: identity) {
                guard session.foreground, session.canAccessDevices, session.capabilities?.contains("esimTransfer") == true, !session.isDemo else { return }
                await session.transferRequests.run(session: session)
            }
    }
}

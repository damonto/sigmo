import SwiftUI

struct ESIMFlowView: View {
    @Bindable var state: ESIMFlowState
    @ScaledMetric(relativeTo: .title3) private var methodIconWidth = 28.0
    private enum SetupPage { case methods, manual, scanner, discoveries }
    @State private var setupPath: [SetupPage]
    private var setupPage: SetupPage { setupPath.last ?? .methods }
    @State private var selectedDiscovery: ESIMDiscovery?
    private enum ManualField { case address, activation, confirmation }
    @FocusState private var manualField: ManualField?
    @State private var presentedFailure: BackendFailure?
    @State private var closing = false
    @State private var sourceDeletionPresented = false
    private struct SourceDeletionReply: Identifiable {
        let id = UUID()
        let accepted: Bool
    }

    @State private var sourceDeletionReply: SourceDeletionReply?
    private struct PendingCallback: Identifiable {
        let id = UUID()
        let value: CarrierCallback
    }

    @State private var callback: PendingCallback?
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    init(state: ESIMFlowState, showDiscoveries: Bool = false) {
        self.state = state
        _setupPath = State(initialValue: showDiscoveries ? [.discoveries] : [])
        _selectedDiscovery = State(initialValue: showDiscoveries && state.discoveries?.count == 1 ? state.discoveries?.first : nil)
    }

    var body: some View {
        page
        .disabled(closing)
        .background(Color(uiColor: .systemGroupedBackground))
        .overlay(alignment: .top) {
            if state.phase != .preview && state.phase != .completed {
                navigationControls
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if state.detail.session.isDemo {
                Text("demo.simulatedSIM").font(.footnote).foregroundStyle(.secondary)
                    .padding(8).frame(maxWidth: .infinity).background(.bar)
            }
        }
        .interactiveDismissDisabled()
        .detailErrorAlert($presentedFailure)
        .alert("detail.sourceDeletion", isPresented: $sourceDeletionPresented) {
            Button("common.delete", role: .destructive) { sourceDeletionReply = SourceDeletionReply(accepted: true) }
            Button("common.cancel", role: .cancel) { sourceDeletionReply = SourceDeletionReply(accepted: false) }
        } message: {
            Text(String(format: detailText("detail.sourceDeletionMessage"), state.deletionICCID))
        }
        .onChange(of: state.failure) { _, failure in
            if !closing { presentedFailure = failure }
        }
        .onChange(of: state.ccidError) { _, message in
            if let message { presentedFailure = BackendFailure(code: "ccid", message: message) }
        }
        .onChange(of: state.phase) { _, phase in sourceDeletionPresented = phase == .sourceDeletion }
        .task(id: sourceDeletionReply?.id) {
            guard let sourceDeletionReply else { return }
            await state.send(.init(type: "source_deletion", accept: sourceDeletionReply.accepted))
            guard self.sourceDeletionReply?.id == sourceDeletionReply.id, !Task.isCancelled else { return }
            self.sourceDeletionReply = nil
        }
        .task(id: state.runID) { await state.run() }
        .task(id: closing) {
            guard closing else { return }
            await state.cancel()
            guard !Task.isCancelled else { return }
            dismiss()
        }
        .task { presentedFailure = state.failure }
        .task(id: callback?.id) {
            guard let callback else { return }
            await state.completeWebsheet(callback.value)
            guard self.callback?.id == callback.id, !Task.isCancelled else { return }
            self.callback = nil
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background {
                if setupPage == .scanner { showManualFromScanner() }
            }
        }
        .onChange(of: state.detail.session.accessRevision) { _, _ in
            closing = false
            callback = nil
            sourceDeletionReply = nil
            state.interrupt()
            dismiss()
        }
        .onDisappear {
            closing = false
            callback = nil
            sourceDeletionReply = nil
            state.interrupt()
        }
    }

    private var navigationControls: some View {
        HStack {
            if canGoBack {
                AsyncActionButton(id: "backESIMSetup", minimumLabelHeight: nil, action: { await goBack() }) {
                    Label("detail.back", systemImage: "chevron.left")
                        .font(.system(size: 17, weight: .semibold))
                        .labelStyle(.iconOnly).frame(width: 20, height: 20)
                }
                .buttonBorderShape(.circle).disabled(state.busy)
            } else {
                Button {
                    if state.phase == .completed {
                        dismiss()
                    } else if state.phase == .form {
                        // Setup has not changed the device. Its parent reloads after dismissal.
                        state.interrupt()
                        dismiss()
                    } else {
                        closing = true
                    }
                } label: {
                    Label(state.phase == .completed ? "common.dismiss" : "common.cancel", systemImage: "xmark")
                        .font(.system(size: 17, weight: .semibold))
                        .labelStyle(.iconOnly).frame(width: 20, height: 20)
                        .opacity(closing ? 0 : 1)
                        .overlay { if closing { ProgressView() } }
                }
                .buttonBorderShape(.circle).disabled(closing)
                .accessibilityIdentifier(state.phase == .completed ? "closeCompletedESIM" : "closeESIMFlow")
            }
            Spacer(minLength: 16)
            if state.phase == .form, setupPage == .manual {
                Button("detail.next") { state.startDownload() }
                    .buttonBorderShape(.capsule)
                    .disabled(!state.canStart)
                    .accessibilityIdentifier("startESIMDownload")
            }
        }
        .font(.body.weight(.semibold))
        // Overlay navigation must stay compact when page content uses accessibility sizes.
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .buttonStyle(.glassProminent).controlSize(.large)
        .tint(Color(uiColor: .tertiarySystemGroupedBackground))
        .foregroundStyle(.primary)
        .padding(.horizontal, 16).padding(.top, 16)
    }

    @ViewBuilder private var page: some View {
        switch state.phase {
        case .form:
            setup
        case .websheet:
            carrierPage
        case .connecting, .progress, .preview, .completed, .failed:
            statusPage
        case .sources:
            sourceList
        case .profiles:
            profileList
        case .confirmation, .userInput, .sourceDeletion:
            inputPage
        case .cancelled:
            ProgressView()
        }
    }

    @ViewBuilder private var carrierPage: some View {
        if let url = state.websheetURL {
            CarrierWebsheetView(
                url: url,
                callback: { callback = PendingCallback(value: $0) },
                failure: {
                    presentedFailure = $0
                    if $0.isLocalAuthenticationFailure {
                        do { try state.detail.session.removeCredentials() }
                        catch { presentedFailure = .wrapping(error) }
                    }
                },
                session: state.detail.session
            )
            .id(state.websheet?.id)
            .padding(.top, 76)
        } else {
            ProgressView()
        }
    }

    private var canGoBack: Bool {
        (state.phase == .form && !setupPath.isEmpty) || state.phase == .sources || state.phase == .profiles
    }
    private func goBack() async {
        manualField = nil
        switch state.phase {
        case .form: if !setupPath.isEmpty { setupPath.removeLast() }
        case .sources: state.backToForm()
        case .profiles: await state.loadSources()
        default: break
        }
    }
    private func showManualFromScanner() {
        if setupPath.last == .scanner { setupPath.removeLast() }
        if setupPath.last != .manual { setupPath.append(.manual) }
    }

    @ViewBuilder private var inputPage: some View {
        switch state.phase {
        case .confirmation:
            ESIMSetupPage(title: "detail.confirmationCode", description: detailText("detail.confirmationHint")) {
                ESIMSetupGroup {
                    SecureField("detail.confirmationCode", text: $state.confirmation)
                        .padding(.vertical, 16)
                        .accessibilityIdentifier("downloadConfirmation")
                }
            }.safeAreaInset(edge: .bottom, spacing: 0) { promptActions }
        case .userInput:
            if let prompt = state.prompt {
                ESIMSetupPage(title: "detail.transfer", description: prompt.text) {
                    if prompt.freeText {
                        ESIMSetupGroup {
                            TextField(prompt.freeTextHint ?? detailText("detail.response"), text: $state.response)
                                .padding(.vertical, 16)
                        }.disabled(state.sending)
                    }
                }.safeAreaInset(edge: .bottom, spacing: 0) { promptActions }
            }
        case .sourceDeletion:
            ESIMSetupPage(title: "detail.transfer", description: state.deletionICCID) { EmptyView() }
        default: EmptyView()
        }
    }

    @ViewBuilder private var promptActions: some View {
        if state.phase == .confirmation {
            AsyncActionButton(id: "confirmESIMCode", minimumLabelHeight: nil, action: {
                await state.send(.init(type: "confirmation_code", code: state.confirmation.trimmingCharacters(in: .whitespacesAndNewlines)))
            }) {
                Text("detail.confirm").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent).controlSize(.large).buttonBorderShape(.capsule)
            .disabled(state.confirmation.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || state.sending)
            .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
            .frame(maxWidth: 560).frame(maxWidth: .infinity)
        } else if state.phase == .userInput, let prompt = state.prompt {
            VStack(spacing: 12) {
                AsyncActionButton(minimumLabelHeight: nil, action: {
                    await state.send(.init(type: "user_input", accept: true, response: state.response))
                }) {
                    Text(detailText(prompt.acceptLabel ?? "detail.confirm")).frame(maxWidth: .infinity)
                }.buttonStyle(.glassProminent)
                AsyncActionButton(minimumLabelHeight: nil, action: {
                    await state.send(.init(type: "user_input", accept: false, response: ""))
                }) {
                    Text(detailText(prompt.rejectLabel ?? "common.cancel")).frame(maxWidth: .infinity)
                }
                .buttonStyle(.glass)
            }
            .controlSize(.large).buttonBorderShape(.capsule).disabled(state.sending)
            .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
            .frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
    }

    @ViewBuilder private var setup: some View {
        switch setupPage {
        case .methods: methodSelection
        case .manual: manualEntry
        case .discoveries: discoverySelection
        case .scanner:
            ESIMScannerView(
                scanned: {
                    state.input = $0
                    if state.canStart {
                        state.startDownload()
                    } else {
                        setupPath.append(.manual)
                    }
                },
                enterManually: { setupPath.append(.manual) }
            )
        }
    }

    private var methodSelection: some View {
        ESIMSetupPage(title: "detail.setupHeading", description: detailText("detail.setupDescription")) {
            ESIMSetupGroup {
                if state.detail.session.isDemo {
                    Button {
                        state.input = LPAInput(smdp: "smdp.demo.invalid", activationCode: "DEMO")
                        state.startDownload()
                    } label: { methodLabel("demo.useActivation", symbol: "play.rectangle") }
                    .accessibilityIdentifier("demoActivationCode")
                    Divider().padding(.leading, methodIconWidth + 12)
                }
                if state.detail.canTransfer {
                    AsyncActionButton(id: "transferESIM", minimumLabelHeight: nil, action: { await state.loadSources() }) {
                        methodLabel("detail.transfer", symbol: "iphone.gen3.radiowaves.left.and.right")
                    }
                    Divider().padding(.leading, methodIconWidth + 12)
                }
                Button { setupPath.append(.scanner) } label: { methodLabel("detail.scan", symbol: "qrcode") }
                    .accessibilityIdentifier("scanESIM")
            }.disabled(!state.validSE || state.busy)
        }
    }

    private var manualEntry: some View {
        ESIMSetupPage(title: "detail.enterActivationCode", description: detailText("detail.setupManualHint")) {
            ESIMSetupGroup {
                TextField("detail.smdp", text: $state.input.smdp)
                    .focused($manualField, equals: .address)
                    .submitLabel(.next).padding(.vertical, 16)
                    .accessibilityIdentifier("smdpInput")
                    .onSubmit { state.applyLPA(); manualField = .activation }
                    .onChange(of: state.input.smdp) { old, value in
                        if value.count - old.count > 1 { state.applyLPA() }
                    }
                Divider()
                TextField("detail.activationCode", text: $state.input.activationCode)
                    .focused($manualField, equals: .activation)
                    .submitLabel(.next).padding(.vertical, 16)
                    .onSubmit { manualField = .confirmation }
                    .accessibilityIdentifier("activationCodeInput")
                Divider()
                SecureField("detail.confirmationCode", text: $state.input.confirmationCode)
                    .focused($manualField, equals: .confirmation)
                    .submitLabel(.done).padding(.vertical, 16)
                    .onSubmit { state.startDownload() }
                    .accessibilityHint(Text("detail.setupConfirmationOptional"))
                    .accessibilityIdentifier("setupConfirmationInput")
            }
            if state.input.confirmationRequired { Text("detail.confirmationRequired").foregroundStyle(.secondary) }
        }
        .textInputAutocapitalization(.never).autocorrectionDisabled()
        .task { manualField = state.input.confirmationRequired ? .confirmation : .address }
    }

    private var discoverySelection: some View {
        ESIMSetupPage(title: "detail.discoveries", description: detailText("detail.selectDownload")) {
            ESIMSetupGroup {
                ForEach(state.discoveries ?? []) { discovery in
                    if discovery.id != state.discoveries?.first?.id { Divider() }
                    Button { selectedDiscovery = discovery } label: {
                        HStack(spacing: 12) {
                            Image(systemName: "simcard").foregroundStyle(.tint)
                            Text(discovery.address).frame(maxWidth: .infinity, alignment: .leading)
                            if selectedDiscovery?.id == discovery.id {
                                Image(systemName: "checkmark").foregroundStyle(.tint)
                            }
                        }.foregroundStyle(.primary).padding(.vertical, 16).contentShape(Rectangle())
                    }
                    .accessibilityIdentifier("discoveredESIM-" + discovery.id)
                    .accessibilityAddTraits(selectedDiscovery?.id == discovery.id ? .isSelected : [])
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            Button {
                guard let selectedDiscovery else { return }
                state.selectDiscovery(selectedDiscovery)
                state.startDownload()
            } label: {
                Text("detail.download").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent).controlSize(.large).buttonBorderShape(.capsule)
            .disabled(selectedDiscovery == nil || state.busy || !state.validSE)
            .accessibilityIdentifier("downloadDiscoveredESIM")
            .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
            .frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
    }

    private func methodLabel(_ title: String, symbol: String) -> some View {
        HStack(spacing: 12) {
            Image(systemName: symbol).font(.title3).foregroundStyle(.tint).frame(width: methodIconWidth)
            Text(detailText(title)).fontWeight(.semibold).foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
        }.padding(.vertical, 16).contentShape(Rectangle())
    }
    private var statusPage: some View {
        Group {
            if state.phase == .preview || state.phase == .completed {
                ESIMDownloadSummaryView(
                    profile: state.preview,
                    deviceName: state.detail.modem?.displayName ?? state.detail.modemID,
                    elementName: secureElementTitle(state.seID, elements: state.detail.elements),
                    eid: state.detail.elements.first { $0.id == state.seID }?.eid,
                    completed: state.phase == .completed
                )
                .safeAreaInset(edge: .top, spacing: 0) {
                    navigationControls.padding(.bottom, 8)
                }
            } else {
                ESIMSetupPage(symbol: statusHeading.symbol, title: statusHeading.title, description: statusHeading.description) {
                    if state.phase == .connecting || state.phase == .progress {
                        HStack(spacing: 12) {
                            ProgressView()
                            Text(stageLabel).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if state.phase == .preview || state.phase == .completed || state.phase == .failed {
                VStack(spacing: 12) {
                    if state.phase == .preview {
                        AsyncActionButton(id: "confirmESIMPreview", minimumLabelHeight: nil, action: { await state.send(.init(type: "confirm", accept: true)) }) {
                            Text("detail.confirmDownload").frame(maxWidth: .infinity)
                        }.buttonStyle(.glassProminent).disabled(state.sending)
                    } else if state.phase == .completed {
                        Button { dismiss() } label: { Text("common.done").frame(maxWidth: .infinity) }
                            .buttonStyle(.glassProminent).accessibilityIdentifier("closeESIMFlow")
                    } else {
                        Button {
                            state.backToForm(); setupPath = []; selectedDiscovery = nil
                        } label: { Text("detail.startAgain").frame(maxWidth: .infinity) }
                            .buttonStyle(.glassProminent)
                    }
                }
                .font(.body.weight(.semibold)).controlSize(.large).buttonBorderShape(.capsule)
                .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
                .frame(maxWidth: 560).frame(maxWidth: .infinity)
            }
        }
    }

    private var statusHeading: (symbol: String?, title: String, description: String) {
        switch state.phase {
        case .connecting, .progress:
            ("antenna.radiowaves.left.and.right", "detail.setupProgress", detailText("detail.keepForeground"))
        default:
            ("exclamationmark.triangle", "detail.operationFailed", detailText("detail.reconcileHint"))
        }
    }
    private var sourceList: some View {
        ESIMSetupPage(title: "detail.transfer", description: detailText("detail.transferSourceHint")) {
            ESIMSetupGroup {
                if state.sources.isEmpty, !state.busy { Text("detail.noSources").padding(.vertical, 16) }
                ForEach(state.sources, id: \.key) { source in
                    if source.key != state.sources.first?.key { Divider().padding(.leading, 40) }
                    Button {
                        state.selectSource(source)
                    } label: {
                        HStack(spacing: 12) {
                            Image(systemName: source.type == "modem" ? "iphone.gen3" : "simcard")
                                .font(.title3).foregroundStyle(.tint).frame(width: 28)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(source.name)
                                if let detail = source.detail { Text(detail).font(.caption).foregroundStyle(.secondary) }
                            }.frame(maxWidth: .infinity, alignment: .leading)
                            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
                        }.foregroundStyle(.primary).padding(.vertical, 16).contentShape(Rectangle())
                    }.accessibilityIdentifier("transferSource-" + source.key)
                    .disabled(source.type == "client_ccid" && source.cardPresent != true)
                }
            }
        }
        .disabled(state.busy)
    }
    private var profileList: some View {
        ESIMSetupPage(title: "detail.selectTransferProfile", description: state.source?.name ?? "") {
            if state.source?.requiresSourceImei == true {
                ESIMSetupGroup {
                    TextField("detail.sourceIMEI", text: $state.sourceIMEI).keyboardType(.numberPad)
                        .padding(.vertical, 16).disabled(state.busy)
                }
            }
            ESIMSetupGroup {
                if state.busy, state.profiles.isEmpty { ProfileSkeletonRows() }
                if !state.busy, state.profiles.isEmpty, state.failure == nil {
                    Text("detail.noTransferProfiles").foregroundStyle(.secondary).padding(.vertical, 16)
                }
                ForEach(state.profiles) { profile in
                    if profile.id != state.profiles.first?.id { Divider().padding(.leading, 44) }
                    Button { state.selectProfile(profile) } label: {
                        HStack {
                            ProfileAvatar(icon: profile.icon, region: profile.regionCode)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(profile.name)
                                Text(profile.iccid).font(.caption.monospaced()).foregroundStyle(.secondary)
                                if let reason = profile.unsupportedReason, !profile.supported { Text(reason).font(.caption).foregroundStyle(.secondary) }
                            }.frame(maxWidth: .infinity, alignment: .leading)
                            if state.selectedProfile?.id == profile.id { Image(systemName: "checkmark").foregroundStyle(.tint) }
                        }.foregroundStyle(.primary).padding(.vertical, 16).contentShape(Rectangle())
                    }
                    .disabled(!profile.supported || state.busy)
                    .accessibilityAddTraits(state.selectedProfile?.id == profile.id ? .isSelected : [])
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            Button { state.startTransfer() } label: {
                Text("detail.setupContinue").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent).controlSize(.large).buttonBorderShape(.capsule)
            .disabled(!state.canTransfer || state.busy)
            .accessibilityIdentifier("startESIMTransfer")
            .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
            .frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
        .task(id: state.sourceIMEI) {
            if state.source?.requiresSourceImei == true {
                do { try await Task.sleep(for: .milliseconds(500)) }
                catch { return }
            }
            guard !Task.isCancelled else { return }
            await state.loadProfiles()
        }
    }
    private var stageLabel: String {
        switch state.stage {
        case "Authenticating Client": detailText("detail.stageClient")
        case "Authenticating Server": detailText("detail.stageServer")
        case "Installing": detailText("detail.stageInstalling")
        case "preparing": detailText("detail.stagePreparing")
        case "carrier": detailText("detail.stageCarrier")
        case "downloading": detailText("detail.stageDownloading")
        case "enabling": detailText("detail.stageEnabling")
        case "deleting": detailText("detail.stageDeleting")
        case "completing": detailText("detail.stageCompleting")
        default: state.stage.isEmpty ? detailText("detail.connecting") : state.stage
        }
    }
}

private struct ESIMSetupPage<Content: View>: View {
    var symbol: String? = "antenna.radiowaves.left.and.right"
    let title: String
    var description = ""
    @ViewBuilder var content: () -> Content

    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    if let symbol {
                        Image(systemName: symbol)
                            .font(.system(size: 72, weight: .light)).foregroundStyle(.tint)
                            .frame(height: 76).frame(maxWidth: .infinity)
                            .padding(.bottom, 16).accessibilityHidden(true)
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        Text(detailText(title)).font(.title3.bold())
                            .accessibilityAddTraits(.isHeader)
                        if !description.isEmpty {
                            Text(description).font(.title3).foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    content()
                }
                // The native header moves up when the keyboard reduces the available height.
                .padding(.top, symbol == nil ? max(112, geometry.size.height * 0.28) : (geometry.size.height > 600 ? 96 : 24))
                .padding(.horizontal, 38).padding(.bottom, 24)
                .frame(maxWidth: 560).frame(maxWidth: .infinity, alignment: .top)
            }
            .scrollDismissesKeyboard(.interactively)
        }
    }
}

private struct ESIMSetupGroup<Content: View>: View {
    @ViewBuilder var content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0, content: content)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: .rect(cornerRadius: 28))
            .buttonStyle(.plain)
    }
}

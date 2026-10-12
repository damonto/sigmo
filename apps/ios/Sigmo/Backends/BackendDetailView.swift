import SwiftUI

struct BackendDetailView: View {
    @Environment(BackendStore.self) private var store
    let session: BackendSession
    @State private var editing = false
    @State private var login = false
    @State private var activation = false
    @State private var deleting = false

    var body: some View {
        Form {
            Section("backends.connection") {
                LabeledContent("backends.address") {
                    Text(session.configuration.displayAddress).textSelection(.enabled)
                        .accessibilityIdentifier("backendDisplayAddress")
                }
            }
            Section("auth.title") {
                LabeledContent("auth.status", value: NSLocalizedString("auth.\(session.authentication.rawValue)", comment: ""))
                if session.authentication == .required {
                    Button("auth.login", systemImage: "lock.open") { login = true }
                        .accessibilityIdentifier("loginBackend")
                }
                if session.authentication == .authenticated {
                    Button("auth.logout", role: .destructive) { session.logout() }
                }
            }
            Section("license.title") {
                LabeledContent("license.status", value: NSLocalizedString("license.\(session.license.rawValue)", comment: ""))
                if !session.licenseDeviceID.isEmpty {
                    LabeledContent("license.deviceID") { Text(session.licenseDeviceID).textSelection(.enabled) }
                }
                if session.license == .activation {
                    Button("activation.start", systemImage: "key") { activation = true }
                        .accessibilityIdentifier("activateBackend")
                }
            }
            Section("capabilities.title") {
                if let features = session.capabilities {
                    ForEach(["esimTransfer", "wifiCalling", "volte"], id: \.self) { feature in
                        LabeledContent(featureName(feature)) {
                            Text(features.contains(feature) ? "capabilities.supported" : "capabilities.unsupported")
                        }
                    }
                    ForEach(features.subtracting(["esimTransfer", "wifiCalling", "volte"]).sorted(), id: \.self) { feature in
                        LabeledContent(featureName(feature), value: String(localized: "capabilities.supported"))
                    }
                } else { Text("capabilities.notLoaded").foregroundStyle(Color.secondary) }
            }
            if let information = session.information {
                Section("backends.server") {
                    LabeledContent("backends.version", value: information.version)
                    LabeledContent("backends.edition", value: information.edition)
                    LabeledContent("backends.channel", value: information.channel)
                    LabeledContent("backends.commit", value: information.commit)
                    LabeledContent("backends.target", value: information.target)
                    LabeledContent("backends.distribution", value: information.distribution)
                }
            }
            Section {
                Button(role: .destructive) { deleting = true } label: {
                    Text("backends.delete").frame(maxWidth: .infinity)
                }
                .accessibilityIdentifier("deleteBackend")
            }
        }
        .navigationTitle(session.configuration.name)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await session.refresh(force: true) }
        .detailErrorAlert(login || activation || editing ? nil : session.failure ?? session.authFailure ?? session.capabilityFailure)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { Button("common.edit", systemImage: "pencil") { editing = true } }
        }
        .sheet(isPresented: $editing) { BackendEditor(configuration: session.configuration) }
        .sheet(isPresented: $login) { AuthenticationView(session: session) }
        .sheet(isPresented: $activation) { ActivationView(session: session) }
        .alert("backends.deleteTitle", isPresented: $deleting) {
            Button("common.delete", role: .destructive) { store.deleteBackend(session.id) }
            Button("common.cancel", role: .cancel) { deleting = false }
        } message: { Text("backends.deleteDescription") }
    }
}

struct AuthenticationView: View {
    @Environment(\.dismiss) private var dismiss
    let session: BackendSession
    @State private var code = ""
    @State private var verificationCode: String?

    private var loginComplete: Bool {
        !session.isVerifyingOTP && (session.authentication == .authenticated || session.authentication == .notRequired)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(session.configuration.name).font(.headline)
                    Text("auth.description").foregroundStyle(Color.secondary)
                    TextField("auth.code", text: $code)
                        .keyboardType(.numberPad).textContentType(.oneTimeCode)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        .accessibilityIdentifier("otpCode")
                        .disabled(session.isVerifyingOTP)
                        .onChange(of: code) { _, value in
                            let digits = String(value.filter { $0.isASCII && $0.isNumber }.prefix(6))
                            if digits != code { code = digits }
                            if digits.count == 6 { verificationCode = digits }
                        }
                    Button("auth.verify") { verificationCode = code }
                        .disabled(code.count != 6 || session.isVerifyingOTP)
                    if session.isVerifyingOTP { ProgressView("auth.verifying") }
                }
                Section {
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        let seconds = max(0, Int(ceil(session.resendAvailableAt.timeIntervalSince(context.date))))
                        AsyncActionButton(id: "sendOTP") {
                            await session.sendOTP()
                        } label: {
                            if seconds > 0 {
                                Text(String(format: NSLocalizedString("auth.resendCountdown", comment: ""), seconds))
                            } else { Text("auth.send") }
                        }
                        .disabled(seconds > 0 || session.isSendingOTP)
                    }
                }
            }
            .detailErrorAlert(session.authFailure)
            .navigationTitle("auth.login")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.done") { dismiss() } } }
        }
        .task(id: verificationCode) {
            guard let verificationCode, !Task.isCancelled else { return }
            await session.verifyOTP(verificationCode)
            if self.verificationCode == verificationCode { self.verificationCode = nil }
        }
        .onDisappear { verificationCode = nil; code = "" }
        .onChange(of: loginComplete) { _, complete in
            if complete { dismiss() }
        }
    }
}

struct ActivationView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    let session: BackendSession
    @State private var linkFailure: BackendFailure?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Label(session.configuration.name, systemImage: "server.rack").font(.headline)
                    Text("activation.description").foregroundStyle(Color.secondary)
                    if session.waitingForRestart {
                        ProgressView("activation.restarting")
                    } else if let pairing = session.pairing {
                        Text(LocalizedStringKey(pairing.statusKey))
                            .accessibilityIdentifier("pairingStatus")
                        if pairing.status == "pending" {
                            LabeledContent("activation.expires") { Text(pairing.expiresAt, style: .time) }
                            Button("activation.openTelegram", systemImage: "arrow.up.forward.app") {
                                do {
                                    let url = try pairing.validatedURL()
                                    openURL(url) { accepted in
                                        if !accepted { linkFailure = .local("activation.openFailed") }
                                    }
                                } catch { linkFailure = .wrapping(error) }
                            }
                        }
                    }
                    if session.pairing == nil || session.pairing?.status == "expired" {
                        AsyncActionButton(id: "createPairing") {
                            await session.createPairing()
                        } label: {
                            Label("activation.create", systemImage: "key")
                        }
                        .disabled(session.isCreatingPairing)
                    }
                    AsyncActionButton {
                        await session.refresh(force: true)
                    } label: {
                        Label("common.refresh", systemImage: "arrow.clockwise")
                    }
                }
            }
            .detailErrorAlert(linkFailure ?? session.pairingFailure ?? session.failure)
            .navigationTitle("activation.title")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.done") { dismiss() } } }
        }
        .foregroundTask {
            await session.observePairing()
        }
        .onChange(of: session.activationReady) { _, ready in
            if ready { dismiss() }
        }
    }
}

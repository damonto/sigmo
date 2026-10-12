import SwiftUI

struct UpdatesSettingsView: View {
    @Bindable var state: UpdatesState
    @State private var pendingPreferences: UpdatePreferences?

    private var observationActive: Bool {
        state.shouldPoll && !state.working
    }

    var body: some View {
        SettingsAccessView(session: state.session) {
            Form {
                if let snapshot = state.snapshot {
                    Section("settings.updates.preferences") {
                        Toggle("settings.updates.automatic", isOn: Binding(get: { snapshot.settings.automatic }, set: { value in
                            save(automatic: value, channel: snapshot.settings.channel)
                        }))
                        .disabled(state.working || pendingPreferences != nil || snapshot.busy || state.recovering || !snapshot.selfUpdateSupported)
                        .accessibilityIdentifier("automatic-updates")
                        Picker("settings.updates.channel", selection: Binding(get: { snapshot.settings.channel }, set: { value in
                            save(automatic: snapshot.settings.automatic, channel: value)
                        })) {
                            Text("Stable").tag("stable")
                            if snapshot.current.edition == "pro" { Text("Dev").tag("dev") }
                        }
                        .disabled(state.working || pendingPreferences != nil || snapshot.busy || state.recovering || snapshot.current.edition != "pro")
                        .accessibilityIdentifier("update-channel")
                    }
                    Section("settings.updates.versionTitle") {
                        LabeledContent("settings.updates.installedVersion", value: snapshot.current.version)
                            .accessibilityLabel(Text("settings.updates.installedVersion"))
                            .accessibilityValue(snapshot.current.version)
                        LabeledContent("settings.updates.serverVersion", value: snapshot.latest?.version ?? settingsText("settings.updates.notChecked"))
                            .accessibilityLabel(Text("settings.updates.serverVersion"))
                            .accessibilityValue(snapshot.latest?.version ?? settingsText("settings.updates.notChecked"))
                            .accessibilityIdentifier("latest-update-version")
                        LabeledContent("settings.updates.target", value: snapshot.current.target)
                            .accessibilityLabel(Text("settings.updates.target"))
                            .accessibilityValue(snapshot.current.target)
                        LabeledContent("settings.native.checkedAt", value: snapshot.checkedAt?.formatted(date: .abbreviated, time: .shortened) ?? settingsText("settings.updates.never"))
                        LabeledContent("settings.updates.state", value: settingsText("settings.updates.status.\(state.recovering ? "restarting" : snapshot.state)"))
                        if state.shouldPoll { ProgressView().accessibilityLabel(Text("settings.updates.state")) }
                        if let error = snapshot.error {
                            Text(localizedError(error, code: snapshot.errorCode)).foregroundStyle(.red)
                        }
                        if snapshot.checkedAt != nil && snapshot.state == "idle" && !snapshot.updateAvailable && snapshot.error == nil {
                            Label("settings.updates.upToDate", systemImage: "checkmark.shield")
                                .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
                                .accessibilityIdentifier("up-to-date")
                        }
                        AsyncActionButton(title: "settings.updates.checkNow", id: "check-updates") { await state.check() }.disabled(!state.canCheck)
                        AsyncActionButton(title: "settings.updates.installNow", id: "install-update") { await state.install() }.disabled(!state.canInstall)
                    }
                    if let notes = snapshot.latest?.notes, !notes.isEmpty {
                        Section("settings.updates.notes") { Text(verbatim: notes).textSelection(.enabled) }
                    }
                    if let license = snapshot.license {
                        Section("settings.updates.licensedTo") {
                            LabeledContent("settings.native.licenseName", value: license.displayName)
                            if let username = license.username { LabeledContent("settings.native.username", value: "@\(username)") }
                            LabeledContent("Telegram ID", value: String(license.telegramId))
                            LabeledContent("settings.updates.deviceId", value: license.deviceId)
                            LabeledContent("settings.updates.licenseStatus", value: settingsText(license.statusKey))
                                .accessibilityIdentifier("updateLicenseStatus")
                            LabeledContent("settings.updates.licenseExpires", value: license.expiresAt?.formatted(date: .abbreviated, time: .shortened) ?? settingsText("settings.updates.licenseNeverExpires"))
                            if let date = license.offlineUntil {
                                LabeledContent("settings.updates.licenseOfflineUntil", value: date.formatted(date: .abbreviated, time: .shortened))
                            }
                        }
                    }
                } else if state.working { ProgressView() }
                if state.working && state.snapshot != nil { ProgressView("settings.native.working") }
            }
            .detailErrorAlert(state.failure)
            .refreshable { await state.load() }
            .foregroundTask { await state.load() }
            .foregroundTask(id: observationActive) {
                if observationActive { await state.observe() }
            }
        }
        .navigationTitle("settings.updates.title").navigationBarTitleDisplayMode(.inline)
        .task(id: pendingPreferences) {
            guard let pendingPreferences, !Task.isCancelled else { return }
            await state.save(automatic: pendingPreferences.automatic, channel: pendingPreferences.channel)
            if !Task.isCancelled { self.pendingPreferences = nil }
        }
        .onDisappear { pendingPreferences = nil }
    }

    private func save(automatic: Bool, channel: String) {
        guard pendingPreferences == nil else { return }
        pendingPreferences = UpdatePreferences(automatic: automatic, channel: channel)
    }
    private func localizedError(_ message: String, code: String?) -> String {
        guard let code else { return message }
        let key = "apiError.\(code)"
        let localized = settingsText(key)
        return localized == key ? message : localized
    }
}

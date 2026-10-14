import SwiftUI

struct NativePushSettingsView: View {
    let session: BackendSession
    @Environment(\.nativePush) private var push
    @Environment(\.demoSession) private var demo
    @Environment(\.openURL) private var openURL

    var body: some View {
        Form {
            if session.isDemo {
                if let demo { DemoNotificationSettings(preferences: demo.notifications(for: session.id)) }
            } else if let push {
                Section {
                    LabeledContent("push.permission", value: push.allowed ? String(localized: "push.allowed") : String(localized: "push.denied"))
                    Button("push.systemSettings") {
                        if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
                    }
                }
                if push.unsupported.contains(session.id) {
                    Section { Text("messages.upgrade") }
                } else if let binding = push.binding(sessionID: session.id) {
                    Section {
                        Toggle("push.enabled", isOn: Binding(get: { binding.enabled }, set: { push.setPreferences(sessionID: session.id, enabled: $0) }))
                        Toggle("push.sms", isOn: Binding(get: { binding.sms }, set: { push.setPreferences(sessionID: session.id, sms: $0) }))
                        Toggle("push.reminders", isOn: Binding(get: { binding.reminders }, set: { push.setPreferences(sessionID: session.id, reminders: $0) }))
                        Toggle("calls.incomingCalls", isOn: Binding(get: { binding.allowsCalls }, set: { push.setPreferences(sessionID: session.id, calls: $0) }))
                            .disabled(session.capabilities?.contains("nativeCalls") != true)
                        LabeledContent("push.status", value: push.registered.contains(binding.id) ? String(localized: "push.synced") : String(localized: "push.pending"))
                    } footer: { Text("push.previewExplanation") }
                } else {
                    Section { Text("push.pending").foregroundStyle(.secondary) }
                }
            }
        }
        .navigationTitle("push.thisDevice")
        .navigationBarTitleDisplayMode(.inline)
        .detailErrorAlert(push.flatMap { $0.failures[session.id] ?? $0.failure ?? $0.voipFailure })
        .accessibilityIdentifier("native-push-settings")
    }
}

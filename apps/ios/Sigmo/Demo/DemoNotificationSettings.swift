import Observation
import SwiftUI

@MainActor @Observable
final class DemoNotificationPreferences {
    var enabled = true
    var sms = true
    var reminders = true
    var calls = true
}

struct DemoNotificationSettings: View {
    @Bindable var preferences: DemoNotificationPreferences

    var body: some View {
        Section {
            Toggle("push.enabled", isOn: $preferences.enabled)
            Toggle("push.sms", isOn: $preferences.sms)
            Toggle("push.reminders", isOn: $preferences.reminders)
            Toggle("calls.incomingCalls", isOn: $preferences.calls)
        } footer: { Text("demo.notificationDescription") }
    }
}

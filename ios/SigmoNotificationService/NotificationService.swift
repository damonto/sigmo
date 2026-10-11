import Foundation
import UserNotifications

final class NotificationService: UNNotificationServiceExtension {
    private var completion: ((UNNotificationContent) -> Void)?
    private var fallback: UNNotificationContent?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        completion = contentHandler
        fallback = request.content
        guard let encrypted = request.content.userInfo["sigmo"],
              let data = try? JSONSerialization.data(withJSONObject: encrypted),
              let content = try? PushPayload.decode(data, forPreview: true),
              let updated = request.content.mutableCopy() as? UNMutableNotificationContent else {
            finish(request.content)
            return
        }
        if content.kind == "sms" {
            updated.title = content.sender ?? String(localized: "push.generic.message")
            updated.subtitle = [content.backendName, content.modemName ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
        } else {
            updated.title = String(localized: "push.generic.reminder")
            updated.subtitle = [content.backendName, content.profileName ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
        }
        updated.body = content.text ?? ""
        finish(updated)
    }

    override func serviceExtensionTimeWillExpire() {
        if let fallback { finish(fallback) }
    }

    private func finish(_ content: UNNotificationContent) {
        let handler = completion
        completion = nil
        handler?(content)
    }
}

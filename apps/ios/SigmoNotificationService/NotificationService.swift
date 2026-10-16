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
        let settings = (try? PushKeychain.read(PushPreviewSettings.keychainAccount, shared: true)).flatMap {
            try? JSONDecoder().decode(PushPreviewSettings.self, from: $0)
        }
        let preview = PushPreview(content: content, settings: settings ?? PushPreviewSettings())
        updated.title = preview.title
        updated.subtitle = preview.subtitle
        updated.body = preview.body
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

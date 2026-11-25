import Foundation
@preconcurrency import Intents
import UserNotifications

final class NotificationService: UNNotificationServiceExtension {
    private let lock = NSLock()
    private var delivery: Delivery?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        let delivery = Delivery(fallback: request.content, completion: contentHandler)
        lock.withLock { self.delivery = delivery }
        guard let encrypted = request.content.userInfo["sigmo"],
              let data = try? JSONSerialization.data(withJSONObject: encrypted),
              let content = try? PushPayload.decode(data, forPreview: true),
              let updated = request.content.mutableCopy() as? UNMutableNotificationContent else {
            delivery.finish()
            return
        }
        let settings = (try? PushKeychain.read(PushPreviewSettings.keychainAccount, shared: true)).flatMap {
            try? JSONDecoder().decode(PushPreviewSettings.self, from: $0)
        }
        let preview = PushPreview(content: content, settings: settings ?? PushPreviewSettings())
        updated.title = preview.title
        updated.subtitle = preview.subtitle
        updated.body = preview.body
        delivery.setFallback(updated)
        guard let artwork = PushArtworkCache.notificationImage(for: content, cache: .appGroup), delivery.isPending else {
            delivery.finish()
            return
        }

        let senderID = PushArtworkCache.key(content.backendId, content.sender ?? request.identifier)
        let sender = INPerson(personHandle: INPersonHandle(value: senderID, type: .unknown),
                              nameComponents: nil, displayName: preview.title, image: INImage(imageData: artwork),
                              contactIdentifier: nil, customIdentifier: senderID)
        let intent = INSendMessageIntent(recipients: nil, outgoingMessageType: .outgoingMessageText,
            content: preview.body, speakableGroupName: nil,
            conversationIdentifier: PushArtworkCache.key(content.backendId, content.conversationId ?? request.identifier),
            serviceName: nil, sender: sender, attachments: nil)
        let interaction = INInteraction(intent: intent, response: nil)
        interaction.direction = .incoming
        interaction.donate { error in
            delivery.finish(updatingFrom: error == nil ? intent : nil)
        }
    }

    override func serviceExtensionTimeWillExpire() {
        lock.withLock { delivery }?.finish()
    }

    // Donation and extension expiration can complete on different threads.
    private final class Delivery: @unchecked Sendable {
        private let lock = NSLock()
        private var completion: ((UNNotificationContent) -> Void)?
        private var fallback: UNNotificationContent

        init(fallback: UNNotificationContent, completion: @escaping (UNNotificationContent) -> Void) {
            self.fallback = fallback
            self.completion = completion
        }

        var isPending: Bool { lock.withLock { completion != nil } }

        func setFallback(_ content: UNNotificationContent) {
            // The caller finishes editing the preview before publishing it here.
            lock.withLock { fallback = content }
        }

        func finish(updatingFrom intent: INSendMessageIntent? = nil) {
            let updated: UNNotificationContent? = if let intent, let content = lock.withLock({ completion == nil ? nil : fallback }) {
                try? content.updating(from: intent)
            } else {
                nil
            }
            let (handler, result) = lock.withLock {
                let handler = completion
                completion = nil
                return (handler, updated ?? fallback)
            }
            handler?(result)
        }
    }
}

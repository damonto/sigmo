import UIKit
import UserNotifications
import PushKit
import Intents

@MainActor
final class PushAppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate, @preconcurrency PKPushRegistryDelegate {
    let store: BackendStore
    let push: NativePushState
    let calls: CallsState
    private var voip: PKPushRegistry?

    override init() {
        // Restore the diagnostic preference before configuration, credentials or push initialization.
        AppDiagnostics.record("app.launched")
        let store = AppLaunch.makeStore()
        self.store = store
        self.push = NativePushState(disabled: AppLaunch.usesFixtures)
        self.calls = CallsState(store: store, fixtures: AppLaunch.usesFixtures)
        super.init()
    }

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        calls.foreground = application.applicationState != .background
        UNUserNotificationCenter.current().delegate = self
        registerVoIPIfNeeded()
        return true
    }

    func registerVoIPIfNeeded() {
        // An empty installation can explore the offline demo before connecting
        // a server. Register VoIP only once there is a real connection to use it.
        if !AppLaunch.usesFixtures, voip == nil, !store.settings.backends.isEmpty {
            let registry = PKPushRegistry(queue: .main)
            registry.delegate = self
            voip = registry
            registry.desiredPushTypes = [.voIP]
        }
    }

    func pushRegistry(_ registry: PKPushRegistry, didUpdate pushCredentials: PKPushCredentials, for type: PKPushType) {
        if type == .voIP { push.registeredVoIP(token: pushCredentials.token) }
    }

    func pushRegistry(_ registry: PKPushRegistry, didInvalidatePushTokenFor type: PKPushType) {
        if type == .voIP { push.registeredVoIP(token: nil) }
    }

    func pushRegistry(_ registry: PKPushRegistry, didReceiveIncomingPushWith payload: PKPushPayload,
                      for type: PKPushType, completion: @escaping @Sendable () -> Void) {
        guard type == .voIP else { completion(); return }
        let content = NativePushState.envelopeData(payload.dictionaryPayload).flatMap { try? PushPayload.decode($0) }
        let id = (payload.dictionaryPayload["callUUID"] as? String).flatMap(UUID.init(uuidString:)) ?? UUID()
        calls.receivePush(content: content, id: id, connection: content.flatMap { push.connection(for: $0, in: store) }, completion: completion)
    }

    func application(_ application: UIApplication, continue userActivity: NSUserActivity,
                     restorationHandler: @escaping ([any UIUserActivityRestoring]?) -> Void) -> Bool {
        guard let intent = userActivity.interaction?.intent as? INStartCallIntent,
              let number = intent.contacts?.first?.personHandle?.value,
              CallDigits.callable(number), !CallDigits.isService(number) else { return false }
        calls.callbackNumber = number
        restorationHandler(nil)
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        push.registered(token: deviceToken)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        AppDiagnostics.record(error, category: "push", operation: "registration.failed")
        push.failure = .wrapping(error)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        let content = NativePushState.envelopeData(notification.request.content.userInfo).flatMap { try? PushPayload.decode($0, forPreview: true) }
        let suppress = await push.receive(content, tapped: false)
        AppDiagnostics.record("notification.processed", category: "push", fields: ["tapped": "false", "suppressed": String(suppress)])
        return suppress ? [.badge] : [.banner, .list, .sound, .badge]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                          withCompletionHandler completionHandler: @escaping @Sendable () -> Void) {
        let content = NativePushState.envelopeData(response.notification.request.content.userInfo).flatMap { try? PushPayload.decode($0) }
        push.receiveResponse(content, action: response.actionIdentifier, completion: completionHandler)
    }
}

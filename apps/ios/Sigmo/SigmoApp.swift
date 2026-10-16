import SwiftUI
import Contacts

@main
struct SigmoApp: App {
    @UIApplicationDelegateAdaptor(PushAppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase
    private var store: BackendStore { appDelegate.store }

    var body: some Scene {
        WindowGroup {
            HomeView()
                .environment(store)
                .environment(appDelegate.calls)
                .environment(\.nativePush, appDelegate.push)
                .preferredColorScheme(AppLaunch.colorScheme)
                .foregroundTask { await store.refreshAll() }
                .onChange(of: scenePhase, initial: true) { _, phase in
                    AppDiagnostics.record("scene.changed", fields: ["state": phase == .active ? "active" : phase == .background ? "background" : "inactive"])
                    appDelegate.calls.foreground = phase != .background
                    if phase != .active { appDelegate.calls.recordings.stopPlayback() }
                    store.setForeground(phase != .background)
                }
                .onChange(of: store.settings.backends.map(\.id), initial: true) { _, _ in
                    appDelegate.registerVoIPIfNeeded()
                }
                .task { await appDelegate.calls.run() }
                .task { await appDelegate.calls.recordings.recover() }
                .task(id: appDelegate.push.eventRevision) {
                    guard scenePhase == .active, appDelegate.push.eventRevision > 0 else { return }
                    await store.refreshUnreadCounts()
                }
                .task(id: scenePhase == .active) {
                    guard scenePhase == .active, !AppLaunch.usesFixtures else { return }
                    await appDelegate.calls.reloadContacts()
                    for await _ in NotificationCenter.default.notifications(named: .CNContactStoreDidChange) {
                        if Task.isCancelled { return }
                        await appDelegate.calls.reloadContacts()
                    }
                }
                .foregroundTask(id: appDelegate.push.lifecycle(store: store)) {
                    await appDelegate.push.run(store: store)
                }
        }
    }
}

@MainActor
enum AppLaunch {
    static var usesFixtures: Bool {
        #if DEBUG
        return ProcessInfo.processInfo.environment["SIGMO_UI_SCENARIO"] != nil
            || ProcessInfo.processInfo.environment["SIGMO_UNIT_TESTS"] == "1"
        #else
        return false
        #endif
    }

    static var colorScheme: ColorScheme? {
        #if DEBUG
        switch ProcessInfo.processInfo.environment["SIGMO_UI_APPEARANCE"] {
        case "dark": return .dark
        case "light": return .light
        default: break
        }
        #endif
        return nil
    }

    static func makeStore() -> BackendStore {
        #if DEBUG
        if let scenario = ProcessInfo.processInfo.environment["SIGMO_UI_SCENARIO"] {
            return PreviewBackends.store(scenario: scenario)
        }
        if ProcessInfo.processInfo.environment["SIGMO_UNIT_TESTS"] == "1" {
            return BackendStore(
                file: ConfigurationFile(url: URL.temporaryDirectory.appending(path: "SigmoUnitHost-\(UUID()).json")),
                credentials: BackendCredentials(read: { _ in nil }, write: { _, _ in })
            )
        }
        #endif
        return BackendStore()
    }
}

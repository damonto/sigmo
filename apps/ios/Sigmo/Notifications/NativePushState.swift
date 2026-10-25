import CryptoKit
import Observation
import SwiftUI
import UserNotifications

struct PushLifecycle: Equatable {
    let revision: Int
    let connections: [String]
}

extension EnvironmentValues {
    @Entry var nativePush: NativePushState? = nil
}

// Permission prompts belong to ordinary notifications. A pending prompt must
// never suspend VoIP registration or backend subscription reconciliation.
@MainActor
struct PushNotificationAccess {
    let status: () async -> UNAuthorizationStatus
    let request: () async throws -> Void
    let register: () -> Void
    let setBadge: (Int) async throws -> Void

    static let system = PushNotificationAccess(
        status: { await UNUserNotificationCenter.current().notificationSettings().authorizationStatus },
        request: { _ = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) },
        register: { UIApplication.shared.registerForRemoteNotifications() },
        setBadge: { try await UNUserNotificationCenter.current().setBadgeCount($0) })
}

@MainActor @Observable
final class NativePushState {
    private(set) var authorization: UNAuthorizationStatus = .notDetermined {
        didSet {
            if authorization != oldValue {
                AppDiagnostics.record("authorization.changed", category: "push", fields: ["state": String(authorization.rawValue)])
            }
        }
    }
    private(set) var eventRevision = 0
    private(set) var configurationRevision = 0
    private(set) var failures: [UUID: BackendFailure] = [:]
    private(set) var registered: Set<String> = []
    var failure: BackendFailure?
    private(set) var voipFailure: BackendFailure?
    var navigationFailure: BackendFailure? {
        didSet {
            if let navigationFailure, navigationFailure != oldValue {
                AppDiagnostics.record(navigationFailure, category: "push", operation: "notification.navigation.failed")
            }
        }
    }
    var pendingNavigation: PushNavigation?
    var visibleConversation: PushConversation?
    var visibleThread: PushThread?

    private var identity: PushIdentity?
    @ObservationIgnored private let disabled: Bool
    @ObservationIgnored private let persistence: PushPersistence
    @ObservationIgnored private let gateway: PushGateway
    @ObservationIgnored private let notificationAccess: PushNotificationAccess
    @ObservationIgnored private let center = UNUserNotificationCenter.current()
    @ObservationIgnored private weak var store: BackendStore?
    @ObservationIgnored private var deviceToken: String?
    @ObservationIgnored private var registeredToken: String?
    @ObservationIgnored private var voipToken: String?
    @ObservationIgnored private var registeredVoIPToken: String?
    @ObservationIgnored private var voipRegistration: Task<Void, Never>?
    @ObservationIgnored private var authorizationTask: Task<Void, Never>?
    @ObservationIgnored private var authorizationRequested = false
    @ObservationIgnored private var badges: [String: MessageBadge] = [:]
    @ObservationIgnored private var badgeTask: Task<Void, Never>?
    @ObservationIgnored private var registrationStamps: [String: String] = [:]
    @ObservationIgnored private var previewSettings: PushPreviewSettings?
    @ObservationIgnored private var gatewayRetryAt: Date?
    @ObservationIgnored private let now: () -> Date

    init(disabled: Bool = false, identity initialIdentity: PushIdentity? = nil, persistence: PushPersistence = .keychain,
         gateway: PushGateway = PushGateway(), notificationAccess: PushNotificationAccess = .system,
         now: @escaping () -> Date = Date.init) {
        self.disabled = disabled
        self.persistence = persistence
        self.identity = initialIdentity
        self.gateway = gateway
        self.notificationAccess = notificationAccess
        self.now = now
        guard !disabled else { return }
        restoreIdentity()
    }

    var allowed: Bool { [.authorized, .provisional, .ephemeral].contains(authorization) }
    var hasIdentity: Bool { identity != nil }
    var voipReady: Bool { voipToken?.isEmpty == false && registeredVoIPToken == voipToken }

    func registeredVoIP(token: Data?) {
        guard !disabled else { return }
        AppDiagnostics.record(token == nil ? "voip.token.invalidated" : "voip.token.registered", category: "push")
        let value = token?.map { String(format: "%02x", $0) }.joined() ?? ""
        voipToken = value
        registerVoIP(value)
    }

    private func registerVoIP(_ value: String) {
        let previous = voipRegistration
        voipRegistration = Task { [weak self] in
            await previous?.value
            guard let self, self.voipToken == value, self.canContactGateway else { return }
            self.restoreIdentity()
            guard let identity = self.identity else { return }
            do {
                _ = try await self.gateway.register(identity: identity, voipToken: value, environment: self.environment, label: UIDevice.current.name)
                self.registeredVoIPToken = value
                AppDiagnostics.record("voip.registration.completed", category: "push")
                self.voipFailure = nil
                self.registered.removeAll()
                self.configurationRevision += 1
            } catch { self.voipFailure = self.recordFailure(error) }
        }
    }

    func lifecycle(store: BackendStore) -> PushLifecycle {
        PushLifecycle(revision: configurationRevision, connections: store.settings.backends.map { configuration in
            let session = store.sessions[configuration.id]
            return "\(configuration.id)|\(configuration.address)|\(configuration.name)|\(session?.authentication.rawValue ?? "")|\(session?.license.rawValue ?? "")|\(session?.accessRevision ?? 0)|\(session?.hasLoadedModems ?? false)"
        })
    }

    func run(store: BackendStore) async {
        guard !disabled else { return }
        self.store = store
        while store.foreground, !Task.isCancelled {
            await reconcile(store: store)
            do { try await Task.sleep(for: .seconds(30)) } catch { return }
        }
    }

    func registered(token: Data) {
        let value = token.map { String(format: "%02x", $0) }.joined()
        guard deviceToken != value else { return }
        AppDiagnostics.record("notification.token.registered", category: "push")
        deviceToken = value
        registered.removeAll()
        configurationRevision += 1
    }

    func retry() {
        if !disabled { restoreIdentity() }
        authorizationRequested = false
        configurationRevision += 1
    }

    private func restoreIdentity() {
        guard identity == nil else { return }
        do {
            if let data = try persistence.read("installation", false) {
                identity = try JSONDecoder().decode(PushIdentity.self, from: data)
            } else {
                let value = PushIdentity(id: UUID().uuidString, managementToken: PushIdentity.credential())
                try persist(value)
            }
            failure = nil
        } catch {
            _ = recordFailure(error)
            failure = .local("push.keychainUnavailable")
        }
    }

    func binding(sessionID: UUID) -> PushBinding? {
        guard let identity, let connection = identity.connections.first(where: { $0.configurationID == sessionID }) else { return nil }
        return identity.bindings.first { $0.backendID == connection.backendID }
    }

    func backendID(sessionID: UUID) -> String? {
        identity?.connections.first { $0.configurationID == sessionID }?.backendID
    }

    func setPreferences(sessionID: UUID, enabled: Bool? = nil, sms: Bool? = nil, reminders: Bool? = nil, calls: Bool? = nil) {
        guard var next = identity, let binding = binding(sessionID: sessionID),
              let index = next.bindings.firstIndex(where: { $0.id == binding.id }) else { return }
        if let enabled { next.bindings[index].enabled = enabled }
        if let sms { next.bindings[index].sms = sms }
        if let reminders { next.bindings[index].reminders = reminders }
        if let calls { next.bindings[index].calls = calls }
        do {
            try persist(next)
            registered.remove(binding.id)
            configurationRevision += 1
            applyBadge()
        } catch {
            _ = recordFailure(error)
            failure = .local("push.keychainUnavailable")
        }
    }

    func record(_ badge: MessageBadge, sessionID: UUID) {
        guard backendID(sessionID: sessionID) == badge.backendId else { return }
        merge(badge)
        applyBadge()
    }

    func receive(_ content: PushContent?, tapped: Bool) -> Bool {
        AppDiagnostics.record("notification.received", category: "push", fields: ["decoded": String(content != nil), "tapped": String(tapped)])
        eventRevision += 1
        guard let content else {
            if tapped { navigationFailure = .local("push.unavailableNotification") }
            return false
        }
        if !disabled { restoreIdentity() }
        if identity == nil {
            if tapped { pendingNavigation = PushNavigation(content: content) }
            return false
        }
        guard identity?.connections.contains(where: { $0.backendID == content.backendId }) == true else {
            if tapped { navigationFailure = .local("push.connectionRemoved") }
            return false
        }
        if tapped { pendingNavigation = PushNavigation(content: content) }
        guard content.kind == "sms" else { return false }
        if visibleConversation == PushConversation(backendID: content.backendId, conversationID: content.conversationId ?? "") { return true }
        guard let thread = visibleThread, thread.backendID == content.backendId else { return false }
        if let id = content.conversationId, thread.conversationIDs.contains(id) { return true }
        // A new SIM can create another source conversation for the visible correspondent.
        guard let sender = content.sender, !sender.isEmpty else { return false }
        if sender.trimmingCharacters(in: .whitespacesAndNewlines) == thread.participant { return true }
        let phoneCharacters = CharacterSet.decimalDigits.union(.whitespacesAndNewlines).union(CharacterSet(charactersIn: "+()- ."))
        guard sender.unicodeScalars.allSatisfy(phoneCharacters.contains) else { return false }
        return PhoneNumberDisplay.submission(sender, regionCode: nil) == thread.participant
    }

    nonisolated func receiveResponse(_ content: PushContent?, action: String, completion: @escaping @Sendable () -> Void) {
        // UIKit restores the foreground scene from this completion. The async
        // delegate bridge can invoke it on a worker thread and crash on a tap.
        Task { @MainActor in
            defer { completion() }
            guard action == UNNotificationDefaultActionIdentifier else { return }
            let state = UIApplication.shared.applicationState.rawValue
            AppDiagnostics.record("notification.defaultAction", category: "push", fields: ["decoded": String(content != nil), "applicationState": String(state)])
            let suppress = receive(content, tapped: true)
            AppDiagnostics.record("notification.processed", category: "push", fields: ["tapped": "true", "suppressed": String(suppress)])
        }
    }

    func connection(for content: PushContent, in store: BackendStore) -> BackendConfiguration? {
        let connections = identity?.connections.filter { $0.backendID == content.backendId } ?? []
        let candidates = store.settings.backends.filter { configuration in
            connections.contains { $0.configurationID == configuration.id && $0.address == configuration.address }
        }
        return candidates.first { store.sessions[$0.id]?.canAccessDevices == true } ?? candidates.first
    }

    func takeNavigation(in store: BackendStore, active: Bool) -> BackendDestination? {
        // A notification response can arrive before the scene is foregrounded.
        // Retain the target until the navigation stack can present its destination.
        guard active, hasIdentity, let navigation = pendingNavigation, store.configurationLoaded else { return nil }
        guard let connection = connection(for: navigation.content, in: store) else {
            navigationFailure = .local("push.connectionRemoved")
            pendingNavigation = nil
            return nil
        }
        let content = navigation.content
        pendingNavigation = nil
        if content.kind == "sms", let conversationID = content.conversationId {
            return .messages(connection.id, content.modemId ?? "", conversationID)
        }
        if content.kind == "reminder", let profileID = content.profileId {
            return .reminder(connection.id, content.modemId ?? "", profileID, content.profileType ?? "", content.seId ?? "")
        }
        navigationFailure = .local("push.unavailableNotification")
        return nil
    }

    func removeNotifications(backendID: String, conversationID: String) async {
        let notifications = await center.deliveredNotifications()
        let identifiers = notifications.compactMap { notification -> String? in
            guard let data = Self.envelopeData(notification.request.content.userInfo),
                  let content = try? PushPayload.decode(data), content.backendId == backendID,
                  content.conversationId == conversationID else { return nil }
            return notification.request.identifier
        }
        center.removeDeliveredNotifications(withIdentifiers: identifiers)
    }

    nonisolated static func envelopeData(_ userInfo: [AnyHashable: Any]) -> Data? {
        guard let encrypted = userInfo["sigmo"] else { return nil }
        return try? JSONSerialization.data(withJSONObject: encrypted)
    }

    func reconcile(store: BackendStore) async {
        self.store = store
        guard store.configurationLoaded else { return }
        do {
            let settings = PushPreviewSettings(includesBackendName: store.settings.backends.count != 1)
            if previewSettings != settings {
                try persistence.write(PushPreviewSettings.keychainAccount, JSONEncoder().encode(settings), true)
                previewSettings = settings
            }
        } catch {
            // Preview formatting must not block revocation or push registration.
            AppDiagnostics.record(error, category: "push", operation: "preview.settings.write.failed")
        }
        restoreIdentity()
        guard identity != nil else { return }
        if canContactGateway { failure = nil }
        do {
            try removeInvalidConnections(store)
        } catch {
            _ = recordFailure(error)
            failure = .local("push.keychainUnavailable")
            return
        }
        do {
            try await revokePending()
        } catch {
            if !Task.isCancelled { failure = recordFailure(error) }
        }
        guard !Task.isCancelled else { return }
        await voipRegistration?.value
        if canContactGateway, let voipToken, registeredVoIPToken != voipToken {
            registerVoIP(voipToken)
            await voipRegistration?.value
        }
        do {
            authorization = await notificationAccess.status()
            if authorization == .notDetermined, !authorizationRequested, authorizationTask == nil,
               store.sessions.values.contains(where: { $0.canAccessDevices && $0.hasLoadedModems }) {
                requestNotificationAccess()
            }
            try Task.checkCancellation()
            if allowed {
                // Ask APNs every foreground launch; the token is deliberately not persisted.
                notificationAccess.register()
                if canContactGateway, let identity, let deviceToken, registeredToken != deviceToken {
                    let result = try await gateway.register(identity: identity, deviceToken: deviceToken, environment: environment, label: UIDevice.current.name)
                    try Task.checkCancellation()
                    registeredToken = deviceToken
                    merge(result)
                }
            }
        } catch {
            if !Task.isCancelled { failure = recordFailure(error) }
        }
        // A gateway outage must not prevent discovering backend identity or read state.
        var reconciledBackends: Set<String> = []
        for configuration in store.settings.backends {
            guard !Task.isCancelled else { return }
            guard let session = store.sessions[configuration.id], session.canAccessDevices else { continue }
            await reconcile(session, backends: &reconciledBackends)
        }
        guard !Task.isCancelled else { return }
        do {
            if canContactGateway, let identity, registeredToken != nil {
                let result = try await gateway.badge(identity: identity)
                try Task.checkCancellation()
                merge(result)
            }
        } catch {
            if !Task.isCancelled { failure = recordFailure(error) }
        }
        if !Task.isCancelled { applyBadge() }
    }

    private func requestNotificationAccess() {
        authorizationRequested = true
        authorizationTask = Task { [weak self] in
            guard let self else { return }
            defer { self.authorizationTask = nil; self.configurationRevision += 1 }
            do {
                try await self.notificationAccess.request()
                self.authorization = await self.notificationAccess.status()
            } catch { self.failure = self.recordFailure(error) }
        }
    }

    private var environment: String {
        Bundle.main.object(forInfoDictionaryKey: "SigmoAPNsEnvironment") as? String ?? "development"
    }

    private var canContactGateway: Bool { gatewayRetryAt.map { $0 <= now() } ?? true }

    private func recordFailure(_ error: Error) -> BackendFailure {
        AppDiagnostics.record(error, category: "push", operation: "push.failed")
        let value = BackendFailure.wrapping(error)
        if value.source == .pushGateway, let deadline = value.retryAt {
            gatewayRetryAt = max(gatewayRetryAt ?? deadline, deadline)
        }
        return value
    }

    private func reconcile(_ session: BackendSession, backends: inout Set<String>) async {
        if let deadline = failures[session.id]?.retryAt, deadline > now(), failures[session.id]?.source == .backend { return }
        do {
            let overview = try await session.protectedRequest { api, token in try await api.nativePush(token: token) }
            guard !Task.isCancelled, store?.sessions[session.id] === session else { return }
            guard var next = identity else { return }
            let connection = PushConnection(configurationID: session.id, backendID: overview.backendId, address: session.configuration.address)
            next.connections.removeAll { $0.configurationID == session.id }
            next.connections.append(connection)
            var binding = next.bindings.first { $0.backendID == overview.backendId }
            let callsAvailable = session.capabilities?.contains("nativeCalls") == true && voipReady
            if binding == nil && ((allowed && registeredToken != nil) || callsAvailable) {
                let value = PushBinding(id: UUID().uuidString, backendID: overview.backendId, sendToken: PushIdentity.credential())
                let record = PushDecryptionKey(backendID: overview.backendId, key: SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) })
                try persistence.write("key-" + value.id, JSONEncoder().encode(record), true)
                next.bindings.append(value)
                binding = value
            }
            try persist(next)
            // A reset backend at the same URL is a different data owner.
            try removeInvalidConnections(store)
            merge(MessageBadge(backendId: overview.backendId, revision: overview.revision, unreadCount: overview.unreadCount))
            guard backends.insert(overview.backendId).inserted else { failures[session.id] = nil; return }
            guard var binding, let identity else { failures[session.id] = nil; return }
            binding.calls = binding.allowsCalls && callsAvailable
            binding.enabled = binding.enabled && (allowed || binding.allowsCalls)
            binding.sms = binding.sms && allowed
            binding.reminders = binding.reminders && allowed
            guard !binding.enabled || voipReady || (deviceToken != nil && registeredToken == deviceToken) else { return }
            let stamp = "\(binding.enabled)|\(binding.sms)|\(binding.reminders)|\(binding.allowsCalls)|\(session.id)|\(session.accessRevision)|\(session.configuration.name)"
            let remote = overview.subscriptions.first { $0.id == binding.id }
            if registrationStamps[binding.id] != stamp || remote == nil || remote?.enabled != binding.enabled || remote?.sms != binding.sms || remote?.reminders != binding.reminders || (remote?.calls ?? false) != binding.allowsCalls {
                guard canContactGateway else { return }
                _ = try await gateway.register(binding: binding, identity: identity, backendID: session.id)
                try Task.checkCancellation()
                guard let keyData = try persistence.read("key-" + binding.id, true) else { throw PushPayloadError.missingKey }
                let key = try JSONDecoder().decode(PushDecryptionKey.self, from: keyData)
                let registration = NativePushRegistration(id: binding.id, installationId: identity.id, label: UIDevice.current.name,
                    backendName: session.configuration.name, sendToken: binding.sendToken, encryptionKey: key.key.base64EncodedString(),
                    sms: binding.sms, reminders: binding.reminders, enabled: binding.enabled, calls: binding.allowsCalls)
                try await session.protectedRequest { api, token in try await api.registerPush(registration, token: token) }
                registrationStamps[binding.id] = stamp
            }
            registered.insert(binding.id)
            failures[session.id] = nil
        } catch {
            if !Task.isCancelled { failures[session.id] = recordFailure(error) }
        }
    }

    func removeInvalidConnections(_ store: BackendStore?) throws {
        guard let store, var next = identity else { return }
        next.connections.removeAll { connection in
            store.sessions[connection.configurationID]?.configuration.address != connection.address
        }
        // Retain address-to-backend identity for notification navigation through login.
        // Only the delivery grant is revoked when credentials expire or are removed.
        let removed = next.bindings.filter { binding in
            !next.connections.contains { connection in
                guard connection.backendID == binding.backendID, let session = store.sessions[connection.configurationID] else { return false }
                return session.authentication != .required
            }
        }
        next.bindings.removeAll { binding in removed.contains { $0.id == binding.id } }
        for binding in removed where !next.revocations.contains(binding.id) { next.revocations.append(binding.id) }
        for binding in removed where !(next.retiredKeys ?? []).contains(binding.id) {
            next.retiredKeys = (next.retiredKeys ?? []) + [binding.id]
        }
        try persist(next)
        for binding in removed {
            badges[binding.backendID] = nil
            registered.remove(binding.id)
            registrationStamps[binding.id] = nil
        }
        if !removed.isEmpty { applyBadge() }
        try retireKeys()
    }

    private func retireKeys() throws {
        guard var next = identity else { return }
        for id in next.retiredKeys ?? [] {
            guard let data = try persistence.read("key-" + id, true) else {
                next.retiredKeys?.removeAll { $0 == id }
                continue
            }
            var record = try JSONDecoder().decode(PushDecryptionKey.self, from: data)
            if next.connections.contains(where: { $0.backendID == record.backendID }) {
                if record.allowsPreview != false {
                    record.allowsPreview = false
                    try persistence.write("key-" + id, JSONEncoder().encode(record), true)
                }
            } else {
                try persistence.write("key-" + id, nil, true)
                next.retiredKeys?.removeAll { $0 == id }
            }
        }
        try persist(next)
    }

    private func revokePending() async throws {
        guard canContactGateway, let identity else { return }
        for id in identity.revocations {
            do { try await gateway.revoke(id, identity: identity) }
            catch let error as BackendFailure where error.source == .pushGateway && error.status == 410 && error.code == "push_token_unregistered" { /* Confirmed removed. */ }
            try Task.checkCancellation()
            guard var next = self.identity else { return }
            next.revocations.removeAll { $0 == id }
            try persist(next)
        }
    }

    private func persist(_ value: PushIdentity) throws {
        guard identity != value else { return }
        try persistence.write("installation", JSONEncoder().encode(value), false)
        identity = value
    }

    private func merge(_ result: GatewayRegistration) {
        for badge in result.backends ?? [] { merge(badge) }
    }

    private func merge(_ badge: MessageBadge) {
        if badge.revision >= (badges[badge.backendId]?.revision ?? -1) { badges[badge.backendId] = badge }
    }

    private func applyBadge() {
        guard !disabled else { return }
        let count = allowed ? (identity?.bindings ?? []).filter { $0.enabled && $0.sms }.reduce(0) { $0 + (badges[$1.backendID]?.unreadCount ?? 0) } : 0
        badgeTask?.cancel()
        badgeTask = Task {
            guard !Task.isCancelled else { return }
            do { try await notificationAccess.setBadge(count) }
            catch { if !Task.isCancelled { failure = recordFailure(error) } }
        }
    }
}

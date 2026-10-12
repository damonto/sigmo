import Foundation
import Observation

enum BackendLicense: String { case unknown, community, authorized, activation, unavailable }
enum BackendAuthentication: String { case unknown, notRequired, required, authenticated }

@MainActor @Observable
final class BackendSession: Identifiable {
    private(set) var configuration: BackendConfiguration
    nonisolated let id: UUID
    private(set) var license: BackendLicense = .unknown
    private(set) var authentication: BackendAuthentication = .unknown
    private(set) var information: AppInformation?
    private(set) var licenseDeviceID = ""
    private(set) var capabilities: Set<String>?
    private(set) var capabilityFailure: BackendFailure?
    private(set) var modems: [Modem] = []
    private(set) var unreadCounts: [String: Int] = [:]
    private(set) var hasLoadedModems = false
    private(set) var lastRefresh: Date?
    private(set) var failure: BackendFailure?
    private(set) var isRefreshing = false
    private(set) var isSendingOTP = false
    private(set) var isVerifyingOTP = false
    private(set) var authFailure: BackendFailure?
    private(set) var resendAvailableAt = Date.distantPast
    private(set) var pairing: LicensePairing?
    private(set) var pairingFailure: BackendFailure?
    private(set) var isCreatingPairing = false
    private(set) var waitingForRestart = false
    private(set) var detailFailures: [String: BackendFailure] = [:]
    private(set) var refreshingDetails: Set<String> = []
    private(set) var nextRefresh = Date.distantPast

    @ObservationIgnored private let api: BackendAPI
    @ObservationIgnored private let credentials: BackendCredentials
    @ObservationIgnored private let clock: BackendClock
    @ObservationIgnored private let gate: RefreshGate
    @ObservationIgnored private var token: String?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    @ObservationIgnored private var actionTasks: [UUID: Task<Void, Never>] = [:]
    @ObservationIgnored private var detailTasks: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var failures = 0
    @ObservationIgnored private var pendingCredentialRemoval = false
    @ObservationIgnored private var protectedCancellations: [UUID: () -> Void] = [:]
    @ObservationIgnored private var callCancellations: [UUID: () -> Void] = [:]
    private(set) var accessRevision = 0
    @ObservationIgnored private var deviceRevision = 0
    @ObservationIgnored private var unreadRevision: Int64 = -1
    @ObservationIgnored private var unreadRequest = 0

    init(configuration: BackendConfiguration, credentials: BackendCredentials = .keychain,
         api: BackendAPI? = nil, clock: BackendClock = BackendClock(), gate: RefreshGate = RefreshGate()) {
        self.id = configuration.id
        self.configuration = configuration
        self.credentials = credentials
        self.api = api ?? BackendAPI(configuration: configuration)
        self.clock = clock
        self.gate = gate
    }

    var statusKey: String {
        if let failure { return failure.isNetwork ? "status.offline" : "status.unavailable" }
        if license == .activation { return "status.activationRequired" }
        if authentication == .required { return "status.loginRequired" }
        if isOnline { return "status.online" }
        return isRefreshing ? "status.connecting" : "status.notConnected"
    }

    var activationReady: Bool {
        (license == .authorized || license == .community)
            && information != nil && !isRefreshing && !waitingForRestart
    }

    var foreground = true

    var canAccessDevices: Bool {
        (license == .community || license == .authorized)
            && (authentication == .authenticated || authentication == .notRequired)
    }

    // A refresh keeps a usable snapshot online until it fails or access is revoked.
    var isOnline: Bool {
        canAccessDevices && hasLoadedModems && failure == nil
    }

    func rename(_ name: String) {
        configuration.name = name
    }

    func refresh(force: Bool = false) async {
        guard !Task.isCancelled else { return }
        if let refreshTask { await refreshTask.value; return }
        guard force || clock.now() >= nextRefresh else { return }
        let current = generation
        isRefreshing = true
        let task = Task { [weak self] in
            guard let self else { return }
            do {
                try await gate.run { await self.load(current) }
            } catch { /* Cancellation of queued refresh work has no user-visible error. */ }
            if current == generation {
                isRefreshing = false
                refreshTask = nil
            }
        }
        refreshTask = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    private func load(_ current: Int) async {
        var checkingLicense = true
        do {
            let status = try await api.license()
            try check(current)
            checkingLicense = false
            if let status {
                licenseDeviceID = status.deviceId
                if !status.authorized {
                    license = .activation
                    clearDevices()
                    capabilities = nil
                    capabilityFailure = nil
                    information = nil
                    authentication = .unknown
                    failure = nil
                    finishRefresh()
                    return
                }
            }
            // A 404 is only a community server after the build-info contract succeeds.
            let app = try await api.app()
            try check(current)
            information = app
            if status == nil { license = .community; licenseDeviceID = "" }
            else { license = .authorized }
            waitingForRestart = false
            do {
                let features = try await api.capabilities()
                try check(current)
                capabilities = features
                capabilityFailure = nil
            } catch {
                try check(current)
                capabilities = nil
                capabilityFailure = .wrapping(error)
            }
            let required = try await api.otpRequirement()
            try check(current)
            if pendingCredentialRemoval {
                try credentials.write(id, nil)
                pendingCredentialRemoval = false
            }
            token = required ? try credentials.read(id) : nil
            authentication = required ? (token == nil ? .required : .authenticated) : .notRequired
            if required && token == nil {
                clearDevices()
                failure = nil
                finishRefresh()
                return
            }
            do {
                let revision = deviceRevision
                let devices = try await api.modems(token: token)
                try check(current)
                guard Set(devices.map(\.id)).count == devices.count else {
                    throw BackendFailure.local("api.invalidResponse")
                }
                if revision == deviceRevision { modems = devices }
                unreadCounts = unreadCounts.filter { id, _ in modems.contains { $0.id == id } }
                hasLoadedModems = true
                detailFailures = detailFailures.filter { id, _ in modems.contains { $0.id == id } }
                lastRefresh = clock.now()
                failure = nil
                finishRefresh()
                await refreshUnreadCounts()
            } catch {
                try check(current)
                if (error as? BackendFailure)?.isLocalAuthenticationFailure == true {
                    revokeAccess()
                    return
                }
                throw error
            }
        } catch {
            guard isCurrent(current) else { return }
            if checkingLicense { license = .unavailable }
            failure = .wrapping(error)
            failures += 1
            nextRefresh = clock.now().addingTimeInterval([30.0, 60, 120][min(failures - 1, 2)])
        }
    }

    private func finishRefresh() {
        failures = 0
        nextRefresh = clock.now().addingTimeInterval(30)
    }

    func stop() {
        generation += 1
        refreshTask?.cancel()
        refreshTask = nil
        for task in actionTasks.values { task.cancel() }
        actionTasks.removeAll()
        for cancel in protectedCancellations.values { cancel() }
        protectedCancellations.removeAll()
        for task in detailTasks.values { task.cancel() }
        detailTasks.removeAll()
        refreshingDetails.removeAll()
        isRefreshing = false
        isSendingOTP = false
        isVerifyingOTP = false
        isCreatingPairing = false
    }

    func removeCredentials() throws {
        stop()
        accessRevision += 1
        cancelCallRequests()
        token = nil
        pendingCredentialRemoval = true
        clearDevices()
        authentication = .required
        try credentials.write(id, nil)
        pendingCredentialRemoval = false
    }

    func logout() {
        do {
            try removeCredentials()
            authFailure = nil
            failure = nil
        } catch { authFailure = .wrapping(error) }
    }

    private func revokeAccess() {
        logout()
        // Keychain failures must stay visible; no stale token is reused in memory.
        nextRefresh = clock.now().addingTimeInterval(30)
    }

    // Cancel only the previous refresh. Unrelated settings operations keep their
    // own lifetime, while this refresh must observe the just-committed settings.
    func refreshAfterSettingsChange() async {
        guard !Task.isCancelled else { return }
        if let refreshTask {
            refreshTask.cancel()
            await refreshTask.value
        }
        guard !Task.isCancelled else { return }
        await refresh(force: true)
    }

    // Settings share this session's credential and invalidation boundary. A late
    // response after logout or address changes is never published.
    func protectedRequest<Value: Sendable>(
        _ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Value
    ) async throws -> Value {
        try Task.checkCancellation()
        guard canAccessDevices else { throw BackendFailure.local("status.loginRequired") }
        let current = generation
        let requestID = UUID()
        let api = api
        let token = token
        let task = Task {
            try Task.checkCancellation()
            return try await operation(api, token)
        }
        protectedCancellations[requestID] = { task.cancel() }
        defer { protectedCancellations[requestID] = nil }
        do {
            let value = try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
            try check(current)
            guard canAccessDevices else { throw CancellationError() }
            return value
        } catch {
            try check(current)
            if (error as? BackendFailure)?.isLocalAuthenticationFailure == true { revokeAccess() }
            throw error
        }
    }

    private func clearDevices() {
        modems = []
        unreadCounts = [:]
        unreadRevision = -1
        unreadRequest += 1
        hasLoadedModems = false
        lastRefresh = nil
        detailFailures = [:]
    }

    // Only an unfiltered snapshot can replace all module counts. The response's
    // top-level unreadCount belongs to the entire Sigmo, including unassigned history.
    func refreshUnreadCounts(minimumRevision: Int64? = nil) async {
        guard !Task.isCancelled, canAccessDevices, hasLoadedModems else { return }
        if let minimumRevision { unreadRevision = max(unreadRevision, minimumRevision) }
        unreadRequest += 1
        let request = unreadRequest
        let current = generation
        do {
            let snapshot = try await protectedRequest { api, token in
                try await api.conversations(modemID: nil, token: token)
            }
            guard isCurrent(current), request == unreadRequest, snapshot.revision >= unreadRevision else { return }
            let ids = Set(modems.map(\.id))
            var counts: [String: Int] = [:]
            for conversation in snapshot.items where ids.contains(conversation.modemId) {
                counts[conversation.modemId, default: 0] += max(0, conversation.unreadCount)
            }
            unreadCounts = counts
            unreadRevision = snapshot.revision
        } catch {
            guard isCurrent(current), request == unreadRequest else { return }
            if let failure = error as? BackendFailure, failure.status == 404 {
                unreadCounts = [:]
                unreadRevision = -1
            }
            // An unavailable badge must not hide successfully loaded devices.
            // Transient errors retain the last confirmed counts; legacy servers omit them.
        }
    }

    // Calls follow the credential lifetime instead of the refresh generation.
    // Authentication changes cancel them and reject late cross-account results.
    func callRequest<Value: Sendable>(
        _ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Value
    ) async throws -> Value {
        try Task.checkCancellation()
        guard canAccessDevices else { throw BackendFailure.local("status.loginRequired") }
        let revision = accessRevision
        let requestID = UUID()
        let api = api
        let token = token
        let task = Task { try await operation(api, token) }
        callCancellations[requestID] = { task.cancel() }
        defer { callCancellations[requestID] = nil }
        do {
            let value = try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
            try Task.checkCancellation()
            guard accessRevision == revision, canAccessDevices else { throw CancellationError() }
            return value
        } catch {
            try Task.checkCancellation()
            if accessRevision == revision, (error as? BackendFailure)?.isLocalAuthenticationFailure == true { revokeAccess() }
            throw error
        }
    }

    private func cancelCallRequests() {
        for cancel in callCancellations.values { cancel() }
        callCancellations.removeAll()
    }

    func sendOTP() async {
        guard !Task.isCancelled, !isSendingOTP, clock.now() >= resendAvailableAt, authentication == .required else { return }
        isSendingOTP = true
        authFailure = nil
        await action { current in
            defer { if current == self.generation { self.isSendingOTP = false } }
            do {
                try await self.api.sendOTP()
                try self.check(current)
                self.resendAvailableAt = self.clock.now().addingTimeInterval(60)
            } catch {
                guard self.isCurrent(current) else { return }
                let failure = BackendFailure.wrapping(error)
                if failure.status == 429 { self.resendAvailableAt = self.clock.now().addingTimeInterval(60) }
                self.authFailure = failure
            }
        }
    }

    func verifyOTP(_ code: String) async {
        guard !Task.isCancelled, !isVerifyingOTP, code.count == 6, code.utf8.allSatisfy({ (48...57).contains($0) }) else { return }
        // Cancel earlier reads before a new credential can become authoritative.
        stop()
        isVerifyingOTP = true
        authFailure = nil
        await action { current in
            defer { if current == self.generation { self.isVerifyingOTP = false } }
            do {
                let token = try await self.api.verifyOTP(code)
                try self.check(current)
                try self.credentials.write(self.id, token)
                self.token = token
                self.pendingCredentialRemoval = false
                self.authentication = .authenticated
                await self.refresh(force: true)
            } catch {
                guard self.isCurrent(current) else { return }
                self.authFailure = .wrapping(error)
            }
        }
    }

    // Invalidate reads begun before a write without cancelling other protected operations.
    func invalidateModemReads(_ id: String) async {
        deviceRevision += 1
        if let task = detailTasks[id] {
            task.cancel()
            await task.value
        }
    }

    func refreshModem(_ id: String) async {
        guard !Task.isCancelled, canAccessDevices else { return }
        if let task = detailTasks[id] { await task.value; return }
        let current = generation
        let revision = deviceRevision
        refreshingDetails.insert(id)
        let task = Task { [weak self] in
            guard let self else { return }
            defer {
                if current == generation {
                    refreshingDetails.remove(id)
                    detailTasks[id] = nil
                }
            }
            do {
                let device = try await gate.run { try await self.api.modem(id, token: self.token) }
                try check(current)
                guard revision == deviceRevision else { return }
                if let index = modems.firstIndex(where: { $0.id == id }) { modems[index] = device }
                detailFailures[id] = nil
            } catch {
                guard isCurrent(current), revision == deviceRevision else { return }
                let failure = BackendFailure.wrapping(error)
                if failure.isLocalAuthenticationFailure { revokeAccess(); return }
                if failure.status == 404 {
                    modems.removeAll { $0.id == id }
                    unreadCounts[id] = nil
                }
                detailFailures[id] = failure
            }
        }
        detailTasks[id] = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    func createPairing() async {
        guard !Task.isCancelled, !isCreatingPairing, license == .activation else { return }
        isCreatingPairing = true
        pairingFailure = nil
        await action { current in
            defer { if current == self.generation { self.isCreatingPairing = false } }
            do {
                let result = try await self.api.createPairing()
                try self.check(current)
                guard result.expiresAt > self.clock.now() else { throw BackendFailure.local("activation.expired") }
                self.pairing = result
                self.waitingForRestart = false
            } catch {
                guard self.isCurrent(current) else { return }
                if await self.recoverActivation(current) { return }
                guard self.isCurrent(current) else { return }
                self.pairingFailure = .wrapping(error)
            }
        }
    }

    // Finish the current poll on backgrounding; only start new polls in the foreground.
    func observePairing() async {
        await action { current in
            while self.foreground, self.isCurrent(current), self.license == .activation || self.waitingForRestart {
                if self.pairing != nil || self.waitingForRestart {
                    await self.pollPairing(current)
                }
                do { try await self.clock.sleep(.seconds(2)) }
                catch { return }
            }
        }
    }

    private func pollPairing(_ current: Int) async {
        if waitingForRestart {
            _ = await recoverActivation(current)
            return
        }
        guard let pairing, pairing.status == "pending" else { return }
        if pairing.expiresAt <= clock.now() {
            if await recoverActivation(current) { return }
            guard isCurrent(current) else { return }
            self.pairing?.status = "expired"
            return
        }
        do {
            let result = try await api.pairing(pairing.id)
            try check(current)
            self.pairing = result
            pairingFailure = nil
            if result.status == "active" {
                waitingForRestart = true
                _ = await recoverActivation(current)
            }
        } catch {
            guard isCurrent(current) else { return }
            // The success response can be lost as the activation server restarts.
            if await recoverActivation(current) { return }
            guard isCurrent(current) else { return }
            let failure = BackendFailure.wrapping(error)
            pairingFailure = failure
            if ["license_pairing_expired", "license_pairing_not_found"].contains(failure.code) {
                self.pairing?.status = "expired"
            }
        }
    }

    private func recoverActivation(_ current: Int) async -> Bool {
        do {
            guard let status = try await api.license(), status.authorized else { return false }
            let app = try await api.app()
            try check(current)
            information = app
            license = .authorized
            licenseDeviceID = status.deviceId
            waitingForRestart = false
            pairingFailure = nil
            await refresh(force: true)
            return true
        } catch { return false }
    }

    private func action(_ operation: @escaping @MainActor (Int) async -> Void) async {
        let id = UUID()
        let current = generation
        let task = Task { await operation(current) }
        actionTasks[id] = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
        actionTasks[id] = nil
    }

    private func isCurrent(_ value: Int) -> Bool { value == generation && !Task.isCancelled }
    private func check(_ value: Int) throws {
        guard isCurrent(value) else { throw CancellationError() }
    }
}

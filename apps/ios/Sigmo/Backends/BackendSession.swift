import Foundation
import Observation

enum BackendLicense: String { case unknown, community, authorized, activation, unavailable }
enum BackendAuthentication: String { case unknown, notRequired, required, authenticated }

@MainActor @Observable
final class BackendSession: Identifiable {
    private(set) var savedContacts: [SigmoContact] = [] {
        didSet { contactIndex = ContactIndex(savedContacts) }
    }
    @ObservationIgnored private var contactIndex = ContactIndex([])
    @ObservationIgnored private var contactsRevision = 0
    @ObservationIgnored private var contactsTask: Task<Void, Error>?
    @ObservationIgnored private var contactsLoadedAt = Date.distantPast
    @ObservationIgnored private var linesTask: Task<[CommunicationLine], Error>?
    @ObservationIgnored private var linesRevision = 0
    @ObservationIgnored private var cachedLines: [CommunicationLine] = []
    @ObservationIgnored private var linesLoadedAt = Date.distantPast
    private(set) var communicationBackendID = ""
    private(set) var configuration: BackendConfiguration
    nonisolated let id: UUID
    // A saved connection ID survives endpoint changes; a live session never does.
    nonisolated let incarnationID = UUID()
    private(set) var license: BackendLicense = .unknown
    var isDemo: Bool { api.isDemo }
    private(set) var authentication: BackendAuthentication = .unknown {
        didSet {
            if authentication != oldValue {
                AppDiagnostics.record("authentication.changed", category: "auth", fields: ["backendID": id.uuidString, "state": authentication.rawValue])
            }
        }
    }
    private(set) var information: AppInformation?
    private(set) var licenseDeviceID = ""
    private(set) var capabilities: Set<String>?
    private(set) var capabilityFailure: BackendFailure? {
        didSet {
            if let capabilityFailure, capabilityFailure != oldValue {
                AppDiagnostics.record(capabilityFailure, category: "backend", operation: "capabilities.failed", backendID: id)
            }
        }
    }
    private(set) var modems: [Modem] = []
    private(set) var unreadCounts: [String: Int] = [:]
    private(set) var hasLoadedModems = false
    private(set) var lastRefresh: Date?
    private(set) var failure: BackendFailure? {
        didSet {
            if let failure, failure != oldValue {
                AppDiagnostics.record(failure, category: "backend", operation: "refresh.failed", backendID: id)
            }
        }
    }
    private(set) var isRefreshing = false
    private(set) var isSendingOTP = false
    private(set) var isVerifyingOTP = false
    private(set) var authFailure: BackendFailure? {
        didSet {
            if let authFailure, authFailure != oldValue {
                AppDiagnostics.record(authFailure, category: "auth", operation: "authentication.failed", backendID: id)
            }
        }
    }
    private(set) var authMethods: AuthMethods?
    private(set) var isBrowserLogin = false
    private(set) var isLoggingOut = false
    var supportsOTP: Bool { authMethods?.mode == "otp" }
    var supportsBrowserLogin: Bool { accessChallenge != nil || authMethods?.mode == "oidc" }
    @ObservationIgnored private var accessChallenge: AccessChallenge?
    @ObservationIgnored private var otpChallenge: String?
    @ObservationIgnored private var credential: SessionCredential?
    @ObservationIgnored private var credentialRefresh: Task<String?, any Error>?
    @ObservationIgnored private var browserLoginTask: Task<Void, Never>?
    @ObservationIgnored private var logoutTask: Task<Void, Never>?
    @ObservationIgnored private let browser: any BrowserAuthenticating
    @ObservationIgnored private let managedAccess: ManagedAccess
    private(set) var resendAvailableAt = Date.distantPast
    private(set) var pairing: LicensePairing?
    private(set) var pairingFailure: BackendFailure? {
        didSet {
            if let pairingFailure, pairingFailure != oldValue {
                AppDiagnostics.record(pairingFailure, category: "auth", operation: "pairing.failed", backendID: id)
            }
        }
    }
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
         api: BackendAPI? = nil, clock: BackendClock = BackendClock(), gate: RefreshGate = RefreshGate(),
         browser: any BrowserAuthenticating = BrowserAuthentication(), managedAccess: ManagedAccess? = nil) {
        self.id = configuration.id
        self.configuration = configuration
        self.browser = browser
        self.managedAccess = managedAccess ?? ManagedAccess(backendID: configuration.id)
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
        // Browser presentation can reactivate the scene. An unauthenticated
        // discovery request would revoke access and cancel that same login.
        guard !isBrowserLogin else { return }
        await refreshConnection(force: force)
    }

    private func refreshConnection(force: Bool) async {
        guard !Task.isCancelled, !isLoggingOut else { return }
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
            if pendingCredentialRemoval {
                try credentials.write(id, nil)
                pendingCredentialRemoval = false
            }
            if credential == nil, let saved = try credentials.read(id) {
                credential = try JSONDecoder().decode(SessionCredential.self, from: Data(saved.utf8))
            }
            token = try await freshToken()
            let methods = try await api.authMethods(token: token)
            try check(current)
            authMethods = methods
            // The origin's authentication mode does not describe its Access edge.
            // Keep managed OAuth credentials even when Sigmo itself allows anonymous access.
            let usesManagedAccess = credential?.access != nil
            if methods.mode == "none", !usesManagedAccess {
                try credentials.write(id, nil)
                credential = nil
                token = nil
            }
            if methods.mode != "cloudflare_access", !usesManagedAccess { accessChallenge = nil }
            authentication = methods.mode == "none" && !usesManagedAccess ? .notRequired : (token == nil ? .required : .authenticated)
            let status = try await api.license(token: token)
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
            let app = try await api.app(token: token)
            try check(current)
            information = app
            if status == nil { license = .community; licenseDeviceID = "" }
            else { license = .authorized }
            waitingForRestart = false
            do {
                let features = try await api.capabilities(token: token)
                try check(current)
                capabilities = features
                capabilityFailure = nil
            } catch {
                try check(current)
                capabilities = nil
                capabilityFailure = .wrapping(error)
            }
            if authentication == .required {
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
                throw error
            }
        } catch {
            guard isCurrent(current) else { return }
            if handleAuthenticationFailure(error) {
                failure = nil
                finishRefresh()
                return
            }
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
        cancelBrowserLogin()
        credentialRefresh?.cancel()
        credentialRefresh = nil
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
        discardCredentials()
        pendingCredentialRemoval = true
        try credentials.write(id, nil)
        pendingCredentialRemoval = false
    }

    // Configuration commits use this after removing credentials from storage.
    // Keeping invalidation separate permits a failed file write to restore the
    // Keychain item without interrupting the still-configured live session.
    func discardCredentials() {
        stop()
        accessRevision += 1
        cancelCallRequests()
        token = nil
        credential = nil
        pendingCredentialRemoval = false
        clearDevices()
        authentication = .required
    }

    func logout() async {
        if let logoutTask { await logoutTask.value; return }
        isLoggingOut = true
        // The session owns revocation even when publishing the logged-out state
        // removes the initiating button, or the caller leaves the page.
        let task = Task {
            await performLogout()
            isLoggingOut = false
            logoutTask = nil
        }
        logoutTask = task
        await task.value
    }

    private func performLogout() async {
        var saved = credential
        // Stop operations immediately. An expired access token must be refreshed
        // before revocation, otherwise its still-valid refresh token would survive logout.
        stop()
        let current = generation
        cancelCallRequests()
        authentication = .required
        clearDevices()
        do {
            if let current = saved, current.access == nil {
                if let expiry = current.sessionExpiresAt, expiry <= clock.now() { saved = nil }
                else { _ = try await freshToken(); saved = credential }
            }
        } catch {
            guard current == generation else { return }
            let remoteFailure = BackendFailure.wrapping(error)
            do { try removeCredentials() } catch { authFailure = .wrapping(error); return }
            authFailure = remoteFailure
            return
        }
        guard current == generation else { return }
        do { try removeCredentials() }
        catch { authFailure = .wrapping(error); return }
        let revision = accessRevision
        do {
            if let saved {
                if saved.access != nil { try await managedAccess.revoke(saved) }
                else { try await api.revokeSession(token: saved.accessToken) }
            }
            guard revision == accessRevision else { return }
            authFailure = nil
            failure = nil
        } catch {
            guard revision == accessRevision else { return }
            let remoteFailure = BackendFailure.wrapping(error)
            authFailure = remoteFailure
        }
    }

    private func revokeAccess() {
        do { try removeCredentials(); authFailure = nil }
        catch { authFailure = .wrapping(error) }
        nextRefresh = clock.now().addingTimeInterval(30)
    }

    @discardableResult
    private func handleAuthenticationFailure(_ error: Error) -> Bool {
        if let challenge = error as? AccessChallenge {
            accessChallenge = challenge
        } else if (error as? BackendFailure)?.isLocalAuthenticationFailure != true {
            return false
        }
        revokeAccess()
        failure = nil
        return true
    }

    private func saveCredential(_ value: SessionCredential) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let data = try encoder.encode(value)
        try credentials.write(id, String(decoding: data, as: UTF8.self))
        credential = value
        token = value.accessToken
        pendingCredentialRemoval = false
    }

    private func freshToken() async throws -> String? {
        if let credentialRefresh { return try await credentialRefresh.value }
        guard let saved = credential else { return nil }
        if saved.expiresAt.timeIntervalSince(clock.now()) > 60 { return saved.accessToken }
        let current = generation
        let task = Task<String?, any Error> {
            guard saved.sessionExpiresAt == nil || saved.sessionExpiresAt! > clock.now(),
                  let refresh = saved.refreshToken else { throw BackendFailure.local("status.loginRequired") }
            let next: SessionCredential
            if saved.access != nil { next = try await managedAccess.refresh(saved) }
            else { next = try await api.refreshSession(refresh).credential() }
            try check(current)
            try saveCredential(next)
            return next.accessToken
        }
        credentialRefresh = task
        defer { if current == generation { credentialRefresh = nil } }
        do { return try await task.value }
        catch {
            // A lost refresh response has an unknown outcome. Never replay a rotating credential.
            // A connection that could not be established did not rotate the
            // credential. Timeouts and lost connections still have unknown outcomes.
            if isCurrent(current), BackendFailure.wrapping(error).transportCode != .notConnectedToInternet {
                if !handleAuthenticationFailure(error) { revokeAccess() }
            }
            throw error
        }
    }

    func loginWithBrowser() async {
        guard !Task.isCancelled, !isLoggingOut, supportsBrowserLogin else { return }
        if let browserLoginTask { await browserLoginTask.value; return }
        stop()
        let current = generation
        isBrowserLogin = true
        authFailure = nil
        // The session owns login and its final refresh, even when authentication
        // changes remove the initiating button or the caller leaves the page.
        let task = Task { await performBrowserLogin(current) }
        browserLoginTask = task
        await task.value
    }

    func cancelBrowserLogin() {
        browserLoginTask?.cancel()
        browserLoginTask = nil
        browser.cancel()
        isBrowserLogin = false
    }

    private func performBrowserLogin(_ current: Int) async {
        defer {
            if current == generation {
                isBrowserLogin = false
                browserLoginTask = nil
            }
        }
        do {
            let proof = try LoginProof()
            let next: SessionCredential
            if let challenge = accessChallenge {
                let registration = try await managedAccess.register(challenge, backend: api.address)
                try check(current)
                let callback = try await browser.open(managedAccess.authorizationURL(registration, proof: proof))
                try check(current)
                let code = try proof.code(from: callback)
                next = try await managedAccess.exchange(registration, code: code, proof: proof)
            } else {
                let target = try await api.browserLogin(proof)
                try check(current)
                let callback = try await browser.open(target)
                try check(current)
                let code = try proof.code(from: callback)
                next = try await api.completeBrowserLogin(code: code, proof: proof).credential()
            }
            try check(current)
            try saveCredential(next)
            authentication = .authenticated
            await refreshConnection(force: true)
        } catch is CancellationError {
            // Closing the browser leaves the connection ready for another login.
        } catch {
            if isCurrent(current) { authFailure = .wrapping(error) }
        }
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
        let token = try await freshToken()
        try check(current)
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
            if handleAuthenticationFailure(error), error is AccessChallenge {
                throw BackendFailure.local("status.loginRequired")
            }
            throw error
        }
    }

    private func clearDevices() {
        clearCommunicationCache()
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
            guard isCurrent(current), request == unreadRequest else { return }
            if communicationBackendID != snapshot.backendId {
                communicationBackendID = snapshot.backendId
                clearCommunicationCache()
                unreadRevision = -1
            }
            guard snapshot.revision >= unreadRevision else { return }
            let ids = Set(modems.map(\.id))
            var counts: [String: Int] = [:]
            for conversation in snapshot.items where ids.contains(conversation.modemId) {
                counts[conversation.modemId, default: 0] += max(0, conversation.unreadCount)
            }
            unreadCounts = counts
            unreadRevision = snapshot.revision
        } catch {
            // Retain confirmed counts when the badge request fails. Authentication
            // revocation clears device data through protectedRequest.
            // An unavailable badge must not hide successfully loaded devices.
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
        let token = try await freshToken()
        guard accessRevision == revision else { throw CancellationError() }
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
            if accessRevision == revision, handleAuthenticationFailure(error), error is AccessChallenge {
                throw BackendFailure.local("status.loginRequired")
            }
            throw error
        }
    }

    private func cancelCallRequests() {
        for cancel in callCancellations.values { cancel() }
        callCancellations.removeAll()
    }

    func sendOTP() async {
        guard !Task.isCancelled, !isLoggingOut, !isSendingOTP, supportsOTP, clock.now() >= resendAvailableAt, authentication == .required else { return }
        isSendingOTP = true
        authFailure = nil
        await action { current in
            defer { if current == self.generation { self.isSendingOTP = false } }
            do {
                let challenge = try await self.api.sendOTP()
                try self.check(current)
                self.otpChallenge = challenge.id
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
        guard !Task.isCancelled, !isLoggingOut, !isVerifyingOTP, let challenge = otpChallenge, supportsOTP, code.count == 6, code.utf8.allSatisfy({ (48...57).contains($0) }) else { return }
        // Cancel earlier reads before a new credential can become authoritative.
        stop()
        isVerifyingOTP = true
        authFailure = nil
        await action { current in
            defer { if current == self.generation { self.isVerifyingOTP = false } }
            do {
                let tokens = try await self.api.verifyOTP(code, challenge: challenge)
                try self.check(current)
                try self.saveCredential(tokens.credential())
                self.otpChallenge = nil
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
                let token = try await freshToken()
                let device = try await gate.run { try await self.api.modem(id, token: token) }
                try check(current)
                guard revision == deviceRevision else { return }
                if let index = modems.firstIndex(where: { $0.id == id }) { modems[index] = device }
                detailFailures[id] = nil
            } catch {
                guard isCurrent(current), revision == deviceRevision else { return }
                if handleAuthenticationFailure(error) { return }
                let failure = BackendFailure.wrapping(error)
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
                let token = try await self.freshToken()
                let result = try await self.api.createPairing(token: token)
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
            let token = try await freshToken()
            let result = try await api.pairing(pairing.id, token: token)
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
            let token = try await freshToken()
            guard let status = try await api.license(token: token), status.authorized else { return false }
            let app = try await api.app(token: token)
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

extension BackendSession {
    func clearCommunicationCache() {
        clearContactCache()
        invalidateCommunicationLines()
    }

    private func clearContactCache() {
        contactsRevision += 1
        contactsTask?.cancel()
        contactsTask = nil
        contactsLoadedAt = .distantPast
        savedContacts = []
    }
    func invalidateCommunicationLines() {
        linesRevision += 1
        linesTask?.cancel()
        linesTask = nil
        cachedLines = []
        linesLoadedAt = .distantPast
    }
    func refreshContacts(force: Bool = false) async throws {
        guard canAccessDevices else {
            clearContactCache()
            return
        }
        if !force, let task = contactsTask { return try await task.value }
        if !force, clock.now().timeIntervalSince(contactsLoadedAt) < 15 { return }
        contactsTask?.cancel()
        contactsRevision += 1
        let request = contactsRevision
        let revision = accessRevision
        let task = Task { @MainActor in
            defer { if request == contactsRevision { contactsTask = nil } }
            let result = try await protectedRequest { api, token in try await api.contacts(token: token) }
            guard !Task.isCancelled, revision == accessRevision, request == contactsRevision,
                  canAccessDevices else { throw CancellationError() }
            savedContacts = result
            contactsLoadedAt = clock.now()
        }
        contactsTask = task
        try await task.value
    }
    func communicationLines() async throws -> [CommunicationLine] {
        guard canAccessDevices else {
            invalidateCommunicationLines()
            throw CancellationError()
        }
        if let task = linesTask { return try await task.value }
        if clock.now().timeIntervalSince(linesLoadedAt) < 15 { return cachedLines }
        let revision = accessRevision, request = linesRevision
        let task = Task { @MainActor in
            defer { if request == linesRevision { linesTask = nil } }
            let result = try await protectedRequest { api, token in try await api.communicationLines(token: token, cached: true) }
            guard !Task.isCancelled, revision == accessRevision, request == linesRevision, canAccessDevices else { throw CancellationError() }
            cachedLines = result
            linesLoadedAt = clock.now()
            return result
        }
        linesTask = task
        return try await task.value
    }
    func contacts(matching number: String) -> [SigmoContact] {
        // Register the observable dependency; the derived index is not observed.
        _ = savedContacts
        return contactIndex.matches(number)
    }
    func contact(for number: String) -> SigmoContact? {
        let matches = contacts(matching: number)
        return matches.count == 1 ? matches[0] : nil
    }
}

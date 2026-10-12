import Foundation

// Connection routes address the current modem SIM, not a profile ID in the URL.
// Keep the originating SIM and access revision so a retained page cannot target its replacement.
@MainActor
struct ProfileConnectionScope {
    let detail: ModemDetailState
    let profileID: String
    let capability: String?
    private let sim: ModemSIMIdentity?
    private let accessRevision: Int

    init(detail: ModemDetailState, profileID: String, capability: String? = nil) {
        self.detail = detail
        self.profileID = profileID
        self.capability = capability
        sim = detail.simIdentity
        accessRevision = detail.session.accessRevision
    }

    var available: Bool {
        detail.session.canAccessDevices && detail.session.accessRevision == accessRevision
            && detail.simIdentity == sim && detail.isActiveProfile(profileID)
            && (capability.map { detail.session.capabilities?.contains($0) == true } ?? true)
    }
    var radioAvailable: Bool { available && detail.modem?.airplaneMode == false && detail.modem?.state != "locked" }
    var modemID: String { detail.modemID }

    func check() throws {
        try Task.checkCancellation()
        guard available else { throw CancellationError() }
    }

    func refreshModem() async throws {
        try check()
        await detail.session.refreshModem(modemID)
        try check()
        if let failure = detail.session.detailFailures[modemID] { throw failure }
    }

    func read<Value: Sendable>(_ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Value) async throws -> Value {
        try check()
        let value = try await detail.session.protectedRequest(operation)
        try check()
        return value
    }

    func write(_ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Void) async throws {
        try check()
        guard !detail.busy else { throw BackendFailure.local("connection.busy") }
        let completed = await detail.mutate(operation)
        try check()
        if !completed { throw detail.failure ?? BackendFailure.local("connection.refreshRequired") }
    }
}

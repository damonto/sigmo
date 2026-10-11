import Foundation

extension BackendAPI {
    func nativePush(token: String?) async throws -> NativePushOverview {
        try await decode(["push-subscriptions"], token: token)
    }

    func registerPush(_ registration: NativePushRegistration, token: String?) async throws {
        _ = try await request(["push-subscriptions"], method: "POST", body: JSONEncoder().encode(registration), token: token)
    }

    func removePush(_ id: String, token: String?) async throws {
        _ = try await request(["push-subscriptions", id], method: "DELETE", token: token)
    }
}

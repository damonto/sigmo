import UserNotifications
import XCTest
@testable import Sigmo

@MainActor
final class NativeCallPushTests: XCTestCase {
    func testGatewayRetryDeadlinePreservesBackendDiscovery() async throws {
        let fixture = CallPushRegistrationFixture()
        let time = TestTime()
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss 'GMT'"
        var limited = HTTPProblem.response(status: 429, code: "push_rate_limited", detail: "Wait")
        limited.retryAfter = formatter.string(from: time.now().addingTimeInterval(90))
        await fixture.setGatewayFailure(limited)
        let directory = URL.temporaryDirectory.appending(path: "PushRetry-\(UUID())")
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "backends.json")), credentials: MemoryCredentials().store,
            makeAPI: { BackendAPI(configuration: $0, transport: { try await fixture.backend($0) }) })
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Home", address: "https://calls.example"))
        await store.sessions[id]?.refresh(force: true)
        let access = PushNotificationAccess(status: { .denied }, request: {}, register: {}, setBadge: { _ in })
        let memory = PushMemoryPersistence()
        let push = NativePushState(identity: PushIdentity(id: "installation", managementToken: "test-credential"), persistence: memory.persistence,
            gateway: PushGateway(address: URL(string: "https://gateway.example"), transport: { try await fixture.gateway($0) }), notificationAccess: access, now: { time.now() })
        push.registeredVoIP(token: Data(repeating: 1, count: 32))
        await push.reconcile(store: store)
        XCTAssertEqual(push.backendID(sessionID: id), "fixture-call-backend")
        XCTAssertFalse(push.voipReady)
        XCTAssertEqual(push.voipFailure?.source, .pushGateway)
        let initial = await fixture.gatewayCount()
        XCTAssertEqual(initial, 1)
        time.advance(89)
        await push.reconcile(store: store)
        let waiting = await fixture.gatewayCount()
        XCTAssertEqual(waiting, initial)
        XCTAssertNil(push.failures[id])
        await fixture.setGatewayFailure(nil)
        time.advance(1)
        await push.reconcile(store: store)
        XCTAssertTrue(push.voipReady)
        XCTAssertFalse(push.registered.isEmpty)
    }

    func testMissingPushEndpointSurfacesBackendFailureAndCanRecover() async throws {
        let fixture = CallPushRegistrationFixture()
        await fixture.setBackendFailure(HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Missing endpoint"))
        let directory = URL.temporaryDirectory.appending(path: "PushEndpoint-\(UUID())")
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "backends.json")), credentials: MemoryCredentials().store,
            makeAPI: { BackendAPI(configuration: $0, transport: { try await fixture.backend($0) }) })
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Home", address: "https://calls.example"))
        await store.sessions[id]?.refresh(force: true)
        let access = PushNotificationAccess(status: { .denied }, request: {}, register: {}, setBadge: { _ in })
        let memory = PushMemoryPersistence()
        let push = NativePushState(identity: PushIdentity(id: "installation", managementToken: "test-credential"), persistence: memory.persistence,
            gateway: PushGateway(address: URL(string: "https://gateway.example"), transport: { try await fixture.gateway($0) }), notificationAccess: access)

        await push.reconcile(store: store)
        XCTAssertEqual(push.failures[id]?.status, 404)
        XCTAssertEqual(push.failures[id]?.code, "resource_not_found")
        XCTAssertEqual(push.failures[id]?.source, .backend)
        XCTAssertNil(push.backendID(sessionID: id))
        await fixture.setBackendFailure(nil)
        await push.reconcile(store: store)
        XCTAssertNil(push.failures[id])
        XCTAssertEqual(push.backendID(sessionID: id), "fixture-call-backend")
        let gatewayRequests = await fixture.gatewayCount()
        XCTAssertEqual(gatewayRequests, 0)
    }

    func testPendingNotificationPromptDoesNotBlockVoIPSubscription() async throws {
        let fixture = CallPushRegistrationFixture()
        let directory = URL.temporaryDirectory.appending(path: "CallPush-\(UUID())")
        let store = BackendStore(file: ConfigurationFile(url: directory.appending(path: "backends.json")), credentials: MemoryCredentials().store,
            makeAPI: { BackendAPI(configuration: $0, transport: { try await fixture.backend($0) }) })
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let id = UUID()
        XCTAssertTrue(store.saveBackend(id: id, name: "Home", address: "https://calls.example"))
        await store.sessions[id]?.refresh(force: true)
        let prompt = SettingsRequestGate()
        let opened = expectation(description: "ordinary permission prompt")
        var permission = UNAuthorizationStatus.notDetermined
        let access = PushNotificationAccess(status: { permission }, request: {
            opened.fulfill()
            await prompt.wait()
            permission = .denied
        }, register: { XCTFail("Ordinary notification registration requires authorization") }, setBadge: { _ in })
        let memory = PushMemoryPersistence()
        let push = NativePushState(identity: PushIdentity(id: "installation", managementToken: "test-credential"), persistence: memory.persistence,
            gateway: PushGateway(address: URL(string: "https://gateway.example"), transport: { try await fixture.gateway($0) }), notificationAccess: access)
        push.registeredVoIP(token: Data(repeating: 1, count: 32))
        let finished = expectation(description: "VoIP subscription while prompt is pending")
        let task = Task { await push.reconcile(store: store); finished.fulfill() }
        await fulfillment(of: [opened, finished], timeout: 5)
        XCTAssertTrue(push.voipReady)
        XCTAssertFalse(push.registered.isEmpty)
        let captured = await fixture.latestSubscription()
        let registration = try XCTUnwrap(captured)
        XCTAssertTrue(registration.calls)
        XCTAssertTrue(registration.enabled)
        XCTAssertFalse(registration.sms)
        XCTAssertFalse(registration.reminders)
        XCTAssertEqual(push.authorization, .notDetermined)
        await prompt.release()
        await task.value
        await push.reconcile(store: store)
        XCTAssertEqual(push.authorization, .denied)
        XCTAssertTrue(push.voipReady)
        XCTAssertFalse(push.registered.isEmpty)
        XCTAssertNil(push.failure)
    }

    func testTokenUpdatesAndInvalidationOmitTheOtherChannel() async throws {
        let fixture = CallPushRegistrationFixture()
        let gateway = PushGateway(address: URL(string: "https://gateway.example"), transport: { try await fixture.gateway($0) })
        let identity = PushIdentity(id: "installation", managementToken: "test-credential")
        _ = try await gateway.register(identity: identity, deviceToken: "regular", environment: "development", label: "Phone")
        _ = try await gateway.register(identity: identity, voipToken: "voice", environment: "development", label: "Phone")
        _ = try await gateway.register(identity: identity, voipToken: "", environment: "development", label: "Phone")
        let requests = await fixture.installations()
        XCTAssertEqual(requests.count, 3)
        XCTAssertTrue(requests[0].contains("\"deviceToken\":\"regular\""))
        XCTAssertFalse(requests[0].contains("voipToken"))
        XCTAssertTrue(requests[1].contains("\"voipToken\":\"voice\""))
        XCTAssertFalse(requests[1].contains("deviceToken"))
        XCTAssertTrue(requests[2].contains("\"voipToken\":\"\""))
        XCTAssertFalse(requests[2].contains("deviceToken"))
    }
}

private actor CallPushRegistrationFixture {
    struct Subscription: Decodable, Sendable { let calls, sms, reminders, enabled: Bool }
    private let base = CallsFixture()
    private var subscription: Subscription?
    private var tokens: [String] = []
    private var failure: BackendHTTPResponse?
    private var backendFailure: BackendHTTPResponse?
    private var count = 0

    func latestSubscription() -> Subscription? { subscription }
    func installations() -> [String] { tokens }
    func gatewayCount() -> Int { count }
    func setGatewayFailure(_ response: BackendHTTPResponse?) { failure = response }
    func setBackendFailure(_ response: BackendHTTPResponse?) { backendFailure = response }

    func backend(_ request: URLRequest) async throws -> BackendHTTPResponse {
        if request.url?.path.hasSuffix("/push-subscriptions") == true {
            if let backendFailure { return backendFailure }
            if request.httpMethod == "POST" {
                subscription = try JSONDecoder().decode(Subscription.self, from: XCTUnwrap(request.httpBody))
                return BackendHTTPResponse(data: Data("{}".utf8), status: 200)
            }
            return BackendHTTPResponse(data: Data(#"{"backendId":"fixture-call-backend","revision":0,"unreadCount":0,"subscriptions":[]}"#.utf8), status: 200)
        }
        return try await base.respond(request)
    }

    func gateway(_ request: URLRequest) throws -> BackendHTTPResponse {
        count += 1
        if let failure { return failure }
        if request.url?.path.hasSuffix("/push-installations") == true {
            tokens.append(String(decoding: try XCTUnwrap(request.httpBody), as: UTF8.self))
        }
        return BackendHTTPResponse(data: Data(#"{"id":"installation","count":0,"revision":0,"backends":[]}"#.utf8), status: 200)
    }
}

import Foundation
import Synchronization
@testable import Sigmo

@MainActor
final class MemoryCredentials {
    var tokens: [UUID: String] = [:]
    var readFailure = false
    var writeFailure = false
    var store: BackendCredentials {
        BackendCredentials(read: { id in
            if self.readFailure { throw BackendFailure.local("storage.keychainError") }
            return self.tokens[id]
        }, write: { id, token in
            if self.writeFailure { throw BackendFailure.local("storage.keychainError") }
            self.tokens[id] = token
        })
    }
}

final class TestTime: Sendable {
    private let value = Mutex(Date(timeIntervalSince1970: 1_800_000_000))
    func now() -> Date { value.withLock { $0 } }
    func advance(_ seconds: TimeInterval) { value.withLock { $0 = $0.addingTimeInterval(seconds) } }
    var clock: BackendClock { BackendClock(now: { self.now() }) }
}

actor BackendStub {
    var required = false
    var offline = false
    var modemStatus = 200
    var capabilitiesStatus = 200
    var modemJSON = PreviewBackendServer.modemJSON()
    var featuresJSON = #"["wifiCalling","futureFeature"]"#
    var licenseStatus = 404
    var authorized = false
    var appStatus = 200
    var sendStatus = 201
    var pairingStatus = "pending"
    var createStatus = 201
    var pollStatus = 200
    var expiry = Date(timeIntervalSince1970: 1_800_000_300)
    var requests: [URLRequest] = []
    var conversationsJSON = #"{"backendId":"test-backend","revision":1,"unreadCount":0,"items":[]}"#
    var conversationsStatus = 200
    private var holdConversations = false
    private var holdModems = false
    private var holdSuffix: String?
    private var held: CheckedContinuation<Void, Never>?
    private var arrival: CheckedContinuation<Void, Never>?

    func setOffline(_ value: Bool) { offline = value }
    func setRequired(_ value: Bool) { required = value }
    func setModemJSON(_ value: String) { modemJSON = value }
    func setModemStatus(_ value: Int) { modemStatus = value }
    func setCapabilities(status: Int, json: String = "null") { capabilitiesStatus = status; featuresJSON = json }
    func setLicense(status: Int = 200, authorized: Bool) { licenseStatus = status; self.authorized = authorized }
    func setAppStatus(_ value: Int) { appStatus = value }
    func setSendStatus(_ value: Int) { sendStatus = value }
    func setPairing(status: String, createStatus: Int = 201, pollStatus: Int = 200) {
        pairingStatus = status; self.createStatus = createStatus; self.pollStatus = pollStatus
    }
    func setExpiry(_ value: Date) { expiry = value }
    func holdNextModemRequest() { holdModems = true }
    func holdNextConversationRequest() { holdConversations = true }
    func setConversations(_ json: String, status: Int = 200) {
        conversationsJSON = json
        conversationsStatus = status
    }
    func holdNextRequest(_ suffix: String) { holdSuffix = suffix }
    func waitForHeldRequest() async {
        if held != nil { return }
        await withCheckedContinuation { arrival = $0 }
    }
    func release() { held?.resume(); held = nil }
    func count(_ suffix: String) -> Int { requests.filter { $0.url?.path.hasSuffix(suffix) == true }.count }
    func allRequests() -> [URLRequest] { requests }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if offline { throw URLError(.notConnectedToInternet) }
        let path = request.url?.path ?? ""
        if let suffix = holdSuffix, path.hasSuffix(suffix) {
            holdSuffix = nil
            await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
        }
        if path.hasSuffix("/license/pairings") { return pairingResponse(createStatus) }
        if path.contains("/license/pairings/") { return pairingResponse(pollStatus) }
        if path.hasSuffix("/license") {
            if licenseStatus >= 400 { return HTTPProblem.response(status: licenseStatus, code: licenseStatus == 404 ? "resource_not_found" : "license_service_unavailable", detail: "License unavailable") }
            return reply(licenseStatus, #"{"authorized":\#(authorized),"deviceId":"0123456789abcdef0123456789abcdef"}"#)
        }
        if path.hasSuffix("/app") {
            return reply(appStatus, #"{"version":"1","commit":"abc","channel":"dev","edition":"community","target":"linux-arm64","distribution":"local"}"#)
        }
        if path.hasSuffix("/capabilities") { return reply(capabilitiesStatus, #"{"features":\#(featuresJSON)}"#) }
        if path.hasSuffix("/auth/otp/required") { return reply(200, #"{"otpRequired":\#(required)}"#) }
        if path.hasSuffix("/auth/otp/verify") {
            if request.httpBody == Data(#"{"code":"123456"}"#.utf8) { return reply(200, #"{"token":"new-token"}"#) }
            return HTTPProblem.response(status: 401, code: "invalid_otp", detail: "Invalid code", requestID: "otp-request")
        }
        if path.hasSuffix("/auth/otp") { return reply(sendStatus, "") }
        if path.hasSuffix("/conversations") {
            let status = conversationsStatus
            let json = conversationsJSON
            if holdConversations {
                holdConversations = false
                await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
            }
            if status >= 400 {
                return HTTPProblem.response(status: status, code: status == 401 ? "invalid_token" : "resource_not_found", detail: "Conversations unavailable")
            }
            return reply(status, json)
        }
        if path.contains("/modems") {
            let status = modemStatus
            let modem = modemJSON
            if holdModems {
                holdModems = false
                await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
            }
            if status != 200 { return HTTPProblem.response(status: status, code: "invalid_token", detail: "No access", requestID: "modem-request") }
            return reply(200, path.hasSuffix("/modems") ? "[\(modem)]" : modem)
        }
        return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found")
    }

    private func pairingResponse(_ status: Int) -> BackendHTTPResponse {
        if status != 200 && status != 201 {
            return HTTPProblem.response(status: status, code: "license_pairing_not_found", detail: "Pairing not found")
        }
        return reply(status, #"{"id":"pair","activationUrl":"https://t.me/SigmoProBot?start=pair","status":"\#(pairingStatus)","expiresAt":"\#(expiry.ISO8601Format())"}"#)
    }
    private func reply(_ status: Int, _ body: String) -> BackendHTTPResponse {
        BackendHTTPResponse(data: Data(body.utf8), status: status, requestID: "header-request")
    }
}

@MainActor
func testConfiguration(_ host: String = "one.example") throws -> BackendConfiguration {
    BackendConfiguration(name: host, address: try BackendAddress.normalize("https://\(host)"))
}

@MainActor
func testSession(_ stub: BackendStub, configuration: BackendConfiguration? = nil,
                 credentials: MemoryCredentials = MemoryCredentials(), clock: BackendClock = BackendClock(),
                 gate: RefreshGate = RefreshGate()) throws -> BackendSession {
    let configuration = try configuration ?? testConfiguration()
    return BackendSession(configuration: configuration, credentials: credentials.store,
                          api: BackendAPI(configuration: configuration) { try await stub.respond($0) }, clock: clock, gate: gate)
}

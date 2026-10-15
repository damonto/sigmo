import Foundation

// Deterministic local SIM behavior. These credentials are fictional fixture data.
struct SIMPINFixture {
    var enabled = true
    var lock: SIMPINLock
    var pin = "1234"
    var pinRetries: UInt32 = 3
    var pukRetries: UInt32 = 10
    let token: String

    init(locked: Bool, token: String) {
        lock = locked ? .pin : .unlocked
        self.token = token
    }

    mutating func respond(_ request: URLRequest, resource: String) throws -> BackendHTTPResponse? {
        if resource == "/sim-pin", request.httpMethod == "GET" {
            let status = SIMPINStatus(supported: true, enabled: enabled, lock: lock, pinRetries: pinRetries, pukRetries: pukRetries, simToken: token)
            return BackendHTTPResponse(data: try JSONEncoder().encode(status), status: 200)
        }
        guard ["/sim-pin/protection", "/sim-pin-changes", "/sim-unlocks"].contains(resource) else { return nil }
        struct Submission: Decodable {
            let enabled: Bool?
            let pin: String?
            let oldPin: String?
            let newPin: String?
            let puk: String?
            let simToken: String?
        }
        let value = try JSONDecoder().decode(Submission.self, from: request.httpBody ?? Data())
        if value.simToken != token && !(resource == "/sim-unlocks" && value.simToken == nil && value.puk == nil) { return error("sim_target_changed") }
        if lock == .permanentlyBlocked { return error("sim_permanently_blocked") }
        if let puk = value.puk {
            guard lock == .puk else { return error("sim_pin_state_conflict") }
            guard validSIMPUK(puk), let replacement = value.newPin, validSIMPIN(replacement) else { return error("sim_pin_invalid_request", 400) }
            if puk != "12345678" {
                pukRetries -= 1
                if pukRetries == 0 { lock = .permanentlyBlocked }
                return error(pukRetries == 0 ? "sim_permanently_blocked" : "sim_puk_incorrect", 422)
            }
            pin = replacement
            lock = .unlocked
            enabled = true
            pinRetries = 3
            pukRetries = 10
        } else {
            if lock == .puk { return error("sim_puk_required") }
            let credential = value.oldPin ?? value.pin ?? ""
            guard validSIMPIN(credential) else { return error("sim_pin_invalid_request", 400) }
            if resource == "/sim-pin/protection", value.enabled == enabled { return success() }
            if credential != pin {
                pinRetries -= 1
                if pinRetries == 0 { lock = .puk }
                return error(pinRetries == 0 ? "sim_puk_required" : "sim_pin_incorrect", 422)
            }
            if resource == "/sim-pin-changes" {
                guard let replacement = value.newPin, validSIMPIN(replacement) else { return error("sim_pin_invalid_request", 400) }
                pin = replacement
            } else if resource == "/sim-pin/protection" {
                guard let protection = value.enabled else { return error("sim_pin_invalid_request", 400) }
                enabled = protection
            } else { lock = .unlocked }
            pinRetries = 3
        }
        return success()
    }
    private func success() -> BackendHTTPResponse { BackendHTTPResponse(data: Data(), status: 204) }
    private func error(_ code: String, _ status: Int = 409) -> BackendHTTPResponse {
        HTTPProblem.response(status: status, code: code, detail: "SIM credential operation rejected")
    }
}

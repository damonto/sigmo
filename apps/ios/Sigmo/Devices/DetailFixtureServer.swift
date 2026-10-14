import Foundation
import Synchronization

// In-memory device behavior shared by explicit demo sessions and automated tests.
// These transports never create a URLSession or access physical hardware.
final class DetailFixtureBuffer: Sendable {
    private struct State {
        var queue: [Data] = []
        var waiting: CheckedContinuation<Data, Error>?
        var closed = false
    }
    private let state = Mutex(State())
    func push(_ data: Data) {
        let waiting = state.withLock { state -> CheckedContinuation<Data, Error>? in
            guard !state.closed else { return nil }
            if let waiting = state.waiting { state.waiting = nil; return waiting }
            state.queue.append(data)
            return nil
        }
        waiting?.resume(returning: data)
    }
    func receive() async throws -> Data {
        try Task.checkCancellation()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                state.withLock { state in
                    if state.closed { continuation.resume(throwing: CancellationError()) }
                    else if !state.queue.isEmpty { continuation.resume(returning: state.queue.removeFirst()) }
                    else { state.waiting = continuation }
                }
            }
        } onCancel: { self.close() }
    }
    func close() {
        let waiting = state.withLock { state -> CheckedContinuation<Data, Error>? in
            state.closed = true
            state.queue = []
            let waiting = state.waiting
            state.waiting = nil
            return waiting
        }
        waiting?.resume(throwing: CancellationError())
    }
}

actor DetailFixtureServer {
    enum Scenario {
        case standard, delayedProfiles, unavailableProfiles, singleHardware, notifications, delayedDiscoveries, noDiscoveries, unavailableDiscoveries, emptyProfiles
        case delayedDeletion, deletionFailure
        case delayedActivation, activationFailure
        case mixedTransferProfiles, unsupportedTransferProfiles
        case delayedSettings, unavailableSettings, unsupportedSettings, settingsWriteFailure
        case connectionSetup, connectionScan, connectionScanError, connectionEmptyNetworks, connectionReadFailure, connectionWriteFailure, connectionMBIM, connectionCommunity
    }

    private var physical: Bool
    private var locked: Bool
    private let scenario: Scenario
    private let modemNames: [String: String]
    private var slot = 1
    private var number = "+8613800000000"
    private var physicalReminder: SIMReminder?
    private var groups: [ESIMProfileGroup]
    private var notifications: [ESIMNotificationGroup]
    private var activeSE = "se-a"
    private var connections: [String: ConnectionFixture] = [:]
    private var capabilities = ["wifiCalling", "volte", "esimTransfer"]
    private var modemSettings: [String: ModemSettingsFixture] = [:]
    private var requestStatus: [String: Int] = [:]
    private var profilesOverride: Data?
    private var transferProfilesOverride: [TransferProfile]?
    private var discoveries = [ESIMDiscovery(eventId: "event-1", address: "smdp.example")]
    private(set) var requests: [URLRequest] = []
    private(set) var messages: [Data] = []
    private var sessions: [UUID: (buffer: DetailFixtureBuffer, kind: String, seID: String)] = [:]
    private var nextProfile = 3
    private var holdSuffix: String?
    private var holdSocketSend = false
    private var previewCancellationResponse: String? = #"{"type":"completed"}"#
    private var previewRejected = false
    private var previewRejectionWaiter: CheckedContinuation<Void, Never>?
    private var held: CheckedContinuation<Void, Never>?
    private var arrival: CheckedContinuation<Void, Never>?
    var requireConfirmation = true

    init(physical: Bool = false, locked: Bool = false, scenario: Scenario = .standard, number: String = "+8613800000000",
         initialConnection: ConnectionFixture = ConnectionFixture(), modemNames: [String: String] = [:]) {
        self.number = number
        self.physical = physical; self.locked = locked
        self.scenario = scenario
        self.modemNames = modemNames
        groups = [
            ESIMProfileGroup(id: "se-a", label: "eUICC A", aid: nil, eid: "89049032000000000000000000000001", profiles: [Self.profile(se: "se-a", iccid: "8986000000000000001", name: "Primary", active: true), Self.profile(se: "se-a", iccid: "8986000000000000002", name: "Travel", active: false)]),
            ESIMProfileGroup(id: "se-b", label: "eUICC B", aid: nil, eid: "89049032000000000000000000000002", profiles: [Self.profile(se: "se-b", iccid: "8986000000000000002", name: "Backup", active: false)])
        ]
        for group in groups.indices {
            for profile in groups[group].profiles.indices where groups[group].profiles[profile].enabled {
                groups[group].profiles[profile].number = number
            }
        }
        notifications = [
            ESIMNotificationGroup(id: "se-a", label: "eUICC A", eid: nil, notifications: [Self.notification(sequence: "18446744073709551615", operation: "install")]),
            ESIMNotificationGroup(id: "se-b", label: "eUICC B", eid: nil, notifications: [])
        ]
        if scenario == .singleHardware {
            groups = Array(groups.prefix(1))
            notifications = Array(notifications.prefix(1))
        } else if scenario == .notifications {
            notifications = [
                ESIMNotificationGroup(id: "se-a", label: "eUICC A", eid: nil, notifications: [
                    Self.notification(sequence: "18446744073709551615", operation: "install"),
                    Self.notification(sequence: "2", operation: "enable"),
                    Self.notification(sequence: "3", operation: "disable"),
                    Self.notification(sequence: "4", operation: "delete")
                ]),
                ESIMNotificationGroup(id: "se-b", label: "eUICC B", eid: nil, notifications: [
                    Self.notification(sequence: "18446744073709551615", operation: "install", seID: "se-b")
                ])
            ]
        }
        if scenario == .noDiscoveries { discoveries = [] }
        if scenario == .unavailableDiscoveries { requestStatus["/esim-discoveries"] = 503 }
        if scenario == .emptyProfiles {
            for index in groups.indices { groups[index].profiles = [] }
        }
        if scenario == .mixedTransferProfiles {
            transferProfilesOverride = [Self.transferProfile("Unsupported line", supported: false), Self.transferProfile("Inactive line", enabled: false)]
        } else if scenario == .unsupportedTransferProfiles {
            transferProfilesOverride = [Self.transferProfile("Unsupported line", supported: false)]
        }
        var connection = initialConnection
        switch scenario {
        case .connectionSetup:
            connection.wifiRequiresSetup = true
            connection.wifi.emergencyAddressUpdateAvailable = true
        case .connectionScan: connection.scanPolls = 2
        case .connectionScanError: connection.scanOutcome = "failed"
        case .connectionEmptyNetworks: connection.networks = []
        case .connectionReadFailure:
            connection.statuses["GET /internet-connections/current"] = 503
            connection.statuses["GET /networks/registration"] = 503
            connection.statuses["GET /wifi-calling/settings"] = 503
            connection.statuses["GET /volte/settings"] = 503
        case .connectionWriteFailure:
            connection.statuses["PUT /internet-connections/current/preferences"] = 503
            connection.statuses["PUT /volte/settings"] = 503
        case .connectionMBIM: connection.volte.dataPath = "mbim"
        case .connectionCommunity: capabilities = []
        default: break
        }
        connections["device-1"] = connection
    }

    private static func notification(sequence: String, operation: String, seID: String = "se-a") -> ESIMNotification {
        ESIMNotification(seId: seID, seLabel: seID, seEid: nil, sequenceNumber: sequence, iccid: "8986000000000000001", smdp: "smdp.example", operation: operation)
    }
    static func profile(se: String, iccid: String, name: String, active: Bool) -> ESIMProfile {
        .init(seId: se, seLabel: se, seEid: nil, name: name, number: active ? "+8613800000000" : nil, serviceProviderName: "Example Mobile", iccid: iccid,
              isdPAID: "A0000005591010", icon: "", profileName: name, profileNickname: nil, profileState: active ? 1 : 0,
              profileStateName: active ? "enabled" : "disabled", profileClass: "operational", profileOwner: .init(mcc: "460", mnc: "00", gid1: nil, gid2: nil), regionCode: "CN", reminder: nil)
    }
    static var elements: SecureElements {
        SecureElements(ses: ["se-a", "se-b"].enumerated().map { index, id in
            SecureElement(id: id, label: "eUICC \(index == 0 ? "A" : "B")", aid: nil, eid: "8904903200000000000000000000000\(index + 1)", freeSpace: index == 0 ? 65536 : 32768,
                          sasUp: .init(name: "Example accredited site", region: "CN"), certificates: ["GSMA Root CI"])
        })
    }
    static func transferProfile(_ name: String, supported: Bool = true, enabled: Bool = true) -> TransferProfile {
        TransferProfile(id: name, seId: "se-a", type: "esim", name: name, serviceProviderName: "Example Mobile",
                        iccid: name, icon: nil, regionCode: "CN", enabled: enabled, supported: supported,
                        unsupportedReason: supported ? nil : "Carrier does not support transfer", carrierName: nil)
    }
    func setStatus(_ suffix: String, _ status: Int) { requestStatus[suffix] = status }
    func setConnectionFixture(_ value: ConnectionFixture, modemID: String = "device-1") { connections[modemID] = value }
    func currentConnections(_ id: String = "device-1") -> ConnectionFixture { connections[id] ?? ConnectionFixture() }
    func setCapabilities(_ value: [String]) { capabilities = value }
    func setModemSettings(_ value: ModemSettingsFixture, modemID: String = "device-1") { modemSettings[modemID] = value }
    func currentModemSettings(_ id: String = "device-1") -> ModemSettingsFixture { settingsFixture(id) }
    func setProfilesData(_ data: Data?) { profilesOverride = data }
    func setTransferProfiles(_ values: [TransferProfile]) { transferProfilesOverride = values }
    func setDiscoveries(_ values: [ESIMDiscovery]) { discoveries = values }
    func setConfirmation(_ value: Bool) { requireConfirmation = value }
    func holdNext(_ suffix: String) { holdSuffix = suffix }
    func holdNextSocketSend() { holdSocketSend = true }
    func setPreviewCancellationResponse(_ response: String?) { previewCancellationResponse = response }
    func waitForPreviewRejection() async {
        if !previewRejected { await withCheckedContinuation { previewRejectionWaiter = $0 } }
    }
    func waitForHeldRequest() async { if held == nil { await withCheckedContinuation { arrival = $0 } } }
    func release() { held?.resume(); held = nil }
    func allRequests() -> [URLRequest] { requests }
    func allMessages() -> [Data] { messages }
    func emitRaw(_ text: String) { for session in sessions.values { session.buffer.push(Data(text.utf8)) } }
    func disconnectSessions() { for session in sessions.values { session.buffer.close() }; sessions = [:] }

    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        requests.append(request)
        if scenario == .delayedSettings, request.httpMethod == "GET", let path = request.url?.path,
           path.contains("/networks/") || path.hasSuffix("/settings") {
            try await Task.sleep(for: .seconds(2))
        }
        if scenario == .delayedProfiles, let path = request.url?.path, path.hasSuffix("/ses") || path.hasSuffix("/esims") {
            try await Task.sleep(for: .seconds(3))
        }
        if scenario == .delayedDiscoveries, request.url?.path.hasSuffix("/esim-discoveries") == true {
            try await Task.sleep(for: .seconds(5))
        }
        if scenario == .delayedDeletion || scenario == .deletionFailure,
           request.httpMethod == "DELETE", request.url?.path.contains("/esims/") == true {
            try await Task.sleep(for: .seconds(8))
            if scenario == .deletionFailure {
                return HTTPProblem.response(status: 503, code: "remote_error", detail: "Fixture error")
            }
        }
        if scenario == .delayedActivation || scenario == .activationFailure,
           request.httpMethod == "PUT", request.url?.path.hasSuffix("/activation") == true {
            try await Task.sleep(for: .seconds(8))
            if scenario == .activationFailure {
                return HTTPProblem.response(status: 503, code: "remote_error", detail: "Fixture error")
            }
        }
        let response = try response(request)
        if let suffix = holdSuffix, request.url?.path.hasSuffix(suffix) == true {
            holdSuffix = nil
            await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
        }
        return response
    }
    private func response(_ request: URLRequest) throws -> BackendHTTPResponse {
        let path = request.url?.path ?? "", method = request.httpMethod ?? "GET"
        for (suffix, status) in requestStatus where path.hasSuffix(suffix) && status >= 400 {
            let code = status == 401 ? "invalid_session" : "remote_error"
            return HTTPProblem.response(status: status, code: code, detail: "Fixture error")
        }
        if path.hasSuffix("/license") { return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found") }
        if path.hasSuffix("/app") { return raw(#"{"version":"1","commit":"fixture","channel":"stable","edition":"pro","target":"linux-arm64","distribution":"local"}"#) }
        if path.hasSuffix("/capabilities") { return try reply(["features": capabilities]) }
        if path.hasSuffix("/auth/sessions/current") { return raw("", status: 204) }
        if path.hasSuffix("/auth/methods") { return raw(AuthenticationFixture.methods(required: false)) }
        let parts = path.components(separatedBy: "/")
        if path.contains("/websheets/"), path.hasSuffix("/callback"), method == "POST" {
            let callback = try JSONDecoder().decode(CarrierCallback.self, from: request.httpBody ?? Data())
            let sheetID = parts.dropLast().last ?? ""
            for id in Array(connections.keys) {
                if let response = connections[id]?.callback(callback, id: sheetID) { return response }
            }
        }
        let modemIndex = parts.firstIndex(of: "modems")
        let modemID = modemIndex.flatMap { parts.indices.contains($0 + 1) ? parts[$0 + 1] : nil } ?? "device-1"
        if path.hasSuffix("/modems") { return try reply([modem("device-1"), modem("device-2")]) }
        if path.hasSuffix("/modems/" + modemID) { return try reply(modem(modemID)) }
        if let modemIndex, parts.indices.contains(modemIndex + 1) {
            var settings = settingsFixture(modemID)
            defer { modemSettings[modemID] = settings }
            let resource = "/" + parts.dropFirst(modemIndex + 2).joined(separator: "/")
            if let result = try settings.respond(request, resource: resource) { return result }
            var connection = currentConnections(modemID)
            defer { connections[modemID] = connection }
            if let result = try connection.respond(request, resource: resource, modemID: modemID, airplane: settings.airplaneEnabled) { return result }
        }
        if path.hasSuffix("/ses") {
            return try reply(scenario == .singleHardware ? SecureElements(ses: Array(Self.elements.ses.prefix(1))) : Self.elements)
        }
        if path.hasSuffix("/esims"), method == "GET" {
            if scenario == .unavailableProfiles { return HTTPProblem.response(status: 503, code: "remote_error", detail: "Fixture error") }
            if let profilesOverride { return BackendHTTPResponse(data: profilesOverride, status: 200) }
            return try reply(ESIMProfiles(ses: groups))
        }
        if path.hasSuffix("/sim-unlocks") {
            let value = try JSONDecoder().decode([String: String].self, from: request.httpBody ?? Data())
            if value["pin"] != "1234" { return HTTPProblem.response(status: 400, code: "remote_error", detail: "Incorrect PIN") }
            locked = false; return raw("", status: 204)
        }
        if path.contains("/sim-slots/") { slot = Int(parts.last ?? "") ?? 1; return raw("", status: 204) }
        if path.hasSuffix("/msisdn") {
            number = try JSONDecoder().decode([String: String].self, from: request.httpBody ?? Data())["number"] ?? number
            if !physical {
                for group in groups.indices where groups[group].id == activeSE {
                    for profile in groups[group].profiles.indices where groups[group].profiles[profile].enabled {
                        groups[group].profiles[profile].number = number
                    }
                }
            }
            return raw("", status: 204)
        }
        if path.contains("/reminder") {
            let value: SIMReminder?
            if method == "DELETE" { value = nil }
            else {
                let decoder = JSONDecoder(); decoder.dateDecodingStrategy = .iso8601
                let submission = try decoder.decode(ReminderSubmission.self, from: request.httpBody ?? Data())
                value = .init(nextAt: submission.scheduledAt, repeatDays: submission.repeatDays, content: submission.content)
            }
            if path.contains("/sims/") { physicalReminder = value }
            else if let (g, p) = profileLocation(parts) { groups[g].profiles[p].reminder = value }
            return try value.map { try reply($0) } ?? raw("", status: 204)
        }
        if let (g, p) = profileLocation(parts) {
            if path.hasSuffix("/nickname") {
                groups[g].profiles[p].name = try JSONDecoder().decode([String: String].self, from: request.httpBody ?? Data())["nickname"] ?? ""
            } else if path.hasSuffix("/activation") {
                for pi in groups[g].profiles.indices { groups[g].profiles[pi].profileState = pi == p ? 1 : 0 }
                activeSE = groups[g].id
            } else if method == "DELETE" {
                if groups[g].profiles[p].enabled { return HTTPProblem.response(status: 409, code: "remote_error", detail: "Active profile") }
                groups[g].profiles.remove(at: p)
            }
            return raw("", status: 204)
        }
        if path.hasSuffix("/esim-discoveries") { return try reply(discoveries) }
        if path.hasSuffix("/esim-transfers/sources") {
            return try reply(TransferSources(sources: [.init(type: "modem", id: "device-2", name: "Source modem", detail: "Example Mobile", requiresSourceImei: false), .init(type: "ccid", id: "reader-1", name: "USB reader", detail: nil, requiresSourceImei: true)], ccidError: nil))
        }
        if path.hasSuffix("/esim-transfers/source-profiles") {
            if let transferProfilesOverride { return try reply(transferProfilesOverride) }
            return try reply([TransferProfile(id: "source-profile", seId: "se-a", type: "esim", name: "Source profile", serviceProviderName: "Example Mobile", iccid: "8986000000000000099", icon: nil, regionCode: "CN", enabled: true, supported: true, unsupportedReason: nil, carrierName: nil)])
        }
        if path.hasSuffix("/notifications") { return try reply(ESIMNotifications(ses: notifications)) }
        if path.contains("/notifications/"), method == "DELETE" {
            guard let si = parts.firstIndex(of: "ses"), parts.indices.contains(si + 1),
                  let sequence = parts.last,
                  let index = notifications.firstIndex(where: { $0.id == parts[si + 1] }) else {
                return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found")
            }
            let group = notifications[index]
            notifications[index] = ESIMNotificationGroup(id: group.id, label: group.label, eid: group.eid,
                notifications: group.notifications.filter { $0.sequenceNumber != sequence })
            return raw("", status: 204)
        }
        if path.hasSuffix("/deliveries") || path.hasSuffix("/callback") { return raw("", status: 204) }
        return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found")
    }
    func socket(_ request: URLRequest) throws -> BackendSocket {
        requests.append(request)
        let path = request.url?.path ?? ""
        for (suffix, status) in requestStatus where path.hasSuffix(suffix) && status >= 400 {
            throw BackendFailure(status: status, code: "socket_handshake", message: "Fixture handshake error")
        }
        let id = UUID(), buffer = DetailFixtureBuffer()
        let kind = path.contains("sim-application") ? "sim" : (path.contains("esim-transfers") ? "transfer" : "download")
        sessions[id] = (buffer, kind, "se-a")
        if kind == "sim" { buffer.push(Data(#"{"type":"status","available":true,"profileIccid":"8986000000000000001","menu":{"kind":"root","title":"SIM services","items":[{"id":1,"label":"Balance"},{"id":2,"label":"Enter code"},{"id":3,"label":"Yes or no"},{"id":4,"label":"Confirm"}]}}"#.utf8)) }
        return BackendSocket(send: { data in try await self.send(data, sessionID: id) }, receive: { try await buffer.receive() }, close: { buffer.close() })
    }
    private func send(_ data: Data, sessionID: UUID) async throws {
        if holdSocketSend {
            holdSocketSend = false
            await withCheckedContinuation { held = $0; arrival?.resume(); arrival = nil }
            throw URLError(.networkConnectionLost)
        }
        messages.append(data)
        guard var session = sessions[sessionID] else { throw CancellationError() }
        struct Command: Decodable { let type: String; let seId: String?; let accept: Bool?; let itemId: Int? }
        let command = try JSONDecoder().decode(Command.self, from: data)
        func emit(_ text: String) { session.buffer.push(Data(text.utf8)) }
        if command.type == "cancel" { session.buffer.close(); sessions[sessionID] = nil; return }
        if session.kind == "sim" {
            switch command.itemId {
            case 1: emit(#"{"type":"display_text","text":"Balance: 10.00","userClear":true}"#)
            case 2: emit(#"{"type":"input","text":"Enter code","minLength":1,"maxLength":8,"hideInput":true}"#)
            case 3: emit(#"{"type":"inkey","text":"Continue?","yesNo":true}"#)
            case 4: emit(#"{"type":"confirm","text":"Run SIM service?","command":"send_sms"}"#)
            default: break
            }
            return
        }
        if command.type == "source" {
            emit(#"{"type":"source_ready","version":1}"#)
            return
        }
        if command.type == "profiles" {
            struct Profiles: Encodable { let type = "profiles"; let profiles: [TransferProfile] }
            session.buffer.push(try JSONEncoder().encode(Profiles(profiles: [Self.transferProfile("Demo SIM")])))
            return
        }
        if command.type == "start" {
            session.seID = command.seId ?? "se-a"; sessions[sessionID] = session
            emit(#"{"type":"progress","stage":"Authenticating Client"}"#)
            emit(#"{"type":"preview","profile":{"iccid":"8986000000000000003","serviceProviderName":"Example Mobile","profileName":"Downloaded","profileState":"disabled","profileOwner":{"mcc":"460","mnc":"00"}}}"#)
            if session.kind == "transfer" { emit(#"{"type":"user_input","input":{"text":"Confirm transfer","freeText":true,"freeTextHint":"Verification code"}}"#) }
            return
        }
        if command.type == "confirm", command.accept == false {
            previewRejected = true
            previewRejectionWaiter?.resume()
            previewRejectionWaiter = nil
            if let previewCancellationResponse { emit(previewCancellationResponse) }
            return
        }
        if command.type == "confirm", requireConfirmation { emit(#"{"type":"confirmation_code_required"}"#); return }
        if command.type == "user_input" { emit(#"{"type":"source_deletion","iccid":"8986000000000000099"}"#); return }
        if command.type == "source_deletion", command.accept == false { emit(#"{"type":"error","message":"Source deletion declined"}"#); return }
        if ["confirm", "confirmation_code", "source_deletion"].contains(command.type) {
            let iccid = "898600000000000000\(nextProfile)"
            nextProfile += 1
            if let g = groups.firstIndex(where: { $0.id == session.seID }) { groups[g].profiles.append(Self.profile(se: session.seID, iccid: iccid, name: "Downloaded", active: false)) }
            emit(#"{"type":"progress","stage":"Installing"}"#)
            emit(#"{"type":"completed"}"#)
        }
    }
    private func settingsFixture(_ id: String) -> ModemSettingsFixture {
        if let settings = modemSettings[id] { return settings }
        var settings = ModemSettingsFixture()
        switch scenario {
        case .unavailableSettings: settings.statuses["GET /networks/bands"] = 503
        case .unsupportedSettings:
            settings.airplaneSupported = false
            settings.mode = .init(allowed: 4, preferred: 0)
            settings.supportedModes = Array(settings.supportedModes.prefix(1))
        case .settingsWriteFailure: settings.failNextWrite = true
        default: break
        }
        return settings
    }

    private func modem(_ id: String) -> Modem {
        let settings = settingsFixture(id)
        let connection = currentConnections(id)
        let defaultName = modemNames[id] ?? (id == "device-1" ? "Home Modem" : "Source Modem")
        let profileICCID = groups.first { $0.id == activeSE }?.profiles.first { $0.enabled }?.iccid ?? "8986000000000000001"
        let sim = SIMSlot(slot: slot, active: true, operatorName: "Example Mobile", operatorIdentifier: "46000", regionCode: "CN", identifier: slot == 1 ? profileICCID : "8986000000000000099", reminder: physicalReminder)
        let slots = [SIMSlot(slot: 1, active: slot == 1, operatorName: "Example Mobile", operatorIdentifier: "46000", regionCode: "CN", identifier: "8986000000000000001"), SIMSlot(slot: 2, active: slot == 2, operatorName: "Travel", operatorIdentifier: "23410", regionCode: "GB", identifier: "8986000000000000099")]
        return Modem(id: id, name: settings.device.alias.isEmpty ? defaultName : settings.device.alias, manufacturer: "Quectel", primaryPort: "/dev/cdc-wdm0", firmwareRevision: "1.0", hardwareRevision: "1", number: number, state: locked ? "locked" : "registered", unlockRequired: locked ? "sim-pin" : "none", unlockSupported: locked, sim: sim,
                     slots: scenario == .singleHardware ? Array(slots.prefix(1)) : slots, accessTechnology: settings.airplaneEnabled ? nil : "lte", registrationState: settings.airplaneEnabled ? "" : connection.registrationState, registeredOperator: connection.registeredOperator, signalQuality: settings.airplaneEnabled ? 0 : 78, airplaneMode: settings.airplaneEnabled, simKind: physical ? "physical" : "euicc", internetConnected: connection.internet.connected && !settings.airplaneEnabled, wifiCallingEnabled: connection.wifi.enabled, wifiCallingConnected: connection.wifi.connected, volteEnabled: connection.volte.enabled, volteConnected: connection.volte.connected && !settings.airplaneEnabled)
    }
    private func profileLocation(_ parts: [String]) -> (Int, Int)? {
        guard let si = parts.firstIndex(of: "ses"), parts.indices.contains(si + 1),
              let pi = parts.firstIndex(of: "esims"), parts.indices.contains(pi + 1),
              let g = groups.firstIndex(where: { $0.id == parts[si + 1] }),
              let p = groups[g].profiles.firstIndex(where: { $0.iccid == parts[pi + 1] }) else { return nil }
        return (g, p)
    }
    private func raw(_ text: String, status: Int = 200) -> BackendHTTPResponse { .init(data: Data(text.utf8), status: status) }
    private func reply<Value: Encodable>(_ value: Value) throws -> BackendHTTPResponse {
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        return BackendHTTPResponse(data: try encoder.encode(value), status: 200)
    }
}

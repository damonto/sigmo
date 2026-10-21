#if DEBUG
import Foundation
import Intents

// Only explicit Debug launch arguments enable fixtures; normal app launches use the network.
@MainActor
enum PreviewBackends {
    static let homeID = UUID(uuidString: "00000000-0000-0000-0000-000000000001")!
    static let studioID = UUID(uuidString: "00000000-0000-0000-0000-000000000002")!
    static let lockedID = UUID(uuidString: "00000000-0000-0000-0000-000000000003")!
    static let offlineID = UUID(uuidString: "00000000-0000-0000-0000-000000000004")!
    static let activationID = UUID(uuidString: "00000000-0000-0000-0000-000000000005")!

    static func store(scenario: String) -> BackendStore {
        let file = ConfigurationFile(url: URL.applicationSupportDirectory.appending(path: "UITests/\(scenario)/backends.json"))
        let server = PreviewBackendServer(multipleDevices: scenario == "cards", airplaneMode: scenario == "airplane", imsEnabled: scenario != "ims-disabled", tracksRefreshes: scenario == "refresh")
        var tokens: [UUID: String] = [:]
        let credentials = BackendCredentials(read: { tokens[$0] }, write: { tokens[$0] = $1 })
        let store = BackendStore(file: file, credentials: credentials, makeAPI: { configuration in
            if scenario.hasPrefix("communication") {
                let modemNames = configuration.id == studioID ? ["device-1": "Studio Modem", "device-2": "Studio Source Modem"] : [:]
                let fixture = CommunicationFixture(
                    contactName: configuration.id == studioID ? "Studio Contact" : "Alice",
                    includesHistory: ["communication-history", "communication-multiple"].contains(scenario),
                    modemContacts: scenario == "communication-modem",
                    modemNames: modemNames
                )
                return BackendAPI(configuration: configuration, transport: { try await fixture.respond($0) })
            }
            if scenario.hasPrefix("calls") {
                let fixture = CallsFixture(incoming: scenario == "calls-incoming")
                return BackendAPI(configuration: configuration, transport: { try await fixture.respond($0) }, socketFactory: { await fixture.socket($0) })
            }
            if scenario.hasPrefix("messages") {
                let fixture = MessagesFixture(unreadMessages: scenario == "messages-badge" ? 125 : nil)
                return BackendAPI(configuration: configuration, transport: { try await fixture.respond($0) })
            }
            if scenario.hasPrefix("detail") {
                let fixture: DetailFixtureServer.Scenario
                switch scenario {
                case "detail-loading": fixture = .delayedProfiles
                case "detail-refresh": fixture = .profileRefresh
                case "detail-unavailable": fixture = .unavailableProfiles
                case "detail-single": fixture = .singleHardware
                case "detail-notifications": fixture = .notifications
                case "detail-discovery-loading": fixture = .delayedDiscoveries
                case "detail-no-discoveries": fixture = .noDiscoveries
                case "detail-discovery-error": fixture = .unavailableDiscoveries
                case "detail-empty-profiles": fixture = .emptyProfiles
                case "detail-transfer-mixed": fixture = .mixedTransferProfiles
                case "detail-transfer-unsupported": fixture = .unsupportedTransferProfiles
                case "detail-deletion-loading": fixture = .delayedDeletion
                case "detail-deletion-error": fixture = .deletionFailure
                case "detail-activation-loading": fixture = .delayedActivation
                case "detail-activation-error": fixture = .activationFailure
                case "detail-settings-loading": fixture = .delayedSettings
                case "detail-settings-unavailable": fixture = .unavailableSettings
                case "detail-settings-unsupported": fixture = .unsupportedSettings
                case "detail-settings-write-error": fixture = .settingsWriteFailure
                case "detail-connection-setup": fixture = .connectionSetup
                case "detail-connection-scan": fixture = .connectionScan
                case "detail-connection-scan-error": fixture = .connectionScanError
                case "detail-connection-empty": fixture = .connectionEmptyNetworks
                case "detail-connection-read-error": fixture = .connectionReadFailure
                case "detail-connection-write-error": fixture = .connectionWriteFailure
                case "detail-connection-mbim": fixture = .connectionMBIM
                case "detail-connection-community": fixture = .connectionCommunity
                default: fixture = .standard
                }
                let detail = DetailFixtureServer(physical: scenario == "detail-physical", locked: scenario == "detail-locked", scenario: fixture)
                return BackendAPI(configuration: configuration, transport: { try await detail.respond($0) }, socketFactory: { try await detail.socket($0) })
            }
            return BackendAPI(configuration: configuration, transport: { try await server.respond($0) }, socketFactory: { _ in
                let buffer = DetailFixtureBuffer()
                buffer.push(Data(#"{"type":"status","available":false}"#.utf8))
                return BackendSocket(send: { _ in }, receive: { try await buffer.receive() }, close: { buffer.close() })
            })
        })
        if ProcessInfo.processInfo.environment["SIGMO_UI_RESET"] == "1" || !FileManager.default.fileExists(atPath: file.url.path) {
            if scenario.hasPrefix("communication") { UserDefaults.standard.removeObject(forKey: "communication.backend") }
            do {
                try file.save(BackendSettings())
                store.reloadConfiguration()
                if scenario != "empty" {
                    store.saveBackend(id: homeID, name: "Home", address: "https://home.sigmo.test")
                    if scenario == "communication-multiple" {
                        store.saveBackend(id: studioID, name: "Studio", address: "https://studio.sigmo.test")
                    } else if !["single", "airplane", "ims-disabled", "messages", "refresh"].contains(scenario) && !scenario.hasPrefix("communication") && !scenario.hasPrefix("calls") && !scenario.hasPrefix("messages") {
                        store.saveBackend(id: studioID, name: "Studio", address: "https://studio.sigmo.test")
                        store.saveBackend(id: lockedID, name: "Locked", address: "https://locked.sigmo.test")
                        store.saveBackend(id: offlineID, name: "Offline", address: "https://offline.sigmo.test")
                        store.saveBackend(id: activationID, name: "Pro Activation", address: "https://activation.sigmo.test")
                    }
                }
            } catch { store.failure = .wrapping(error) }
        }
        return store
    }

    static func requestCallbackIfNeeded(store: BackendStore, calls: CallsState) async {
        guard let mode = ProcessInfo.processInfo.environment["SIGMO_UI_CALLBACK"] else { return }
        await store.refreshAll()
        guard !Task.isCancelled, let session = store.onlineSessions.first,
              let modem = session.modems.first else { return }
        let line = CallLine(session: session, modem: modem)
        let number = "10086"
        switch mode {
        case "home":
            break
        case "modem":
            calls.rememberCallbackRoute(
                number: number,
                line: CallLine(backendID: line.backendID, modemID: line.modemID, profileID: "inactive-profile",
                               backendName: line.backendName, label: line.label, region: line.region, address: line.address),
                startedAt: Date.now.ISO8601Format()
            )
        case "dial":
            calls.rememberCallbackRoute(number: number, line: line, startedAt: Date.now.ISO8601Format())
        default:
            return
        }
        let person = INPerson(personHandle: INPersonHandle(value: number, type: .phoneNumber),
                              nameComponents: nil, displayName: nil, image: nil,
                              contactIdentifier: nil, customIdentifier: nil)
        calls.requestCallback(INStartCallIntent(callRecordFilter: nil, callRecordToCallBack: nil,
                                                audioRoute: .unknown, destinationType: .normal,
                                                contacts: [person], callCapability: .audioCall))
    }
}

actor PreviewBackendServer {
    private let multipleDevices: Bool
    private let airplaneMode: Bool
    private let imsEnabled: Bool
    private let tracksRefreshes: Bool
    private var modemReads = 0
    private var paired = false
    private var polls = 0
    private var settingsServers: [String: SettingsFixtureServer] = [:]

    init(multipleDevices: Bool = false, airplaneMode: Bool = false, imsEnabled: Bool = true, tracksRefreshes: Bool = false) {
        self.multipleDevices = multipleDevices
        self.airplaneMode = airplaneMode
        self.imsEnabled = imsEnabled
        self.tracksRefreshes = tracksRefreshes
    }

    func respond(_ request: URLRequest) throws -> BackendHTTPResponse {
        let host = request.url?.host ?? ""
        let path = request.url?.path ?? ""
        if host == "offline.sigmo.test" { throw URLError(.cannotConnectToHost) }
        let locked = host == "locked.sigmo.test"
        let activation = host == "activation.sigmo.test"
        var settingsServer = try settingsServers[host] ?? SettingsFixtureServer()
        defer { settingsServers[host] = settingsServer }
        let settingsResponse = try settingsServer.respond(request)
        if let settingsResponse { return settingsResponse }
        let requiresOTP = locked || settingsServer.otpRequired
        if path.hasSuffix("/license/pairings") {
            polls = 0
            return pairing(status: "pending", httpStatus: 201)
        }
        if path.contains("/license/pairings/") {
            polls += 1
            paired = polls >= 2
            return pairing(status: paired ? "active" : "pending", httpStatus: 200)
        }
        if path.hasSuffix("/license") {
            if activation {
                return reply(200, #"{"authorized":\#(paired),"deviceId":"0123456789abcdef0123456789abcdef"}"#)
            }
            return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not Found")
        }
        if path.hasSuffix("/app") {
            return BackendHTTPResponse(data: try JSONEncoder().encode(settingsServer.app), status: 200)
        }
        if path.hasSuffix("/capabilities") {
            return reply(200, host == "home.sigmo.test" ? #"{"features":["contacts","messageThreads","historyAssignments","wifiCalling","esimTransfer","volte"]}"# : #"{"features":["contacts","messageThreads","historyAssignments"]}"#)
        }
        if request.httpMethod == "GET" {
            if path.hasSuffix("/contacts") || path.hasSuffix("/sim-profiles") { return reply(200, #"{"items":[]}"#) }
            if path.hasSuffix("/message-threads") { return reply(200, #"{"items":[],"nextCursor":""}"#) }
            if path.hasSuffix("/conversations") { return reply(200, #"{"backendId":"fixture-preview","revision":0,"unreadCount":0,"items":[]}"#) }
        }
        if path.hasSuffix("/auth/sessions/current") { return reply(204, "") }
        if path.hasSuffix("/auth/methods") { return reply(200, AuthenticationFixture.methods(required: requiresOTP)) }
        if path.hasSuffix("/auth/sessions") {
            if AuthenticationFixture.validCode(request) { return reply(200, AuthenticationFixture.tokens("fixture-token")) }
            return HTTPProblem.response(status: 401, code: "invalid_session", detail: "Invalid verification code", requestID: "fixture-request")
        }
        if path.hasSuffix("/auth/otp/challenges") { return reply(201, AuthenticationFixture.challenge) }
        if path.contains("/modems") {
            if requiresOTP && request.value(forHTTPHeaderField: "Authorization") != "Bearer fixture-token" {
                return HTTPProblem.response(status: 401, code: "invalid_session", detail: "Login required")
            }
            if path.hasSuffix("/ses") { return reply(200, #"{"ses":[]}"#) }
            if path.hasSuffix("/esims") { return reply(200, #"{"ses":[]}"#) }
            if tracksRefreshes {
                modemReads += 1
                let modem = Self.modemJSON(name: "Read \(modemReads)")
                    .replacingOccurrences(of: "Example Mobile", with: "Read \(modemReads)")
                    .replacingOccurrences(of: #""simKind":"euicc""#, with: #""simKind":"unknown""#)
                return reply(200, path.hasSuffix("/modems") ? "[\(modem)]" : modem)
            }
            if multipleDevices && host == "home.sigmo.test" {
                let first = Self.modemJSON(name: "Home Modem", wifiCalling: true, volte: true)
                    .replacingOccurrences(of: "Example Mobile", with: "CMCC")
                let second = Self.modemJSON(name: "Travel Modem with a long descriptive device name", id: "device-2", number: "")
                return reply(200, path.hasSuffix("/modems") ? "[\(first),\(second)]" : (path.hasSuffix("device-2") ? second : first))
            }
            let modem = Self.modemJSON(name: host == "home.sigmo.test" ? "Home Modem" : "Studio Modem", wifiCalling: host == "home.sigmo.test", volte: host == "home.sigmo.test", airplaneMode: airplaneMode, imsEnabled: imsEnabled)
            return reply(200, path.hasSuffix("/modems") ? "[\(modem)]" : modem)
        }
        return HTTPProblem.response(status: 404, code: "resource_not_found", detail: "Not found")
    }

    private func reply(_ status: Int, _ body: String) -> BackendHTTPResponse {
        BackendHTTPResponse(data: Data(body.utf8), status: status)
    }
    private func pairing(status: String, httpStatus: Int) -> BackendHTTPResponse {
        let expiry = Date().addingTimeInterval(300).ISO8601Format()
        return reply(httpStatus, #"{"id":"fixture","activationUrl":"https://t.me/SigmoProBot?start=fixture","status":"\#(status)","expiresAt":"\#(expiry)"}"#)
    }
    static func modemJSON(name: String = "Test Modem", id: String = "device-1", wifiCalling: Bool = false, volte: Bool = false, airplaneMode: Bool = false, imsEnabled: Bool = true, number: String = "+86 138 0000 0000") -> String {
        #"{"id":"\#(id)","name":"\#(name)","manufacturer":"Quectel","primaryPort":"/dev/cdc-wdm0","firmwareRevision":"1.0","hardwareRevision":"1","number":"\#(number)","state":"registered","unlockRequired":"none","unlockSupported":false,"sim":{"slot":1,"active":true,"operatorName":"Example Mobile","operatorIdentifier":"46000","regionCode":"CN","identifier":"8986000000000000000"},"slots":null,"accessTechnology":"lte","registrationState":"home","registeredOperator":{"name":"Example Mobile","code":"46000"},"signalQuality":78,"airplaneMode":\#(airplaneMode),"simKind":"euicc","internetConnected":\#(!airplaneMode),"wifiCallingEnabled":\#(imsEnabled),"wifiCallingConnected":\#(imsEnabled && wifiCalling),"volteEnabled":\#(imsEnabled),"volteConnected":\#(imsEnabled && volte && !airplaneMode)}"#
    }
}
#endif

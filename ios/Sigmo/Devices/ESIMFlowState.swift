import Foundation
import Observation

enum ESIMFlowPhase: Equatable {
    case form, sources, profiles, connecting, progress, preview, confirmation, userInput, sourceDeletion, websheet, completed, failed
    var running: Bool {
        switch self {
        case .connecting, .progress, .preview, .confirmation, .userInput, .sourceDeletion, .websheet: true
        default: false
        }
    }
}

struct TransferPrompt: Decodable, Sendable {
    let text: String
    let acceptLabel: String?
    let rejectLabel: String?
    let freeText: Bool
    let freeTextHint: String?
}

struct ESIMServerMessage: Decodable, Sendable {
    let type: String
    let stage: String?
    let profile: ESIMPreview?
    let message: String?
    let input: TransferPrompt?
    let iccid: String?
    let websheet: CarrierWebsheet?
}

struct ESIMClientMessage: Encodable, Sendable {
    let type: String
    var seId: String?
    var smdp: String?
    var activationCode: String?
    var confirmationCode: String?
    var sourceType: String?
    var sourceId: String?
    var profileId: String?
    var sourceImei: String?
    var accept: Bool?
    var code: String?
    var response: String?
}

@MainActor @Observable
final class ESIMFlowState: Identifiable {
    let id = UUID()
    let detail: ModemDetailState
    let seID: String
    var input = LPAInput()
    var confirmation = ""
    var response = ""
    var sourceIMEI = ""
    private(set) var phase: ESIMFlowPhase = .form
    private(set) var transfer = false
    private(set) var stage = ""
    private(set) var failure: BackendFailure?
    private(set) var busy = false
    private(set) var sending = false
    private(set) var discoveries: [ESIMDiscovery]?
    private(set) var didDiscover = false
    private(set) var sources: [TransferSource] = []
    private(set) var source: TransferSource?
    private(set) var profiles: [TransferProfile] = []
    private(set) var selectedProfile: TransferProfile?
    private(set) var ccidError: String?
    private(set) var preview: ESIMPreview?
    private(set) var prompt: TransferPrompt?
    private(set) var deletionICCID = ""
    private(set) var websheet: CarrierWebsheet?
    private(set) var websheetURL: URL?
    private(set) var runID: UUID?
    @ObservationIgnored private var socket: BackendSocket?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var profilesIMEI: String?

    init(detail: ModemDetailState, seID: String) {
        self.detail = detail
        self.seID = seID
    }
    var validSE: Bool { detail.elements.contains { $0.id == seID } }
    private var normalizedSourceIMEI: String {
        sourceIMEI.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var canStart: Bool {
        phase == .form && validSE && input.valid && !busy
    }

    var canTransfer: Bool {
        phase == .profiles && validSE && !busy && selectedProfile?.supported == true
            && profilesIMEI == normalizedSourceIMEI
            && (source?.requiresSourceImei != true || !normalizedSourceIMEI.isEmpty)
    }

    func discover() async {
        guard phase == .form, validSE, !busy, !didDiscover, !Task.isCancelled else { return }
        // One lookup belongs to each Add eSIM attempt, before presenting its setup flow.
        didDiscover = true
        busy = true
        failure = nil
        discoveries = nil
        let current = revision
        let id = detail.modemID
        let se = seID
        defer {
            if current == revision { busy = false }
        }
        do {
            let values = try await detail.session.protectedRequest { api, token in try await api.discoverProfiles(id, seID: se, token: token) }
            guard current == revision, !Task.isCancelled else { return }
            discoveries = values
        } catch {
            report(error, current: current)
        }
    }
    func selectDiscovery(_ value: ESIMDiscovery) {
        input = LPAInput(smdp: LPAInput.compact(value.address))
    }
    func loadSources() async {
        guard !phase.running, detail.canTransfer, validSE, !busy else { return }
        busy = true
        failure = nil
        discoveries = nil
        let current = revision
        let id = detail.modemID
        defer {
            if current == revision { busy = false }
        }
        do {
            let result = try await detail.session.protectedRequest { api, token in try await api.transferSources(id, token: token) }
            guard current == revision, !Task.isCancelled else { return }
            sources = result.sources
            ccidError = result.ccidError
            transfer = true
            // Changing the step removes its initiating button and cancels that button's task.
            // Publish the destination only after the read has completed.
            phase = .sources
        } catch { report(error, current: current) }
    }
    func selectSource(_ value: TransferSource) {
        guard phase == .sources, !busy else { return }
        source = value
        sourceIMEI = value.requiresSourceImei ? detail.modemID : ""
        profiles = []
        profilesIMEI = nil
        selectedProfile = nil
        phase = .profiles
        failure = nil
    }
    func loadProfiles() async {
        guard phase == .profiles, let source, !busy,
              !source.requiresSourceImei || !normalizedSourceIMEI.isEmpty else { return }
        busy = true
        failure = nil
        profiles = []
        profilesIMEI = nil
        selectedProfile = nil
        let current = revision
        let id = detail.modemID
        let imei = normalizedSourceIMEI
        defer {
            if current == revision { busy = false }
        }
        do {
            let result = try await detail.session.protectedRequest { api, token in try await api.transferProfiles(id, source: source, imei: imei, token: token) }
            guard current == revision, !Task.isCancelled,
                  self.source == source, normalizedSourceIMEI == imei else { return }
            profiles = result
            profilesIMEI = imei
        } catch {
            guard self.source == source, normalizedSourceIMEI == imei else { return }
            report(error, current: current)
        }
    }

    func selectProfile(_ value: TransferProfile) {
        guard phase == .profiles, !busy, profilesIMEI == normalizedSourceIMEI,
              let profile = profiles.first(where: { $0.id == value.id }), profile.supported else { return }
        selectedProfile = profile
    }
    func backToForm() {
        guard !phase.running else { return }
        revision += 1
        runID = nil
        busy = false
        transfer = false
        phase = .form
        failure = nil
        discoveries = nil
    }
    func applyLPA() {
        guard var parsed = LPAInput.parse(input.smdp) else { return }
        parsed.confirmationCode = input.confirmationCode
        input = parsed
    }
    func startDownload() {
        applyLPA()
        guard canStart, !phase.running else { return }
        transfer = false
        prepare()
    }
    func startTransfer() {
        guard canTransfer, detail.canTransfer, !phase.running else { return }
        transfer = true
        prepare()
    }
    private func prepare() {
        revision += 1
        busy = false
        sending = false
        failure = nil
        preview = nil
        prompt = nil
        response = ""
        confirmation = ""
        stage = ""
        phase = .connecting
        runID = UUID()
    }

    func run() async {
        guard let runID, phase == .connecting else { return }
        let current = revision
        let id = detail.modemID
        let isTransfer = transfer
        let path = ["modems", id, isTransfer ? "esim-transfers" : "esim-downloads", "sessions"]
        var start = ESIMClientMessage(type: "start", seId: seID)
        if isTransfer {
            start.sourceType = source?.type
            start.sourceId = source?.id
            start.profileId = selectedProfile?.id
            start.sourceImei = normalizedSourceIMEI
        } else {
            start.smdp = LPAInput.compact(input.smdp)
            start.activationCode = LPAInput.compact(input.activationCode)
            start.confirmationCode = input.confirmationCode.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        let data: Data
        do {
            data = try JSONEncoder().encode(start)
        } catch {
            report(error, current: current)
            phase = .failed
            return
        }
        input = LPAInput()
        do {
            await detail.session.invalidateModemReads(id)
            try await detail.session.protectedRequest { api, token in
                let connection = try await api.socket(path, token: token)
                try await withTaskCancellationHandler {
                    try await self.exchange(connection, start: data, runID: runID, api: api, token: token)
                } onCancel: { connection.close() }
            }
        } catch {
            guard current == revision, self.runID == runID, phase != .completed else { return }
            phase = .failed
            failure = Task.isCancelled ? .local("detail.interrupted") : .wrapping(error)
        }
        if current == revision, !Task.isCancelled { await reconcile() }
    }

    private func exchange(_ connection: BackendSocket, start: Data, runID: UUID, api: BackendAPI, token: String?) async throws {
        guard self.runID == runID, !Task.isCancelled else { connection.close(); throw CancellationError() }
        socket = connection
        defer { connection.close(); if self.runID == runID { socket = nil } }
        try await connection.send(start)
        while !Task.isCancelled, self.runID == runID {
            let data = try await connection.receive()
            try Task.checkCancellation()
            guard self.runID == runID, detail.session.canAccessDevices else { throw CancellationError() }
            let event = try JSONDecoder().decode(ESIMServerMessage.self, from: data)
            if let sheet = event.websheet, event.type == "websheet" {
                websheetURL = try CarrierWebsheetAddress.url(sheet, api: api, token: token)
            }
            try consume(event)
            if phase == .completed || phase == .failed { return }
        }
        throw CancellationError()
    }

    func consume(_ event: ESIMServerMessage) throws {
        guard phase.running else { return }
        switch event.type {
        case "progress": stage = event.stage ?? stage; phase = .progress
        case "preview":
            guard let profile = event.profile else { throw BackendFailure.local("api.invalidResponse") }
            preview = profile
            // Transfer previews are informative; its protocol has no confirm message.
            if !transfer { phase = .preview }
        case "confirmation_code_required" where !transfer: confirmation = ""; phase = .confirmation
        case "user_input" where transfer:
            guard let input = event.input else { throw BackendFailure.local("api.invalidResponse") }
            prompt = input; response = ""; phase = .userInput
        case "source_deletion" where transfer:
            guard let iccid = event.iccid, !iccid.isEmpty else { throw BackendFailure.local("api.invalidResponse") }
            deletionICCID = iccid; phase = .sourceDeletion
        case "websheet" where transfer:
            guard let sheet = event.websheet else { throw BackendFailure.local("api.invalidResponse") }
            websheet = sheet; phase = .websheet
        case "completed": phase = .completed; websheet = nil; websheetURL = nil; confirmation = ""; response = ""
        case "error": phase = .failed; failure = BackendFailure(code: "esim_flow", message: event.message ?? detailText("detail.operationFailed"))
        default: throw BackendFailure.local("api.invalidResponse")
        }
    }

    func send(_ message: ESIMClientMessage) async {
        guard !Task.isCancelled, let socket, phase.running, !sending, detail.session.canAccessDevices else { return }
        sending = true
        let current = revision
        let previousPhase = phase
        defer {
            if current == revision { sending = false }
        }
        do {
            try await socket.send(JSONEncoder().encode(message))
            guard current == revision, !Task.isCancelled else { return }
            confirmation = ""
            response = ""
            if message.type != "cancel", phase == previousPhase { phase = .progress }
        } catch {
            guard current == revision, !Task.isCancelled else { return }
            if phase != .completed { phase = .failed; failure = .wrapping(error); socket.close() }
        }
    }
    func completeWebsheet(_ callback: CarrierCallback) async {
        guard !Task.isCancelled, let websheet, phase == .websheet, !sending else { return }
        sending = true
        let current = revision
        defer {
            if current == revision { sending = false }
        }
        do {
            try await detail.session.protectedRequest { api, token in try await api.carrierCallback(websheet.id, callback: callback, token: token) }
            guard current == revision, !Task.isCancelled, self.websheet?.id == websheet.id else { return }
            if phase == .websheet { self.websheet = nil; websheetURL = nil; phase = .progress }
        } catch { report(error, current: current) }
    }
    func cancel() async {
        let current = revision
        if let socket {
            defer { socket.close() }
            do { try await socket.send(JSONEncoder().encode(ESIMClientMessage(type: "cancel"))) }
            catch { /* Closing also cancels the server session when delivery is impossible. */ }
        }
        // A suspended cancellation owns its original session, never a replacement run.
        guard current == revision else { return }
        interrupt()
        await reconcile()
    }
    func interrupt() {
        revision += 1
        socket?.close()
        socket = nil
        runID = nil
        input = LPAInput()
        confirmation = ""
        response = ""
        websheetURL = nil
        websheet = nil
        busy = false
        sending = false
        if phase.running {
            phase = .failed
            failure = .local("detail.interrupted")
        }
    }
    func reconcile() async {
        let current = revision
        let sourceID = transfer && source?.type == "modem" ? source?.id : nil
        await detail.session.invalidateModemReads(detail.modemID)
        guard current == revision, !Task.isCancelled else { return }
        await detail.load()
        guard current == revision, !Task.isCancelled else { return }
        if let sourceID, sourceID != detail.modemID {
            await detail.session.refreshModem(sourceID)
        }
    }
    private func report(_ error: Error, current: Int) {
        if revision == current, !Task.isCancelled { failure = .wrapping(error) }
    }
}

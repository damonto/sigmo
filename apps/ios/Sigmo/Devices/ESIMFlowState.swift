import Foundation
import Observation

enum ESIMFlowPhase: Equatable {
    case form, sources, profiles, connecting, progress, preview, confirmation, userInput, sourceDeletion, websheet, completed, cancelled, failed
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
    var version: Int? = nil
    var card: RemoteCardRequest? = nil
    var profiles: [TransferProfile]? = nil
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
    var version: Int?
    var card: RemoteCardReply?
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
    private(set) var cancelling = false
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
    @ObservationIgnored private var operation: Task<Void, Error>?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var profilesIMEI: String?
    @ObservationIgnored private let smartCards: SmartCardAccess
    @ObservationIgnored private var remoteCard: RemoteAPDUSession?
    @ObservationIgnored private var stopReaderObservation: (() -> Void)?

    init(detail: ModemDetailState, seID: String, smartCards: SmartCardAccess? = nil) {
        self.detail = detail
        self.seID = seID
        self.smartCards = smartCards ?? (detail.session.isDemo ? .demo : .system)
        if detail.session.isDemo { sourceIMEI = "490154203237518"; confirmation = "123456" }
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
        input = LPAInput(smdp: LPAInput.compact(value.address), activationCode: value.eventId)
    }
    func loadSources() async {
        guard !phase.running, detail.canTransfer, validSE, !busy else { return }
        closeRemoteCard()
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
            refreshLocalReaders()
            if stopReaderObservation == nil {
                stopReaderObservation = smartCards.observe { [weak self] in self?.refreshLocalReaders() }
            }
            ccidError = result.ccidError
            transfer = true
            // Changing the step removes its initiating button and cancels that button's task.
            // Publish the destination only after the read has completed.
            phase = .sources
        } catch { report(error, current: current) }
    }
    func selectSource(_ value: TransferSource) {
        guard phase == .sources, !busy else { return }
        guard value.type != "client_ccid" || sources.contains(where: { $0.key == value.key && $0.cardPresent == true }) else { return }
        closeRemoteCard()
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
            let result: [TransferProfile]
            if source.type == "client_ccid" {
                result = try await loadLocalProfiles(source: source, imei: imei)
            } else {
                result = try await detail.session.protectedRequest { api, token in try await api.transferProfiles(id, source: source, imei: imei, token: token) }
            }
            guard current == revision, !Task.isCancelled,
                  self.source == source, normalizedSourceIMEI == imei else { return }
            profiles = result.filter(\.supported)
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
        stopReaderObservation?()
        stopReaderObservation = nil
        closeRemoteCard()
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
        if source?.type == "client_ccid", remoteCard?.closed != false {
            failure = .local("detail.remoteCardInterrupted")
            return
        }
        transfer = true
        prepare()
    }
    private func prepare() {
        revision += 1
        busy = false
        sending = false
        cancelling = false
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
        let local = remoteCard
        let operation = Task {
            await detail.session.invalidateModemReads(id)
            try await detail.session.protectedRequest { api, token in
                try await BackgroundExecution.run(name: "eSIM operation") {
                    let connection: BackendSocket
                    if let local { connection = local.socket }
                    else { connection = try await api.socket(path, token: token) }
                    try await withTaskCancellationHandler {
                        try await self.exchange(connection, start: data, runID: runID, api: api, token: token, remote: local)
                    } onCancel: { connection.close() }
                }
            }
        }
        self.operation = operation
        defer { if self.runID == runID { self.operation = nil } }
        do {
            try await withTaskCancellationHandler {
                try await operation.value
            } onCancel: { operation.cancel() }
        } catch {
            guard current == revision, self.runID == runID, phase != .completed else { return }
            if cancelling {
                phase = .cancelled
            } else {
                phase = .failed
                if local != nil { failure = .local("detail.remoteCardInterrupted") }
                else { failure = Task.isCancelled || error is CancellationError ? .local("detail.interrupted") : .wrapping(error) }
            }
        }
        if current == revision, !Task.isCancelled, !cancelling { await reconcile() }
    }

    private func exchange(_ connection: BackendSocket, start: Data, runID: UUID, api: BackendAPI, token: String?, remote: RemoteAPDUSession? = nil) async throws {
        guard self.runID == runID, !Task.isCancelled else { connection.close(); throw CancellationError() }
        socket = connection
        defer { connection.close(); remote?.close(); if self.runID == runID { socket = nil } }
        try await connection.send(start)
        while !Task.isCancelled, self.runID == runID {
            let event: ESIMServerMessage
            if let remote { event = try await remote.receive() }
            else { event = try JSONDecoder().decode(ESIMServerMessage.self, from: await connection.receive()) }
            try Task.checkCancellation()
            guard self.runID == runID, detail.session.canAccessDevices else { throw CancellationError() }
            if let sheet = event.websheet, event.type == "websheet" {
                websheetURL = try CarrierWebsheetAddress.url(sheet, api: api)
            }
            try consume(event)
            if phase == .completed || phase == .cancelled || phase == .failed { return }
        }
        throw CancellationError()
    }

    func consume(_ event: ESIMServerMessage) throws {
        guard phase.running else { return }
        if cancelling {
            if event.type == "completed" || event.type == "error" { phase = .cancelled }
            return
        }
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
        guard !Task.isCancelled, let socket, phase.running, !sending, !cancelling, detail.session.canAccessDevices else { return }
        sending = true
        let current = revision
        let previousPhase = phase
        defer {
            if current == revision { sending = false }
        }
        do {
            try await socket.send(JSONEncoder().encode(message))
            guard current == revision, !Task.isCancelled, !cancelling else { return }
            confirmation = ""
            response = ""
            if message.type != "cancel", phase == previousPhase { phase = .progress }
        } catch {
            guard current == revision, !Task.isCancelled, !cancelling else { return }
            if phase != .completed { phase = .failed; failure = .wrapping(error); socket.close() }
        }
    }
    func completeWebsheet(_ callback: CarrierCallback, delivery: CarrierCallbackDelivery.Status = .native) async {
        guard delivery != .failed else { return }
        guard !Task.isCancelled, let websheet, phase == .websheet, !sending, !cancelling else { return }
        sending = true
        let current = revision
        defer {
            if current == revision { sending = false }
        }
        do {
            if delivery == .native {
                try await detail.session.protectedRequest { api, token in try await api.carrierCallback(websheet.id, callback: callback, token: token) }
            }
            guard current == revision, !Task.isCancelled, !cancelling, self.websheet?.id == websheet.id else { return }
            if phase == .websheet { self.websheet = nil; websheetURL = nil; phase = .progress }
        } catch { report(error, current: current) }
    }
    func cancel(gracePeriod: Duration = .seconds(15)) async {
        guard !cancelling else { return }
        let current = revision
        // Once confirmation is in flight, a second confirm cannot undo it.
        let rejectPreview = phase == .preview && !transfer && !sending
        let operation = operation
        cancelling = true
        failure = nil
        if let socket {
            // Bound both sending the request and waiting for server cleanup.
            let timeout = Task {
                try await Task.sleep(for: gracePeriod)
                socket.close()
            }
            defer {
                timeout.cancel()
                socket.close()
            }
            do {
                try await withTaskCancellationHandler {
                    try Task.checkCancellation()
                    let message = rejectPreview ? ESIMClientMessage(type: "confirm", accept: false) : ESIMClientMessage(type: "cancel")
                    try await socket.send(JSONEncoder().encode(message))
                    if rejectPreview, let operation {
                        // Keep the transport alive while the server cancels the eUICC and SM-DP+ session.
                        _ = await operation.result
                    }
                } onCancel: { socket.close() }
            } catch {
                // Closing also cancels the server session when delivery is impossible.
            }
        }
        // A suspended cancellation owns its original session, never a replacement run.
        guard current == revision else { return }
        phase = .cancelled
        interrupt()
        await reconcile()
    }
    func interrupt() {
        revision += 1
        stopReaderObservation?()
        stopReaderObservation = nil
        closeRemoteCard()
        operation?.cancel()
        operation = nil
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
            phase = cancelling ? .cancelled : .failed
            if !cancelling { failure = .local("detail.interrupted") }
        }
        cancelling = false
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
    private func loadLocalProfiles(source: TransferSource, imei: String) async throws -> [TransferProfile] {
        let current = revision
        if remoteCard?.closed == true { closeRemoteCard() }
        do {
            return try await detail.session.protectedRequest { api, token in
                try await BackgroundExecution.run(name: "Read local SIM") {
                    try await self.localProfiles(api: api, token: token, source: source, imei: imei, revision: current)
                }
            }
        } catch {
            if current == revision { closeRemoteCard() }
            throw error
        }
    }

    private func localProfiles(api: BackendAPI, token: String?, source: TransferSource, imei: String, revision: Int) async throws -> [TransferProfile] {
        guard api.address.scheme == "https" else { throw BackendFailure.local("detail.localReaderHTTPS") }
        if remoteCard == nil {
            let connection = try await api.socket(["modems", detail.modemID, "esim-transfers", "sessions"], token: token)
            guard self.revision == revision, self.source == source, !Task.isCancelled else {
                connection.close()
                throw CancellationError()
            }
            let remote = RemoteAPDUSession(socket: connection, source: source)
            remoteCard = remote
            try await remote.connect(access: smartCards) { [weak self, weak remote] in
                guard let self, remoteCard === remote else { return }
                profiles = []
                selectedProfile = nil
                failure = .local("detail.remoteCardInterrupted")
            }
        }
        guard let remoteCard, !remoteCard.closed else { throw BackendFailure.local("detail.remoteCardInterrupted") }
        return try await remoteCard.profiles(imei: imei)
    }

    private func closeRemoteCard() {
        remoteCard?.close()
        remoteCard = nil
    }

    private func refreshLocalReaders() {
        sources.removeAll { $0.type == "client_ccid" }
        sources += smartCards.readers().map {
            TransferSource(type: "client_ccid", id: $0.name, name: $0.name,
                           detail: detailText($0.cardPresent ? "detail.localReader" : "detail.localReaderInsertCard"),
                           requiresSourceImei: true, cardPresent: $0.cardPresent)
        }
    }

    private func report(_ error: Error, current: Int) {
        if revision == current, !Task.isCancelled, !cancelling { failure = .wrapping(error) }
    }
}

import Foundation
import Observation

struct SIMApplicationMenu: Decodable, Sendable {
    let kind: String
    let title: String?
    let items: [Item]
    let defaultItemId: Int?
    let helpAvailable: Bool?
    struct Item: Decodable, Identifiable, Sendable { let id: Int; let label: String }
}

struct SIMApplicationMessage: Decodable, Sendable {
    let type: String
    let available: Bool?
    let profileIccid: String?
    let menu: SIMApplicationMenu?
    let kind: String?
    let title: String?
    let items: [SIMApplicationMenu.Item]?
    let defaultItemId: Int?
    let helpAvailable: Bool?
    let text: String?
    let defaultText: String?
    let minLength: Int?
    let maxLength: Int?
    let hideInput: Bool?
    let yesNo: Bool?
    let highPriority: Bool?
    let userClear: Bool?
    let immediateResponse: Bool?
    let command: String?
    let message: String?

    var resolvedMenu: SIMApplicationMenu? {
        if let menu { return menu }
        guard let kind, ["root", "select-item"].contains(kind) else { return nil }
        return .init(kind: kind, title: title, items: items ?? [], defaultItemId: defaultItemId, helpAvailable: helpAvailable)
    }
}

enum SIMApplicationPage {
    case menu(SIMApplicationMenu)
    case display(SIMApplicationMessage)
    case input(SIMApplicationMessage)
    case inkey(SIMApplicationMessage)
    case confirm(SIMApplicationMessage)
}

struct SIMApplicationReply: Encodable, Sendable {
    let type: String
    var itemId: Int?
    var helpRequested: Bool?
    var text: String?
    var accepted: Bool?
}

@MainActor @Observable
final class SIMApplicationState {
    let session: BackendSession
    let modemID: String
    private(set) var available = false
    private(set) var profileICCID = ""
    private(set) var root: SIMApplicationMenu?
    private(set) var page: SIMApplicationPage?
    private(set) var failure: BackendFailure? {
        didSet {
            if let failure, failure != oldValue {
                AppDiagnostics.record(failure, category: "sim", operation: "simApplication.failed", backendID: session.id)
            }
        }
    }
    private(set) var busy = false
    private(set) var pageRevision = 0
    var presented = false
    var input = ""
    @ObservationIgnored private var socket: BackendSocket?
    @ObservationIgnored private var revision = 0
    private var backgroundExecution: BackgroundExecution?
    @ObservationIgnored private let backgroundAccess: BackgroundExecution.Access

    var observationSuspended: Bool { !session.foreground && backgroundExecution == nil }

    init(session: BackendSession, modemID: String, backgroundAccess: BackgroundExecution.Access = .system) {
        self.session = session
        self.modemID = modemID
        self.backgroundAccess = backgroundAccess
    }

    func observe() async {
        guard session.canAccessDevices, !Task.isCancelled else { return }
        stop()
        failure = nil
        guard beginBackgroundExecution() else { return }
        let current = revision, id = modemID
        do {
            try await session.protectedRequest { api, token in
                let conn = try await api.socket(["modems", id, "sim-application", "sessions"], token: token)
                try await withTaskCancellationHandler {
                    try await self.exchange(conn, current: current)
                } onCancel: { conn.close() }
            }
        } catch {
            guard current == revision else { return }
            stop()
            if !Task.isCancelled, !(error is CancellationError) { failure = .wrapping(error) }
        }
    }
    private func exchange(_ conn: BackendSocket, current: Int) async throws {
        guard current == revision, !Task.isCancelled else { conn.close(); throw CancellationError() }
        socket = conn
        defer { conn.close(); if current == revision { socket = nil } }
        while !Task.isCancelled, current == revision {
            let data = try await conn.receive()
            try Task.checkCancellation()
            guard current == revision, session.canAccessDevices else { throw CancellationError() }
            try consume(JSONDecoder().decode(SIMApplicationMessage.self, from: data))
        }
    }
    func consume(_ value: SIMApplicationMessage) throws {
        pageRevision += 1
        input = value.defaultText ?? ""
        switch value.type {
        case "status":
            profileICCID = value.profileIccid ?? profileICCID
            guard let available = value.available else { return }
            self.available = available
            root = available ? value.resolvedMenu : nil
            if !available { page = nil; presented = false }
            else if presented, let root { page = .menu(root) }
        case "menu":
            guard let menu = value.resolvedMenu else { throw BackendFailure.local("api.invalidResponse") }
            if menu.kind == "root" { root = menu; if presented { page = .menu(menu) } }
            else { page = .menu(menu); presented = true }
        case "display_text": page = .display(value); presented = true
        case "input": page = .input(value); presented = true
        case "inkey": page = .inkey(value); presented = true
        case "confirm": page = .confirm(value); presented = true
        case "error": throw BackendFailure(code: "sim_application", message: value.message ?? detailText("detail.operationFailed"))
        default: throw BackendFailure.local("api.invalidResponse")
        }
        if !presented {
            // The initial status read is complete. An idle subscription is not
            // a background operation, but a SIM interaction must finish or expire.
            endBackgroundExecution()
        } else if value.type != "status" {
            _ = beginBackgroundExecution()
        }
    }
    func open() { if available, let root { page = .menu(root); presented = true } }
    func send(_ reply: SIMApplicationReply) async {
        guard let socket, !busy, session.canAccessDevices, !Task.isCancelled else { return }
        guard beginBackgroundExecution() else { return }
        busy = true
        let current = revision
        let page = pageRevision
        defer { if current == revision { busy = false } }
        do {
            try await socket.send(JSONEncoder().encode(reply))
            guard current == revision, !Task.isCancelled else { return }
            if pageRevision == page { self.page = root.map { .menu($0) }; input = "" }
        } catch {
            guard current == revision, !Task.isCancelled else { return }
            failure = .wrapping(error)
            stop()
        }
    }
    func dismiss() async {
        let current = revision
        let page = pageRevision
        switch self.page {
        case .menu(let menu) where menu.kind == "root": break
        case nil: break
        case .display, .confirm: await send(.init(type: "confirm_response", accepted: false))
        default: await send(.init(type: "back"))
        }
        guard current == revision, page == pageRevision, !Task.isCancelled else { return }
        presented = false
        input = ""
        endBackgroundExecution()
    }

    func stop() {
        revision += 1
        endBackgroundExecution()
        socket?.close(); socket = nil
        available = false; profileICCID = ""; root = nil; page = nil; presented = false; input = ""; busy = false
    }

    private func beginBackgroundExecution() -> Bool {
        if backgroundExecution != nil { return true }
        let current = revision
        backgroundExecution = BackgroundExecution.begin(name: "SIM application", access: backgroundAccess) { [weak self] in
            guard let self, current == self.revision else { return }
            self.failure = .local("detail.interrupted")
            self.stop()
        }
        return backgroundExecution != nil
    }

    private func endBackgroundExecution() {
        backgroundExecution?.end()
        backgroundExecution = nil
    }
}

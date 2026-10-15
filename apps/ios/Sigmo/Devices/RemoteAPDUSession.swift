import Foundation

struct RemoteCardRequest: Codable, Sendable {
    let id: String
    let connectionId: String
    let operation: String
    var command: Data?
}

struct RemoteCardReply: Codable, Sendable {
    let id: String
    let connectionId: String
    var data: Data?
    var error: String?
}

// One socket grants access to one user-selected reader. It never reconnects
// or replays a card command, including when the response was lost.
@MainActor
final class RemoteAPDUSession {
    let socket: BackendSocket
    let source: TransferSource
    private var card: SmartCardConnection?
    private var connectionID: String?
    private var lastRequestID: UInt64 = 0
    private var receiving = false
    private(set) var closed = false

    init(socket: BackendSocket, source: TransferSource) {
        self.socket = socket
        self.source = source
    }

    func connect(access: SmartCardAccess, invalidated: @escaping () -> Void) async throws {
        do {
            guard !closed, card == nil else { throw BackendFailure.local("detail.remoteCardInterrupted") }
            card = try access.bind(source.id) { [weak self] in
                guard let self, !closed else { return }
                close()
                invalidated()
            }
            try await send(ESIMClientMessage(type: "source", sourceType: source.type, sourceId: source.id, version: 1))
            // No APDU may execute before the protocol version is acknowledged.
            let ready = try await readEvent(browsing: true)
            guard ready.type == "source_ready", ready.version == 1 else {
                throw BackendFailure.local("api.invalidResponse")
            }
        } catch {
            close()
            throw error
        }
    }

    func profiles(imei: String) async throws -> [TransferProfile] {
        try await send(ESIMClientMessage(type: "profiles", sourceType: source.type, sourceId: source.id, sourceImei: imei))
        let event = try await receive(browsing: true)
        if event.type == "error" {
            close()
            throw BackendFailure(code: "esim_flow", message: event.message ?? detailText("detail.operationFailed"))
        }
        guard event.type == "profiles" else {
            close()
            throw BackendFailure.local("api.invalidResponse")
        }
        return event.profiles ?? []
    }

    func receive(browsing: Bool = false) async throws -> ESIMServerMessage {
        guard !receiving else {
            close()
            throw BackendFailure.local("detail.remoteCardInterrupted")
        }
        receiving = true
        defer { receiving = false }
        do {
            return try await withTaskCancellationHandler {
                while !closed {
                    let event = try await readEvent(browsing: browsing)
                    guard event.type == "card_request" else { return event }
                    guard let request = event.card else { throw BackendFailure.local("api.invalidResponse") }
                    try await execute(request)
                }
                throw BackendFailure.local("detail.remoteCardInterrupted")
            } onCancel: {
                // Cancellation can arrive while a native card callback is pending.
                self.socket.close()
                Task { @MainActor [weak self] in self?.close() }
            }
        } catch {
            close()
            throw error
        }
    }

    private func readEvent(browsing: Bool) async throws -> ESIMServerMessage {
        // Bound protocol handshakes and profile reads while allowing the user
        // to take as long as needed at interactive carrier prompts.
        let timeout: Task<Void, Error>? = browsing ? Task { [weak self] in
            try await Task.sleep(for: .seconds(45))
            self?.close()
        } : nil
        defer { timeout?.cancel() }
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            guard !closed else { throw BackendFailure.local("detail.remoteCardInterrupted") }
            let data = try await socket.receive()
            try Task.checkCancellation()
            guard !closed else { throw BackendFailure.local("detail.remoteCardInterrupted") }
            return try JSONDecoder().decode(ESIMServerMessage.self, from: data)
        } onCancel: { self.socket.close() }
    }

    private func send(_ message: ESIMClientMessage) async throws {
        do {
            try await withTaskCancellationHandler {
                try Task.checkCancellation()
                guard !closed else { throw BackendFailure.local("detail.remoteCardInterrupted") }
                try await socket.send(JSONEncoder().encode(message))
                try Task.checkCancellation()
            } onCancel: { self.socket.close() }
        } catch {
            close()
            throw error
        }
    }

    private func execute(_ request: RemoteCardRequest) async throws {
        guard !closed, let card, lastRequestID < UInt64.max,
              let id = UInt64(request.id), id == lastRequestID + 1, !request.connectionId.isEmpty else {
            close()
            throw BackendFailure.local("detail.remoteCardInterrupted")
        }
        lastRequestID = id
        var reply = RemoteCardReply(id: request.id, connectionId: request.connectionId)
        do {
            switch request.operation {
            case "open":
                guard connectionID == nil, request.command == nil, request.connectionId == request.id else {
                    throw BackendFailure.local("api.invalidResponse")
                }
                try await card.open()
                connectionID = request.connectionId
            case "transmit":
                guard connectionID == request.connectionId, let command = request.command,
                      (4...65544).contains(command.count) else { throw BackendFailure.local("api.invalidResponse") }
                let data = try await card.transmit(command)
                guard (2...65538).contains(data.count) else { throw BackendFailure.local("api.invalidResponse") }
                reply.data = data
            case "close":
                guard connectionID == request.connectionId, request.command == nil else {
                    throw BackendFailure.local("api.invalidResponse")
                }
                card.close()
                connectionID = nil
            default:
                throw BackendFailure.local("api.invalidResponse")
            }
            try Task.checkCancellation()
        } catch {
            // Once a command was submitted, its outcome may be unknown. No retry.
            reply.data = nil
            reply.error = "reader_interrupted"
            if !closed, !Task.isCancelled {
                // Best effort: the transport may already have been lost.
                try? await send(ESIMClientMessage(type: "card_response", card: reply))
            }
            throw BackendFailure.local("detail.remoteCardInterrupted")
        }
        // Sending is outside the operation's catch: a lost reply must never
        // cause a second response or a second execution of the command.
        try await send(ESIMClientMessage(type: "card_response", card: reply))
    }

    func close() {
        guard !closed else { return }
        closed = true
        socket.close()
        card?.invalidate()
        card = nil
        connectionID = nil
    }
}

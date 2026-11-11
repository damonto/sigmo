import Foundation
import Observation

struct PeerTransferTarget: Codable, Sendable {
    let name: String
    let imei: String
    let eid: String
}

struct PeerTransferRequest: Codable, Identifiable, Sendable {
    let id: String
    let revision: Int
    let phase: String
    let remoteAddress: String
    let target: PeerTransferTarget
    let sourceName: String?
    let profile: TransferProfile?
    let stage: String?
    let outcome: String?
    let expiresAt: String
    var sources: [PeerTransferSourceOption]? = nil
    var needsDecision: Bool { phase == "authorization" }
}

struct PeerTransferSnapshot: Decodable, Sendable {
    let type: String
    let requests: [PeerTransferRequest]?
}

struct PeerTransferSourceOption: Codable, Sendable {
    let source: TransferSource
    let profiles: [TransferProfile]
}

struct PeerTransferSelection: Codable, Sendable {
    let sourceType: String
    let sourceId: String
    let profileId: String
    let sourceImei: String?
}

struct PeerTransferDecision: Encodable, Sendable {
    let phase: String
    let revision: Int
    let accept: Bool
    var selection: PeerTransferSelection? = nil
}

// Passive subscriptions may reconnect to obtain a snapshot. Transfer and card
// operations use their original socket and are never replayed by this observer.
@MainActor @Observable
final class PeerTransferRequestsState {
    private(set) var requests: [PeerTransferRequest] = []
    private(set) var sending = false
    private(set) var failure: BackendFailure?
    private var dismissed: Set<String> = []
    @ObservationIgnored private var observationID: UUID?
    var current: PeerTransferRequest? {
        requests.first { !dismissed.contains($0.id) && $0.needsDecision }
            ?? requests.first { !dismissed.contains($0.id) && ["running", "ended"].contains($0.phase) }
    }

    func run(session: BackendSession) async {
        let observation = UUID()
        observationID = observation
        defer { if observationID == observation { requests = [] } }
        while !Task.isCancelled, observationID == observation, session.canAccessDevices, session.foreground {
            do {
                try await session.protectedRequest { api, token in
                    let socket = try await api.socket(["esim-transfer-requests", "events"], token: token)
                    defer { socket.close() }
                    try await withTaskCancellationHandler {
                        while !Task.isCancelled {
                            let event = try JSONDecoder().decode(PeerTransferSnapshot.self, from: await socket.receive())
                            guard event.type == "requests" else { throw BackendFailure.local("api.invalidResponse") }
                            await self.apply(event.requests ?? [], observation: observation)
                        }
                    } onCancel: { socket.close() }
                }
            } catch { if observationID == observation { requests = [] } }
            if Task.isCancelled || observationID != observation { return }
            do { try await Task.sleep(for: .seconds(10)) } catch { return }
        }
    }

    private func apply(_ values: [PeerTransferRequest], observation: UUID) {
        guard observationID == observation, !Task.isCancelled else { return }
        let liveIDs = Set(values.map(\.id))
        dismissed.formIntersection(liveIDs)
        requests = values
    }

    func decide(session: BackendSession, request: PeerTransferRequest, accept: Bool, selection: PeerTransferSelection? = nil) async {
        guard request.needsDecision, !sending, (!accept || selection != nil),
              requests.contains(where: { $0.id == request.id && $0.revision == request.revision && $0.phase == request.phase }) else { return }
        let observation = observationID
        sending = true
        failure = nil
        defer { sending = false }
        do {
            try await session.protectedRequest { api, token in try await api.decidePeerTransfer(request, accept: accept, selection: selection, token: token) }
        } catch {
            guard !Task.isCancelled, observation == observationID,
                  current?.id == request.id, current?.revision == request.revision else { return }
            failure = .wrapping(error)
        }
    }

    func dismiss(_ id: String) { dismissed.insert(id) }
}


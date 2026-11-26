import Foundation
import Observation

struct InternetTrafficResponse: Codable, Sendable {
    let connected: Bool
    let sample: InternetTrafficSample?
    let sessionID: String

    private enum CodingKeys: String, CodingKey {
        case connected, sample
        case sessionID = "sessionId"
    }
}

struct InternetTrafficSample: Codable, Sendable {
    let elapsedMilliseconds: UInt64
    let rxBytes: UInt64
    let txBytes: UInt64
}

struct InternetTrafficHistory {
    struct Rates: Equatable {
        let download: Double
        let upload: Double

        var total: Double { download + upload }
    }

    struct Point {
        let elapsedMilliseconds: UInt64
        let bitsPerSecond: Double
    }

    static let windowMilliseconds: UInt64 = 30_000
    private(set) var rates: Rates?
    private(set) var usedBytes: UInt64?
    private(set) var points: [Point] = []
    private(set) var sessionID: String?
    private var previous: InternetTrafficSample?

    mutating func append(_ sample: InternetTrafficSample?, sessionID: String) {
        if self.sessionID != sessionID || sample == nil {
            self = .init()
            self.sessionID = sessionID
        }
        guard let sample else { return }

        let prior = previous
        if let prior, sample.elapsedMilliseconds <= prior.elapsedMilliseconds { return }

        let (total, overflow) = sample.rxBytes.addingReportingOverflow(sample.txBytes)
        usedBytes = overflow ? .max : total
        previous = sample

        guard let prior, sample.rxBytes >= prior.rxBytes, sample.txBytes >= prior.txBytes else {
            rates = nil
            points.removeAll()
            return
        }
        let elapsed = sample.elapsedMilliseconds - prior.elapsedMilliseconds
        guard elapsed <= Self.windowMilliseconds else {
            rates = nil
            points.removeAll()
            return
        }

        let seconds = Double(elapsed) / 1_000
        let rates = Rates(download: Double(sample.rxBytes - prior.rxBytes) * 8 / seconds,
                          upload: Double(sample.txBytes - prior.txBytes) * 8 / seconds)
        self.rates = rates
        points.removeAll { sample.elapsedMilliseconds - $0.elapsedMilliseconds > Self.windowMilliseconds }
        points.append(Point(elapsedMilliseconds: sample.elapsedMilliseconds, bitsPerSecond: rates.total))
    }

    mutating func missSample() {
        rates = nil
    }
}

@MainActor @Observable
final class InternetTrafficState {
    private(set) var connectedTarget: ModemSIMTarget?
    private(set) var history = InternetTrafficHistory()
    var sessionID: String? { history.sessionID }
    @ObservationIgnored private var observationID: UUID?

    func observe(_ detail: ModemDetailState, target: ModemSIMTarget) async {
        guard !Task.isCancelled, detail.simTarget == target, detail.session.canAccessDevices else { return }
        let observation = UUID()
        observationID = observation
        clear()
        defer {
            if observationID == observation {
                observationID = nil
                clear()
            }
        }

        func isCurrent() -> Bool {
            !Task.isCancelled && observationID == observation
                && detail.simTarget == target && detail.session.canAccessDevices
        }

        let modemID = detail.modemID
        var lastResponse = ContinuousClock.now
        while isCurrent() {
            do {
                let result = try await detail.session.protectedRequest { api, token in
                    try await api.internetTraffic(modemID, token: token)
                }
                guard isCurrent() else { return }
                lastResponse = .now
                if result.connected, !result.sessionID.isEmpty {
                    history.append(result.sample, sessionID: result.sessionID)
                    if connectedTarget != target { connectedTarget = target }
                } else {
                    clear()
                }
            } catch {
                guard isCurrent() else { return }
                if lastResponse.duration(to: .now) >= .seconds(5) {
                    clear()
                } else {
                    // The next cumulative sample can recover the rate over a missed interval.
                    history.missSample()
                }
            }
            do {
                try await Task.sleep(for: .seconds(connectedTarget == nil ? 5 : 1))
            } catch {
                return
            }
        }
    }

    private func clear() {
        guard connectedTarget != nil else { return }
        connectedTarget = nil
        history = .init()
    }
}

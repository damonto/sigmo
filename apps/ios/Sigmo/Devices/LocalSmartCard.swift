import Foundation
@preconcurrency import CryptoTokenKit

struct LocalCardReader: Sendable {
    let name: String
    let cardPresent: Bool
}

// Keep the native reader replaceable without exposing APDU interpretation to iOS.
@MainActor
struct SmartCardAccess {
    var readers: () -> [LocalCardReader]
    var bind: (String, @escaping @MainActor () -> Void) throws -> SmartCardConnection
    var observe: (@escaping @MainActor () -> Void) -> (() -> Void) = { _ in {} }

    static let system = SmartCardAccess(
        readers: {
            guard let manager = TKSmartCardSlotManager.default else { return [] }
            return manager.slotNames.map { name in
                LocalCardReader(name: name, cardPresent: manager.slotNamed(name)?.state == .validCard)
            }
        },
        bind: { name, invalidated in
            guard let slot = TKSmartCardSlotManager.default?.slotNamed(name), slot.state == .validCard,
                  let card = slot.makeSmartCard() else {
                throw BackendFailure.local("detail.localReaderUnavailable")
            }
            let reader = LocalSmartCard(slot: slot, card: card, invalidated: invalidated)
            return SmartCardConnection(open: { try await reader.open() },
                                       transmit: { try await reader.transmit($0) },
                                       close: { reader.close() }, invalidate: { reader.invalidate() })
        },
        observe: { changed in
            let observation = LocalCardDiscovery(changed: changed)
            return { observation.stop() }
        }
    )
}

@MainActor
private final class LocalCardDiscovery {
    private let manager = TKSmartCardSlotManager.default
    private let changed: @MainActor () -> Void
    private var readers: NSKeyValueObservation?
    private var slots: [TKSmartCardSlot] = []
    private var cards: [NSKeyValueObservation] = []
    private var stopped = false

    init(changed: @escaping @MainActor () -> Void) {
        self.changed = changed
        readers = manager?.observe(\.slotNames, options: [.new]) { [weak self] _, _ in
            Task { @MainActor [weak self] in self?.refresh() }
        }
        refresh()
    }

    private func refresh() {
        guard !stopped else { return }
        cards.forEach { $0.invalidate() }
        // Keep the observed slots alive even while no card session is open.
        slots = (manager?.slotNames ?? []).compactMap { manager?.slotNamed($0) }
        cards = slots.map { slot in
            slot.observe(\.state, options: [.new]) { [weak self] _, _ in
                Task { @MainActor [weak self] in
                    guard let self, !stopped else { return }
                    changed()
                }
            }
        }
        changed()
    }

    func stop() {
        stopped = true
        readers?.invalidate()
        readers = nil
        cards.forEach { $0.invalidate() }
        cards = []
        slots = []
    }
}

@MainActor
struct SmartCardConnection {
    let open: () async throws -> Void
    let transmit: (Data) async throws -> Data
    let close: () -> Void
    let invalidate: () -> Void
}

@MainActor
private final class LocalSmartCard {
    private let slot: TKSmartCardSlot
    private let card: TKSmartCard
    private let invalidated: @MainActor () -> Void
    private var observation: NSKeyValueObservation?
    private var sessionOpen = false
    private var invalid = false
    private var pending: CheckedContinuation<Data, Error>?
    private var requestID = UUID()

    init(slot: TKSmartCardSlot, card: TKSmartCard, invalidated: @escaping @MainActor () -> Void) {
        self.slot = slot
        self.card = card
        self.invalidated = invalidated
        card.isSensitive = true
        // Pin the card across native sessions. A replaced SIM cannot make this
        // object valid again, even before the UI processes the removal event.
        observation = card.observe(\.isValid, options: [.initial, .new]) { [weak self] _, change in
            if change.newValue == false { Task { @MainActor [weak self] in self?.invalidate() } }
        }
    }

    func open() async throws {
        guard !invalid, !sessionOpen, pending == nil, card.isValid else {
            throw BackendFailure.local("detail.localReaderUnavailable")
        }
        _ = try await perform { completion in
            card.beginSession { [weak self, card] success, error in
                Task { @MainActor in
                    // Only a successful beginSession owns an endSession call.
                    // A late success after cancellation must release its own session.
                    if success {
                        guard let self, !self.invalid, card.isValid else {
                            card.endSession()
                            completion(nil, error)
                            return
                        }
                        self.sessionOpen = true
                    }
                    completion(success ? Data() : nil, error)
                }
            }
        }
    }

    func transmit(_ command: Data) async throws -> Data {
        guard !invalid, sessionOpen, card.isValid, command.count <= slot.maxInputLength else {
            throw BackendFailure.local("detail.localReaderUnavailable")
        }
        return try await perform { completion in
            card.transmit(command) { data, error in
                Task { @MainActor in completion(data, error) }
            }
        }
    }

    func close() {
        if sessionOpen { card.endSession() }
        sessionOpen = false
    }

    func invalidate() {
        guard !invalid else { return }
        invalid = true
        observation?.invalidate()
        observation = nil
        let continuation = pending
        pending = nil
        continuation?.resume(throwing: BackendFailure.local("detail.remoteCardInterrupted"))
        close()
        invalidated()
    }

    private func perform(_ start: (@escaping @MainActor (Data?, Error?) -> Void) -> Void) async throws -> Data {
        guard pending == nil, !invalid else { throw BackendFailure.local("detail.remoteCardInterrupted") }
        let id = UUID()
        requestID = id
        let timeout = Task { [weak self] in
            try await Task.sleep(for: .seconds(25))
            guard let self, requestID == id, pending != nil else { return }
            invalidate()
        }
        defer { timeout.cancel() }
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                pending = continuation
                start { [weak self] data, error in
                    guard let self, requestID == id, let pending else { return }
                    self.pending = nil
                    if let data, error == nil { pending.resume(returning: data) }
                    else {
                        pending.resume(throwing: BackendFailure.local("detail.remoteCardInterrupted"))
                        invalidate()
                    }
                }
            }
        } onCancel: {
            Task { @MainActor [weak self] in
                guard let self, requestID == id else { return }
                invalidate()
            }
        }
    }
}

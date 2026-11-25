import Foundation
import UIKit

@MainActor
struct PushArtworkSync {
    let cache: PushArtworkCache

    func synchronize(_ session: BackendSession, backend: String, isCurrent: @escaping @MainActor () -> Bool) async {
        async let contacts: Void = syncContacts(session, backend: backend, isCurrent: isCurrent)
        await syncLines(session, backend: backend, isCurrent: isCurrent)
        await contacts
    }

    private func syncContacts(_ session: BackendSession, backend: String, isCurrent: @MainActor () -> Bool) async {
        do {
            try await session.refreshContacts()
            guard !Task.isCancelled, isCurrent() else { return }
            let contacts = session.savedContacts
            var index: PushArtworkCache.Index = [:]
            var avatars: [(contact: SigmoContact, key: String)] = []
            for contact in contacts where !contact.numbers.isEmpty {
                let key = PushArtworkCache.key(contact.id, contact.updatedAt, contact.hasAvatar ? "avatar" : "none")
                for number in contact.numbers {
                    if !number.number.isEmpty { index[PushArtworkCache.rawNumberKey(number.number), default: []].insert(key) }
                    if !number.key.isEmpty { index[PushArtworkCache.numberKey(number.key), default: []].insert(key) }
                }
                if contact.hasAvatar { avatars.append((contact, key)) }
            }
            try cache.publish(index, in: .contacts, backend: backend)
            let missing = await cache.missingImages(avatars.map(\.key), in: .contacts, backend: backend)
            for (contact, key) in avatars where missing.contains(key) {
                guard !Task.isCancelled, isCurrent() else { return }
                do {
                    let data = try await session.protectedRequest { api, token in try await api.contactAvatar(contact.id, token: token) }
                    let image = try await AvatarImage.load(data, maximumSize: PushArtworkCache.maximumSize)
                    guard !Task.isCancelled, isCurrent() else { return }
                    guard session.savedContacts.contains(contact), let bitmap = image.cgImage,
                          let png = PushArtworkCache.png(bitmap) else { continue }
                    try cache.store(png, as: key, in: .contacts, backend: backend)
                } catch {
                    // An unavailable avatar can be retried on the next refresh.
                }
            }
        } catch {
            // Keep the last successful snapshot when the backend is temporarily unavailable.
        }
    }

    private func syncLines(_ session: BackendSession, backend: String, isCurrent: @MainActor () -> Bool) async {
        guard !Task.isCancelled, isCurrent(), session.hasLoadedModems else { return }
        var index: PushArtworkCache.Index = [:]
        var logos: [(modem: String, sim: SIMSlot, source: CarrierLogoArtwork.Source, key: String)] = []
        for modem in session.modems where modem.sim.active && !modem.sim.identifier.isEmpty {
            let sim = modem.sim
            let source = CarrierLogoArtwork.Source(icon: sim.icon, catalogURL: AppLaunch.usesFixtures ? nil : CarrierLogoArtwork.catalogURL(plmn: sim.operatorIdentifier))
            guard source.icon != nil || source.catalogURL != nil else { continue }
            let key = PushArtworkCache.key(modem.id, sim.identifier, source.icon ?? "", source.catalogURL?.absoluteString ?? "")
            index[PushArtworkCache.lineKey(modem: modem.id, profile: sim.identifier)] = [key]
            logos.append((modem.id, sim, source, key))
        }
        do { try cache.publish(index, in: .lines, backend: backend) }
        catch { return }
        let missing = await cache.missingImages(logos.map(\.key), in: .lines, backend: backend)
        for (modem, sim, source, key) in logos where missing.contains(key) {
            guard !Task.isCancelled, isCurrent() else { return }
            guard let artwork = await source.load(samplesTint: false, maximumSize: PushArtworkCache.maximumSize),
                  !Task.isCancelled, isCurrent(),
                  session.modems.contains(where: { $0.id == modem && $0.sim == sim }),
                  let png = PushArtworkCache.png(artwork.image) else { continue }
            try? cache.store(png, as: key, in: .lines, backend: backend)
        }
    }
}

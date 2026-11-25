import UIKit
import XCTest
@testable import Sigmo

@MainActor
final class PushArtworkTests: XCTestCase {
    func testMissingArtworkUsesTheLogoBundledInTheExtension() throws {
        let directory = try XCTUnwrap(Bundle.main.builtInPlugInsURL)
        let bundle = try XCTUnwrap(Bundle(url: directory.appending(path: "SigmoNotificationService.appex")))
        let cache = try makeCache()
        let sms = PushContent(kind: "sms", backendId: "home", backendName: "Home", modemId: "modem", profileId: "line", sender: "10086")
        let logo = try XCTUnwrap(PushArtworkCache.notificationImage(for: sms, cache: nil, bundle: bundle))
        assertWidth(PushArtworkCache.maximumSize, logo)
        XCTAssertEqual(PushArtworkCache.notificationImage(for: sms, cache: cache, bundle: bundle), logo)

        let badge = try png(width: 40), key = PushArtworkCache.key("carrier")
        try cache.publish([PushArtworkCache.lineKey(modem: "modem", profile: "line"): [key]], in: .lines, backend: "home")
        try cache.store(badge, as: key, in: .lines, backend: "home")
        XCTAssertEqual(PushArtworkCache.notificationImage(for: sms, cache: cache, bundle: bundle), badge)
        let reminder = PushContent(kind: "reminder", backendId: "home", backendName: "Home")
        XCTAssertNil(PushArtworkCache.notificationImage(for: reminder, cache: cache, bundle: bundle))
    }

    func testSenderArtworkFallsBackToTheExactLine() throws {
        let cache = try makeCache(), backend = "home"
        let avatar = try png(width: 20), logo = try png(width: 40)
        let avatarKey = PushArtworkCache.key("alice", "v1"), logoKey = PushArtworkCache.key("carrier")
        let number = PushArtworkCache.numberKey("+14155552671")
        let raw = PushArtworkCache.rawNumberKey("+1 (415) 555-2671")
        let line = PushArtworkCache.lineKey(modem: "modem", profile: "line")
        try cache.publish([number: [avatarKey], raw: [avatarKey], PushArtworkCache.numberKey("10086"): [avatarKey]], in: .contacts, backend: backend)
        try cache.store(avatar, as: avatarKey, in: .contacts, backend: backend)
        try cache.publish([line: [logoKey]], in: .lines, backend: backend)
        try cache.store(logo, as: logoKey, in: .lines, backend: backend)

        var sms = PushContent(kind: "sms", backendId: backend, backendName: "Home", modemId: "modem", profileId: "line", sender: "+1 (415) 555-2671")
        XCTAssertEqual(cache.image(for: sms), avatar)
        sms.sender = "(+1) 415 555 2671"
        XCTAssertEqual(cache.image(for: sms), avatar)
        sms.sender = "10086"
        XCTAssertEqual(cache.image(for: sms), avatar)
        sms.sender = "BANK10086"
        XCTAssertEqual(cache.image(for: sms), logo)
        sms.sender = "+1 (415) 555-2671"
        try cache.publish([number: [avatarKey, PushArtworkCache.key("different contact")], raw: [avatarKey]], in: .contacts, backend: backend)
        XCTAssertEqual(cache.image(for: sms), logo)

        try cache.publish([number: [avatarKey], raw: [avatarKey]], in: .contacts, backend: backend)
        try cache.store(Data("broken PNG".utf8), as: avatarKey, in: .contacts, backend: backend)
        XCTAssertEqual(cache.image(for: sms), logo)
        XCTAssertNil(cache.image(for: PushContent(kind: "reminder", backendId: backend, backendName: "Home", modemId: "modem", profileId: "line")))
        XCTAssertNil(cache.image(for: PushContent(kind: "sms", backendId: "work", backendName: "Work", modemId: "modem", profileId: "line")))
        sms.profileId = "new-line"
        XCTAssertNil(cache.image(for: sms))
        try cache.remove(backend: backend)
        sms.profileId = "line"
        XCTAssertNil(cache.image(for: sms))
    }

    func testSyncRepairsCorruptAvatarAndTracksSwappedSIM() async throws {
        let cache = try makeCache(), sync = PushArtworkSync(cache: cache), server = CommunicationFixture()
        let avatar = try png(width: 20), originalLogo = try png(width: 30), newLogo = try png(width: 40)
        let configuration = try testConfiguration()
        let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
            api: BackendAPI(configuration: configuration) { try await server.respond($0) })
        await server.setModems([try modem(profile: "line", icon: originalLogo)])
        await session.refresh(force: true)
        _ = try await session.protectedRequest { api, token in try await api.saveContactAvatar("alice", data: avatar, token: token) }
        let backend = "home", sender = "+14155552671"
        var sms = PushContent(kind: "sms", backendId: backend, backendName: "Home", modemId: "device-1", profileId: "line", sender: sender)
        await sync.synchronize(session, backend: backend) { session.canAccessDevices }
        assertWidth(20, cache.image(for: sms))
        sms.sender = "unknown"
        assertWidth(30, cache.image(for: sms))
        sms.sender = sender

        await sync.synchronize(session, backend: backend) { session.canAccessDevices }
        let reads = await server.allRequests().filter { $0.httpMethod == "GET" && $0.url?.path.hasSuffix("/avatar") == true }
        XCTAssertEqual(reads.count, 1)
        let contact = try XCTUnwrap(session.savedContacts.first)
        let avatarKey = PushArtworkCache.key(contact.id, contact.updatedAt, "avatar")
        try cache.store(Data(repeating: 0, count: 256 * 1024), as: avatarKey, in: .contacts, backend: backend)
        assertWidth(30, cache.image(for: sms))
        await sync.synchronize(session, backend: backend) { session.canAccessDevices }
        assertWidth(20, cache.image(for: sms))

        _ = try await session.protectedRequest { api, token in try await api.saveContactAvatar("alice", data: nil, token: token) }
        try await session.refreshContacts(force: true)
        await server.setModems([try modem(profile: "new-line", icon: newLogo)])
        await session.refresh(force: true)
        await sync.synchronize(session, backend: backend) { session.canAccessDevices }
        XCTAssertNil(cache.image(for: sms))
        sms.profileId = "new-line"
        assertWidth(40, cache.image(for: sms))
    }

    private func makeCache() throws -> PushArtworkCache {
        let directory = URL.temporaryDirectory.appending(path: "PushArtwork-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        return PushArtworkCache(directory: directory)
    }

    private func modem(profile: String, icon: Data) throws -> Modem {
        let json = PreviewBackendServer.modemJSON()
            .replacingOccurrences(of: "8986000000000000000", with: profile)
            .replacingOccurrences(of: #""sim":{"#, with: #""sim":{"icon":"data:image/png;base64,\#(icon.base64EncodedString())","#)
        return try JSONDecoder().decode(Modem.self, from: Data(json.utf8))
    }

    private func png(width: Int) throws -> Data {
        let format = UIGraphicsImageRendererFormat(); format.scale = 1
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: width, height: width), format: format)
        return try XCTUnwrap(renderer.image { context in
            UIColor.blue.setFill()
            context.fill(CGRect(x: 0, y: 0, width: width, height: width))
        }.pngData())
    }

    private func assertWidth(_ width: Int, _ image: Data?, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(image.flatMap { UIImage(data: $0)?.cgImage?.width }, width, file: file, line: line)
    }
}

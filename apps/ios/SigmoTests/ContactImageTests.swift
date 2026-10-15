import UIKit
import XCTest
@testable import Sigmo

@MainActor
final class ContactImageTests: XCTestCase {
    func testImportDownsamplesBeforeCreatingThePreview() async throws {
        let format = UIGraphicsImageRendererFormat(); format.scale = 1
        let source = UIGraphicsImageRenderer(size: CGSize(width: 4096, height: 2048), format: format).image { context in
            UIColor.red.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 4096, height: 2048))
        }
        let image = try await AvatarImage.load(XCTUnwrap(source.jpegData(compressionQuality: 0.8)), maximumSize: 1024)
        XCTAssertEqual(image.cgImage?.width, 1024)
        XCTAssertEqual(image.cgImage?.height, 512)
        do {
            _ = try await AvatarImage.load(Data("invalid".utf8))
            XCTFail("Invalid image must be rejected")
        } catch { XCTAssertEqual((error as? BackendFailure)?.code, "communication.invalidPhoto") }
    }

    func testAvatarDownloadsAreSharedAndScopedToSessionAndVersion() async throws {
        let data = try XCTUnwrap(UIGraphicsImageRenderer(size: CGSize(width: 4, height: 4)).image { _ in }.pngData())
        let fixture = AvatarFixture(data: data), configuration = try testConfiguration()
        func make() async -> BackendSession {
            let session = BackendSession(configuration: configuration, credentials: MemoryCredentials().store,
                api: BackendAPI(configuration: configuration, transport: { try await fixture.respond($0) }))
            await session.refresh(force: true)
            return session
        }
        let session = await make(), cache = ContactAvatarCache()
        var contact = SigmoContact(id: "photo", name: "Photo", hasAvatar: true, updatedAt: "one")
        let original = contact
        await fixture.holdNext()
        let first = Task { try await cache.image(for: original, session: session) }
        await fixture.waitForRead()
        let second = Task { try await cache.image(for: original, session: session) }
        await Task.yield()
        await fixture.release()
        _ = try await first.value
        _ = try await second.value
        _ = try await cache.image(for: contact, session: session)
        var count = await fixture.count
        XCTAssertEqual(count, 1)
        contact.updatedAt = "two"
        _ = try await cache.image(for: contact, session: session)
        let replacement = await make()
        _ = try await cache.image(for: contact, session: replacement)
        count = await fixture.count
        XCTAssertEqual(count, 3)
        await session.logout()
        do {
            _ = try await cache.image(for: contact, session: session)
            XCTFail("A logged-out session must not read a cached image")
        } catch { }
    }

    func testContactIndexKeepsRawMatchesAndAmbiguityWithoutDuplicates() {
        let first = SigmoContact(id: "first", numbers: [ContactPhone(number: "+14155552671", key: "+14155552671"), ContactPhone(number: "10086", key: "10086")])
        let second = SigmoContact(id: "second", numbers: [ContactPhone(number: "+1 415 555 2671", key: "+14155552671")])
        let index = ContactIndex([first, second])
        XCTAssertEqual(index.matches("+14155552671").map(\.id), ["first", "second"])
        XCTAssertEqual(index.matches("10086").map(\.id), ["first"])
        XCTAssertTrue(index.matches("5552671").isEmpty)
        XCTAssertTrue(ContactIndex([]).matches("10086").isEmpty)
    }
}

private actor AvatarFixture {
    let data: Data
    private let base = CommunicationFixture()
    private var hold = false
    private var held: CheckedContinuation<Void, Never>?
    private var arrived: CheckedContinuation<Void, Never>?
    private(set) var count = 0
    init(data: Data) { self.data = data }
    func holdNext() { hold = true }
    func waitForRead() async { if held == nil { await withCheckedContinuation { arrived = $0 } } }
    func release() { held?.resume(); held = nil }
    func respond(_ request: URLRequest) async throws -> BackendHTTPResponse {
        if request.url?.path.hasSuffix("/avatar") == true {
            count += 1
            if hold {
                hold = false
                await withCheckedContinuation { held = $0; arrived?.resume(); arrived = nil }
            }
            return BackendHTTPResponse(data: data, status: 200)
        }
        return try await base.respond(request)
    }
}

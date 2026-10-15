import ImageIO
import UIKit

enum AvatarImage {
    @MainActor static func load(_ data: Data, maximumSize: Int = 2048) async throws -> UIImage {
        let image = try await Task.detached(priority: .userInitiated) {
            try Task.checkCancellation()
            guard data.count <= 20 * 1024 * 1024,
                  let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
                  let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                  let width = properties[kCGImagePropertyPixelWidth] as? Int,
                  let height = properties[kCGImagePropertyPixelHeight] as? Int,
                  width > 0, height > 0, width <= 32768, height <= 32768,
                  Int64(width) * Int64(height) <= 100_000_000,
                  let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                    kCGImageSourceCreateThumbnailFromImageAlways: true,
                    kCGImageSourceCreateThumbnailWithTransform: true,
                    kCGImageSourceThumbnailMaxPixelSize: maximumSize,
                    kCGImageSourceShouldCacheImmediately: true
                  ] as CFDictionary) else { throw BackendFailure.local("communication.invalidPhoto") }
            return image
        }.value
        try Task.checkCancellation()
        return UIImage(cgImage: image)
    }
}

@MainActor
final class ContactAvatarCache {
    static let shared = ContactAvatarCache()

    struct Key: Hashable {
        let session: UUID
        let revision: Int
        let contact: String
        let version: String
    }

    private let images = NSCache<NSString, UIImage>()
    private var pending: [Key: Task<UIImage, Error>] = [:]

    init() {
        images.totalCostLimit = 16 * 1024 * 1024
        images.countLimit = 64
    }

    func image(for contact: SigmoContact, session: BackendSession) async throws -> UIImage {
        let key = Key(session: session.incarnationID, revision: session.accessRevision, contact: contact.id, version: contact.updatedAt)
        let cacheKey = "\(key.session)|\(key.revision)|\(key.contact)|\(key.version)" as NSString
        guard session.canAccessDevices else { throw CancellationError() }
        if let image = images.object(forKey: cacheKey) { return image }
        if let task = pending[key] { return try await task.value }
        let task = Task { @MainActor in
            defer { pending[key] = nil }
            let data = try await session.protectedRequest { api, token in try await api.contactAvatar(contact.id, token: token) }
            let image = try await AvatarImage.load(data, maximumSize: 512)
            guard key.revision == session.accessRevision, session.canAccessDevices else { throw CancellationError() }
            images.setObject(image, forKey: cacheKey, cost: (image.cgImage?.bytesPerRow ?? 0) * (image.cgImage?.height ?? 0))
            return image
        }
        pending[key] = task
        return try await task.value
    }
}

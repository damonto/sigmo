import CryptoKit
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct PushArtworkCache: Sendable {
    enum Source: String, Sendable { case contacts, lines }
    typealias Index = [String: Set<String>]

    static let maximumSize = 128
    private static let maximumPNGBytes = 128 * 1024
    let directory: URL

    static var appGroup: Self? {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "SigmoPushArtworkGroup") as? String,
              !group.isEmpty, !group.contains("$("),
              let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
        return Self(directory: container.appending(path: "Library/Caches/PushArtwork-v1", directoryHint: .isDirectory))
    }

    static func notificationImage(for content: PushContent, cache: Self?, bundle: Bundle = .main) -> Data? {
        guard content.kind == "sms" else { return nil }
        if let image = cache?.image(for: content) { return image }
        guard let url = bundle.url(forResource: "AppIcon", withExtension: "png"),
              let source = CGImageSourceCreateWithURL(url as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceThumbnailMaxPixelSize: maximumSize
              ] as CFDictionary) else { return nil }
        return png(image)
    }

    func image(for content: PushContent) -> Data? {
        guard content.kind == "sms" else { return nil }
        if let sender = content.sender, !sender.isEmpty {
            let contacts = index(in: .contacts, backend: content.backendId)
            var matches = contacts[Self.rawNumberKey(sender)] ?? []
            let number = PhoneNumberKey.smsSender(sender) ?? sender
            matches.formUnion(contacts[Self.numberKey(number)] ?? [])
            if matches.count == 1, let key = matches.first,
               let image = image(key, in: .contacts, backend: content.backendId) { return image }
        }
        guard let modem = content.modemId, !modem.isEmpty,
              let profile = content.profileId, !profile.isEmpty,
              let keys = index(in: .lines, backend: content.backendId)[Self.lineKey(modem: modem, profile: profile)],
              keys.count == 1, let key = keys.first else { return nil }
        return image(key, in: .lines, backend: content.backendId)
    }

    func publish(_ index: Index, in source: Source, backend: String) throws {
        let folder = folder(for: source, backend: backend)
        guard !index.isEmpty else {
            try removeFolder(folder)
            return
        }
        guard index != self.index(in: source, backend: backend) else { return }
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try JSONEncoder().encode(index).write(to: folder.appending(path: "index.json"), options: Self.writeOptions)
        let referenced = Set(index.values.flatMap { $0 })
        for file in try FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)
        where file.pathExtension == "png" && !referenced.contains(file.deletingPathExtension().lastPathComponent) {
            try FileManager.default.removeItem(at: file)
        }
    }

    @concurrent func missingImages(_ keys: [String], in source: Source, backend: String) async -> Set<String> {
        var missing: Set<String> = []
        for key in keys {
            guard !Task.isCancelled else { break }
            if image(key, in: source, backend: backend) == nil { missing.insert(key) }
        }
        return missing
    }

    func store(_ data: Data, as key: String, in source: Source, backend: String) throws {
        let url = imageURL(key, in: source, backend: backend)
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: url, options: Self.writeOptions)
    }

    func remove(backend: String) throws {
        try removeFolder(backendFolder(backend))
    }

    static func rawNumberKey(_ number: String) -> String { key("raw", number) }
    static func numberKey(_ number: String) -> String { key("normalized", number) }
    static func lineKey(modem: String, profile: String) -> String { key(modem, profile) }

    static func key(_ values: String...) -> String {
        let input = values.map { "\($0.utf8.count):\($0)" }.joined()
        return SHA256.hash(data: Data(input.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    static func png(_ image: CGImage) -> Data? {
        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, UTType.png.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(destination, image, nil)
        return CGImageDestinationFinalize(destination) ? data as Data : nil
    }

    private static let writeOptions: Data.WritingOptions = [.atomic, .completeFileProtectionUntilFirstUserAuthentication]

    private func index(in source: Source, backend: String) -> Index {
        let url = folder(for: source, backend: backend).appending(path: "index.json")
        guard let data = try? Data(contentsOf: url) else { return [:] }
        return (try? JSONDecoder().decode(Index.self, from: data)) ?? [:]
    }

    private func image(_ key: String, in source: Source, backend: String) -> Data? {
        guard key.utf8.count == 64, key.utf8.allSatisfy({ (48...57).contains($0) || (97...102).contains($0) }),
              let file = try? FileHandle(forReadingFrom: imageURL(key, in: source, backend: backend)) else { return nil }
        defer { try? file.close() }
        guard let data = try? file.read(upToCount: Self.maximumPNGBytes + 1), data.count <= Self.maximumPNGBytes,
              let imageSource = CGImageSourceCreateWithData(data as CFData, nil),
              CGImageSourceGetType(imageSource) as String? == UTType.png.identifier,
              let properties = CGImageSourceCopyPropertiesAtIndex(imageSource, 0, nil) as? [CFString: Any],
              let width = properties[kCGImagePropertyPixelWidth] as? Int, (1...Self.maximumSize).contains(width),
              let height = properties[kCGImagePropertyPixelHeight] as? Int, (1...Self.maximumSize).contains(height),
              CGImageSourceCreateImageAtIndex(imageSource, 0, nil) != nil else { return nil }
        return data
    }

    private func removeFolder(_ folder: URL) throws {
        if FileManager.default.fileExists(atPath: folder.path) { try FileManager.default.removeItem(at: folder) }
    }

    private func backendFolder(_ backend: String) -> URL {
        directory.appending(path: Self.key(backend), directoryHint: .isDirectory)
    }

    private func folder(for source: Source, backend: String) -> URL {
        backendFolder(backend).appending(path: source.rawValue, directoryHint: .isDirectory)
    }

    private func imageURL(_ key: String, in source: Source, backend: String) -> URL {
        folder(for: source, backend: backend).appending(path: key + ".png", directoryHint: .notDirectory)
    }
}

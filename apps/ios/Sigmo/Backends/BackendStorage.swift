import Foundation
import Security

struct ConfigurationFile {
    let url: URL

    static var application: ConfigurationFile {
        ConfigurationFile(url: URL.applicationSupportDirectory.appending(path: "Sigmo/backends.json"))
    }

    func load() throws -> BackendSettings {
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch let error as CocoaError where error.code == .fileReadNoSuchFile {
            return BackendSettings()
        }
        let settings = try JSONDecoder().decode(BackendSettings.self, from: data)
        guard settings.version == 1,
              Set(settings.backends.map(\.id)).count == settings.backends.count,
              settings.backends.allSatisfy({ backend in
                  (try? BackendAddress.normalize(backend.address.absoluteString)) == backend.address
              }) else { throw BackendFailure.local("storage.invalidConfiguration") }
        return settings
    }

    func save(_ settings: BackendSettings) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        try encoder.encode(settings).write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }
}

// Closures keep the Security boundary replaceable in tests without a global store.
@MainActor
struct BackendCredentials {
    var read: (UUID) throws -> String?
    var write: (UUID, String?) throws -> Void

    static let keychain = BackendCredentials(read: { id in
        var query = keychainQuery(id)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data,
              let token = String(data: data, encoding: .utf8), !token.isEmpty else {
            throw keychainError(status)
        }
        return token
    }, write: { id, token in
        let query = keychainQuery(id)
        guard let token else {
            let status = SecItemDelete(query as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw keychainError(status) }
            return
        }
        let attributes: [String: Any] = [
            kSecValueData as String: Data(token.utf8),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let status = SecItemAdd(query.merging(attributes) { _, new in new } as CFDictionary, nil)
            guard status == errSecSuccess else { throw keychainError(status) }
        } else if status != errSecSuccess {
            throw keychainError(status)
        }
    })

    private static func keychainQuery(_ id: UUID) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: "com.damon.sigmo.backend-session-v2",
         kSecAttrAccount as String: id.uuidString]
    }

    private static func keychainError(_ status: OSStatus) -> BackendFailure {
        BackendFailure(code: "keychain.\(status)", message: NSLocalizedString("storage.keychainError", comment: ""))
    }
}

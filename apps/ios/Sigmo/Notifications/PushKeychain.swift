import Foundation
import Security

enum PushKeychainError: Error { case status(OSStatus), missingAccessGroup }

// Keep Keychain failures reproducible without using a device's real credentials in tests.
@MainActor
struct PushPersistence {
    let read: (String, Bool) throws -> Data?
    let write: (String, Data?, Bool) throws -> Void

    static let keychain = PushPersistence(
        read: { try PushKeychain.read($0, shared: $1) },
        write: { try PushKeychain.write($0, data: $1, shared: $2) }
    )
}

enum PushKeychain {
    private static func query(_ account: String, shared: Bool) throws -> [String: Any] {
        var query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "com.damon.sigmo.push", kSecAttrAccount as String: account]
        if shared {
            guard let group = Bundle.main.object(forInfoDictionaryKey: "SigmoPushKeychainGroup") as? String,
                  !group.isEmpty, !group.contains("$(") else { throw PushKeychainError.missingAccessGroup }
            query[kSecAttrAccessGroup as String] = group
        }
        return query
    }

    static func read(_ account: String, shared: Bool = false) throws -> Data? {
        var query = try query(account, shared: shared)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var value: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &value)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = value as? Data else { throw PushKeychainError.status(status) }
        return data
    }

    static func write(_ account: String, data: Data?, shared: Bool = false) throws {
        let query = try query(account, shared: shared)
        guard let data else {
            let status = SecItemDelete(query as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw PushKeychainError.status(status) }
            return
        }
        let attributes: [String: Any] = [kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly]
        let update = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if update == errSecSuccess { return }
        guard update == errSecItemNotFound else { throw PushKeychainError.status(update) }
        let status = SecItemAdd(query.merging(attributes) { _, new in new } as CFDictionary, nil)
        guard status == errSecSuccess else { throw PushKeychainError.status(status) }
    }
}

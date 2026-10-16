import Foundation

struct PushPreviewSettings: Codable, Equatable, Sendable {
    static let keychainAccount = "preview-settings"

    // Preserve the backend name until the app has shared its configuration.
    var includesBackendName = true
}

struct PushPreview: Equatable, Sendable {
    let title: String
    let subtitle: String
    let body: String

    init(content: PushContent, settings: PushPreviewSettings = PushPreviewSettings()) {
        title = content.kind == "sms"
            ? content.sender ?? String(localized: "push.generic.message")
            : String(localized: "push.generic.reminder")
        subtitle = [settings.includesBackendName ? content.backendName : nil, content.modemName, content.profileName]
            .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
        body = content.text ?? ""
    }
}

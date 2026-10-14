import Foundation
import Observation

@MainActor @Observable
final class SettingsState {
    let session: BackendSession
    private(set) var snapshot: SettingsResponse?
    var auth: SettingsFields = [:]
    var notifications: SettingsFields = [:]
    var proxy: SettingsFields = [:]
    var channels: [String: SettingsFields] = [:]
    private(set) var failure: BackendFailure?
    private(set) var message: String?
    private(set) var busy = false
    // Clearing a page invalidates suspended work even when its session stays valid.
    @ObservationIgnored private var revision = 0

    init(session: BackendSession) {
        self.session = session
    }

    var channelSchemas: [NotificationSchema] {
        snapshot?.schema.channels.filter { $0.key.lowercased() != "webpush" } ?? []
    }

    var configuredChannels: [NotificationSchema] {
        channelSchemas.filter { snapshot?.values.channels?[$0.key] != nil }
    }

    subscript(section: SettingsSection) -> SettingsFields {
        get {
            switch section {
            case .authentication: auth
            case .notifications: notifications
            case .proxy: proxy
            case .notification(let channel): channels[channel] ?? [:]
            }
        }
        set {
            switch section {
            case .authentication: auth = newValue
            case .notifications: notifications = newValue
            case .proxy: proxy = newValue
            case .notification(let channel): channels[channel] = newValue
            }
            message = nil
        }
    }

    func fields(for section: SettingsSection) -> [SettingsField] {
        switch section {
        case .authentication: (snapshot?.schema.auth ?? []).filter { authFieldVisible($0.key) }
        case .notifications: [SettingsField(key: "channels", label: "settings.businessChannels", description: "settings.businessChannelsDescription", control: .channelList, required: false, secret: false, placeholder: nil, min: nil, max: nil, options: nil)]
        case .proxy: snapshot?.schema.proxy ?? []
        case .notification(let channel): channelSchemas.first { $0.key == channel }?.fields ?? []
        }
    }

    func load() async {
        guard !busy, !Task.isCancelled, snapshot == nil else { return }
        let current = revision
        busy = true
        failure = nil
        defer { if current == revision { busy = false } }
        do {
            let response = try await session.protectedRequest { api, token in
                try await api.settings(token: token)
            }
            try check(current)
            snapshot = response
            auth = response.values.auth
            notifications = response.values.notifications
            proxy = response.values.proxy
            channels = response.values.channels ?? [:]
            for channel in channelSchemas where channels[channel.key] == nil {
                channels[channel.key] = [:]
            }
            failure = nil
        } catch {
            report(error, revision: current)
        }
    }

    func clear() {
        revision += 1
        busy = false
        snapshot = nil
        auth = [:]
        notifications = [:]
        proxy = [:]
        channels = [:]
        failure = nil
        message = nil
    }

    func validation(for section: SettingsSection) -> String? {
        let values = self[section]
        if section == .authentication, values["mode"]?.text == "otp", values["otpChannels"]?.strings.isEmpty != false {
            return settingsText("settings.native.authProviderRequired")
        }
        return validation(fields: fields(for: section), values: values)
    }

    private func authFieldVisible(_ key: String) -> Bool {
        let mode = auth["mode"]?.text ?? "none"
        if key == "mode" { return true }
        if key.hasPrefix("access") { return mode == "cloudflare_access" }
        if key.hasPrefix("oidc") { return mode == "oidc" }
        return mode == "otp" && ["sessionMaxDays", "otpChannels"].contains(key)
    }

    func validation(fields: [SettingsField], values: SettingsFields, enabled: Bool = true) -> String? {
        for field in fields {
            let value = values[field.key] ?? .null
            if field.control == .number {
                if value.text.isEmpty && (!enabled || field.required != true) { continue }
                guard let number = Int(value.text), field.min.map({ number >= $0 }) ?? true,
                      field.max.map({ number <= $0 }) ?? true else {
                    return String(
                        format: settingsText("settings.native.invalidNumber"),
                        settingsText(field.label), field.min ?? 0, field.max ?? Int.max
                    )
                }
            }
            guard enabled, field.required == true else { continue }
            let empty: Bool
            switch field.control {
            case .list, .channelList:
                empty = value.strings.allSatisfy { $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
            case .toggle, .number:
                empty = false
            default:
                empty = value.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            }
            if empty {
                return String(format: settingsText("settings.native.required"), settingsText(field.label))
            }
        }
        return nil
    }

    func save(_ section: SettingsSection) async {
        guard !busy, !Task.isCancelled, snapshot != nil else { return }
        if let invalid = validation(for: section) {
            failure = BackendFailure(code: "validation", message: invalid)
            return
        }
        let payloadFields = section == .authentication ? snapshot?.schema.auth ?? [] : fields(for: section)
        let payload = Self.payload(self[section], fields: payloadFields)
        let request = payload
        let current = revision
        busy = true
        failure = nil
        message = nil
        defer { if current == revision { busy = false } }
        do {
            let saved = try await session.protectedRequest { api, token in
                try await api.saveSettings(section, fields: request, token: token)
            }
            try check(current)
            snapshot = saved
            switch section {
            case .authentication:
                auth = saved.values.auth
                await session.refreshAfterSettingsChange()
            case .notifications:
                notifications = saved.values.notifications
            case .proxy:
                proxy = saved.values.proxy
            case .notification(let channel):
                channels[channel] = saved.values.channels?[channel] ?? [:]

            }
            try check(current)
        } catch {
            report(error, revision: current)
        }
    }

    func testAuth() async {
        let providers = auth["otpChannels"]?.strings ?? []
        guard !busy, !Task.isCancelled, !providers.isEmpty else { return }
        let current = revision
        busy = true
        failure = nil
        message = nil
        defer { if current == revision { busy = false } }
        do {
            try await session.protectedRequest { api, token in
                try await api.testAuthentication(providers, token: token)
            }
            try check(current)
            message = "settings.authTestSuccess"
        } catch {
            report(error, revision: current)
        }
    }

    static func payload(_ values: SettingsFields, fields: [SettingsField]) -> SettingsFields {
        var result = values
        for field in fields {
            let value = values[field.key] ?? .null
            switch field.control {
            case .number:
                result[field.key] = Int(value.text).map(SettingValue.number)
            case .select:
                if value.text.isEmpty { result[field.key] = .text(field.options?.first?.value ?? "") }
            case .toggle:
                result[field.key] = .toggle(value.bool)
            case .list, .channelList:
                let strings = value.strings.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                result[field.key] = .list(strings.filter { !$0.isEmpty })
            default:
                break
            }
        }
        return result
    }

    private func check(_ current: Int) throws {
        try Task.checkCancellation()
        guard current == revision else { throw CancellationError() }
    }

    private func report(_ error: Error, revision current: Int) {
        guard current == revision else { return }
        if !session.canAccessDevices { clear() }
        guard !Task.isCancelled, !(error is CancellationError) else { return }
        failure = .wrapping(error)
    }
}

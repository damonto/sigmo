import Foundation
import OSLog

enum AppDiagnostics {
    static let bundleIdentifier = Bundle.main.bundleIdentifier ?? "com.damon.sigmo"
    static let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"
    static let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "unknown"

    private static let logger = Logger(
        subsystem: bundleIdentifier,
        category: "diagnostics"
    )

    static let store: DiagnosticLogStore = {
        #if DEBUG
        let environment = ProcessInfo.processInfo.environment
        if environment["SIGMO_UNIT_TESTS"] == "1" {
            // The test host must never access a developer's diagnostic logs.
            return DiagnosticLogStore(directory: .temporaryDirectory.appending(path: "SigmoDiagnostics-\(UUID())"), defaults: nil)
        }
        if environment["SIGMO_UI_SCENARIO"] != nil,
           let defaults = UserDefaults(suiteName: "com.damon.sigmo.diagnostics.uitests") {
            let reset = environment["SIGMO_UI_RESET"] == "1"
            if reset { defaults.removeObject(forKey: DiagnosticLogStore.enabledKey) }
            return DiagnosticLogStore(directory: .applicationSupportDirectory.appending(path: "UITests/Diagnostics"),
                                      defaults: defaults, reset: reset)
        }
        #endif
        return DiagnosticLogStore(directory: .applicationSupportDirectory.appending(path: "Sigmo/Diagnostics"), defaults: .standard,
                                  console: { level, line in
            // Avoid stdout, which can fail when the debug console disconnects.
            switch level {
            case .info: logger.notice("\(line, privacy: .public)")
            case .error: logger.error("\(line, privacy: .public)")
            }
        })
    }()

    static func record(_ event: String, category: String = "app", fields: [String: String] = [:]) {
        store.record(event, category: category, fields: fields)
    }

    static func record(_ error: any Error, category: String, operation: String, backendID: UUID? = nil) {
        store.record(error, category: category, operation: operation, backendID: backendID)
    }
}

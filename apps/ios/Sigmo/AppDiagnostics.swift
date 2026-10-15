import Foundation
import OSLog

enum AppDiagnostics {
    static let bundleIdentifier = Bundle.main.bundleIdentifier ?? "com.damon.sigmo"
    static let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"

    private static let logger = Logger(
        subsystem: bundleIdentifier,
        category: "diagnostics"
    )

    static func record(_ event: String) {
        // Keep diagnostics independent of stdout, which can fail when the debug console disconnects.
        logger.notice("\(event, privacy: .public)")
    }
}

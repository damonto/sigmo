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
        logger.notice("\(event, privacy: .public)")
        #if DEBUG
        // Device console capture reads stdout; OSLog remains available in Console.
        let line = "[Sigmo] \(event)\n"
        FileHandle.standardOutput.write(Data(line.utf8))
        #endif
    }
}

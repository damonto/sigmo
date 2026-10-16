import Foundation

/// Selects fixed error identifiers without descriptions, URLs or userInfo.
enum DiagnosticError {
    static func isCancellation(_ error: any Error) -> Bool {
        error is CancellationError || (error as? URLError)?.code == .cancelled
            || (error as? BackendFailure)?.transportCode == .cancelled
    }

    static func fields(_ error: any Error) -> [String: String] {
        if isCancellation(error) { return ["errorCode": "cancelled"] }
        if let failure = error as? BackendFailure {
            // Messages and HTTP problem detail can contain server data, URLs or credentials.
            let numericCode = ["network.", "keychain."].contains { marker in
                failure.code.hasPrefix(marker) && Int32(failure.code.dropFirst(marker.count)) != nil
            }
            let known = numericCode || failure.status.map({ failure.code == "http_\($0)" }) == true
                || HTTPProblem.isKnownCode(failure.code)
                || (failure.source == .local && NSLocalizedString(failure.code, comment: "") != failure.code)
                || ["invalid_response", "response_too_large", "socket_handshake", "client_error", "esim_flow", "call_failed"].contains(failure.code)
            let fallback = failure.source == .local ? "local_error" : "http_error"
            var fields = ["errorCode": known ? failure.code : fallback, "source": failure.source.rawValue]
            fields["transportCode"] = failure.transportCode.map { String($0.rawValue) }
            fields["status"] = failure.status.map(String.init)
            return fields
        }
        if let error = error as? URLError {
            return ["errorDomain": NSURLErrorDomain, "errorCode": String(error.code.rawValue)]
        }
        if error is DecodingError { return ["errorCode": "invalid_response"] }
        let error = error as NSError
        let domains = [NSCocoaErrorDomain, NSOSStatusErrorDomain, "AVFoundationErrorDomain", "com.apple.AuthenticationServices.WebAuthenticationSession"]
        return ["errorDomain": domains.contains(error.domain) ? error.domain : "other", "errorCode": String(error.code)]
    }
}

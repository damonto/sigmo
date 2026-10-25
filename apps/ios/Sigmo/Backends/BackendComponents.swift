import SwiftUI

struct FailureView: View {
    let failure: BackendFailure
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label { Text(failure.message) } icon: { Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange) }
            DisclosureGroup("error.details") {
                Text(failure.code).textSelection(.enabled)
                if let requestID = failure.requestID { Text(requestID).textSelection(.enabled) }
            }
            .font(.caption)
            .foregroundStyle(Color.secondary)
        }
        .accessibilityElement(children: .contain)
    }
}

struct BackendStatusLabel: View {
    let session: BackendSession
    private var symbol: String {
        switch session.statusKey {
        case "status.online": "checkmark.circle.fill"
        case "status.loginRequired": "lock.fill"
        case "status.offline", "status.unavailable": "exclamationmark.circle"
        default: "circle.dotted"
        }
    }
    var body: some View {
        Label {
            Text(LocalizedStringKey(session.statusKey))
        } icon: {
            Image(systemName: symbol)
                .foregroundStyle(session.isOnline ? Color.green : Color.secondary)
        }
            .font(.caption)
            .accessibilityIdentifier("backendStatus-\(session.id)")
    }
}

func modemValue(_ value: String?) -> String {
    guard let value, !value.isEmpty else { return NSLocalizedString("common.unknown", comment: "") }
    let key = "modem.value.\(value.lowercased())"
    let localized = NSLocalizedString(key, comment: "")
    return localized == key ? value : localized
}

func featureName(_ feature: String) -> String {
    let key = "capability.\(feature)"
    let localized = NSLocalizedString(key, comment: "")
    return localized == key ? feature : localized
}

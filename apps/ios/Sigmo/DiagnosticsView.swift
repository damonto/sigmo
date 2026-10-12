#if DEBUG
import SwiftUI

struct DiagnosticsView: View {
    @State private var eventCount = 0

    var body: some View {
        Form {
            Section("diagnostics.app") {
                LabeledContent("diagnostics.bundleIdentifier") {
                    Text(AppDiagnostics.bundleIdentifier)
                        .textSelection(.enabled)
                }
                .accessibilityValue(Text(AppDiagnostics.bundleIdentifier))
                .accessibilityIdentifier("bundleIdentifier")
                LabeledContent("diagnostics.version") {
                    Text(AppDiagnostics.version)
                        .textSelection(.enabled)
                }
            }
            Section {
                Button("diagnostics.writeLog") {
                    eventCount += 1
                    AppDiagnostics.record("diagnostic event \(eventCount)")
                }
                .accessibilityIdentifier("writeDiagnosticLog")

                LabeledContent("diagnostics.eventCount") {
                    Text(eventCount.formatted())
                }
                .accessibilityValue(Text(eventCount.formatted()))
                .accessibilityIdentifier("diagnosticEventCount")
            } footer: {
                Text("diagnostics.description")
            }
        }
        .navigationTitle("diagnostics.title")
    }
}
#endif

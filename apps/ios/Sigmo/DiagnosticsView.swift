import SwiftUI
import UniformTypeIdentifiers
import UIKit

struct DiagnosticLogDocument: FileDocument {
    static let readableContentTypes: [UTType] = [.utf8PlainText]
    var data: Data

    init(data: Data) { self.data = data }
    init(configuration: ReadConfiguration) throws { data = configuration.file.regularFileContents ?? Data() }
    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper { FileWrapper(regularFileWithContents: data) }

    @MainActor
    static func snapshot(from store: DiagnosticLogStore) async throws -> DiagnosticLogDocument {
        let data = try await store.snapshot()
        let header = """
        Sigmo diagnostics
        Bundle: \(AppDiagnostics.bundleIdentifier)
        Version: \(AppDiagnostics.version) (\(AppDiagnostics.build))
        iOS: \(UIDevice.current.systemVersion)
        Exported: \(Date().ISO8601Format())

        """
        return DiagnosticLogDocument(data: Data(header.utf8) + data)
    }
}

struct DiagnosticsView: View {
    private enum Action: Equatable {
        case logging(Bool)
        case clear
    }

    private let store = AppDiagnostics.store
    @State private var enabled = AppDiagnostics.store.isEnabled
    @State private var summary = DiagnosticLogSummary()
    @State private var document: DiagnosticLogDocument?
    @State private var exporting = false
    @State private var clearing = false
    @State private var action: Action?
    @State private var preparingExport = false
    @State private var failure: BackendFailure?

    private var busy: Bool { action != nil || preparingExport }

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
                Toggle("diagnostics.enabled", isOn: Binding(get: { enabled }, set: {
                    enabled = $0
                    action = .logging($0)
                }))
                    .accessibilityIdentifier("diagnosticLoggingEnabled")
                    .disabled(busy)
                AsyncActionButton(title: "diagnostics.writeLog", id: "writeDiagnosticLog") {
                    store.record("diagnostic.test", category: "app")
                    await refresh()
                }
                .disabled(!enabled || busy)

                LabeledContent("diagnostics.eventCount") {
                    Text(summary.eventCount.formatted())
                }
                .accessibilityValue(Text(summary.eventCount.formatted()))
                .accessibilityIdentifier("diagnosticEventCount")
                LabeledContent("diagnostics.size", value: ByteCountFormatter.string(fromByteCount: Int64(summary.byteCount), countStyle: .file))
                    .accessibilityIdentifier("diagnosticLogSize")
                AsyncActionButton(title: "diagnostics.export", id: "exportDiagnosticLogs") {
                    preparingExport = true
                    defer { preparingExport = false }
                    do {
                        document = try await DiagnosticLogDocument.snapshot(from: store)
                        exporting = true
                    } catch { failure = .local("diagnostics.exportFailed") }
                }
                .disabled(summary.eventCount == 0 || busy)
                Button("diagnostics.clear", role: .destructive) { clearing = true }
                    .disabled(summary.eventCount == 0 || busy)
                    .accessibilityIdentifier("clearDiagnosticLogs")
                    .alert("diagnostics.clearConfirmation", isPresented: $clearing) {
                        Button("common.cancel", role: .cancel) {}
                        Button("diagnostics.clear", role: .destructive) { action = .clear }
                            .accessibilityIdentifier("confirmClearDiagnosticLogs")
                    }
                if summary.storageFailed {
                    Label("diagnostics.storageFailed", systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.red)
                        .accessibilityIdentifier("diagnosticStorageFailure")
                }
            } footer: {
                Text("diagnostics.description")
            }
        }
        .navigationTitle("diagnostics.title")
        .detailErrorAlert($failure)
        .fileExporter(isPresented: $exporting, document: document, contentTypes: [.utf8PlainText],
                      defaultFilename: "sigmo-diagnostics-\(Date().ISO8601Format().replacingOccurrences(of: ":", with: "-")).txt") { result in
            if case .failure = result { failure = .local("diagnostics.exportFailed") }
            document = nil
        } onCancellation: { document = nil }
        .task(id: action) {
            guard let action else { return }
            switch action {
            case .logging(let enabled): await store.setEnabled(enabled)
            case .clear:
                do { try await store.clear() }
                catch { failure = .local("diagnostics.storageFailed") }
            }
            await refresh()
            if self.action == action { self.action = nil }
        }
        .task {
            for await _ in store.changes() {
                guard !Task.isCancelled else { return }
                await refresh()
            }
        }
        .onDisappear { action = nil }
    }

    private func refresh() async { summary = await store.summary() }
}

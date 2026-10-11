import AVFoundation
import SwiftUI
import VisionKit

struct ESIMScannerView: View {
    let scanned: (LPAInput) -> Void
    let enterManually: () -> Void

    private enum Availability {
        case checking
        case ready
        case unavailable
    }

    @State private var availability = Availability.checking
    @State private var failure: BackendFailure?

    var body: some View {
        GeometryReader { geometry in
            let cameraHeight = max(180, min(320, geometry.size.height * 0.47))
            ScrollView {
                VStack(spacing: 0) {
                    camera
                        .frame(minHeight: cameraHeight, maxHeight: availability == .unavailable ? nil : cameraHeight)
                        .frame(maxWidth: .infinity)
                        .background(.black)
                        .clipped()
                    VStack(alignment: .leading, spacing: 4) {
                        Text("detail.scanTitle").font(.title3.bold()).accessibilityAddTraits(.isHeader)
                        Text("detail.scanInstructions").font(.title3).foregroundStyle(.secondary)
                        Text("detail.scanManualHint").font(.title3).foregroundStyle(.secondary)
                            .padding(.top, 16)
                    }
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 38).padding(.top, 28).padding(.bottom, 24)
                }
                .padding(.top, 76)
                .frame(maxWidth: 560).frame(maxWidth: .infinity)
            }
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .safeAreaInset(edge: .bottom, spacing: 0) {
            Button(action: enterManually) {
                Text("detail.manualInput").fontWeight(.semibold).frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent).controlSize(.large).buttonBorderShape(.capsule)
            .tint(Color(uiColor: .tertiarySystemGroupedBackground)).foregroundStyle(.primary)
            .accessibilityIdentifier("manualESIM")
            .padding(.horizontal, 38).padding(.top, 16).padding(.bottom, 8)
            .frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
        .detailErrorAlert($failure)
        .task {
            guard DataScannerViewController.isSupported else {
                availability = .unavailable
                return
            }
            let permitted = await AVCaptureDevice.requestAccess(for: .video)
            guard !Task.isCancelled else { return }
            availability = permitted && DataScannerViewController.isAvailable ? .ready : .unavailable
        }
    }

    @ViewBuilder private var camera: some View {
        switch availability {
        case .checking:
            ProgressView().tint(.white)
        case .ready:
            QRScanner(scanned: scanned, failure: {
                availability = .unavailable
                failure = BackendFailure(code: "scanner", message: $0)
            })
        case .unavailable:
            ContentUnavailableView(
                "detail.cameraUnavailable",
                systemImage: "camera",
                description: Text("detail.cameraFallback")
            )
            .fixedSize(horizontal: false, vertical: true)
            .environment(\.colorScheme, .dark)
        }
    }
}

private struct QRScanner: UIViewControllerRepresentable {
    let scanned: (LPAInput) -> Void
    let failure: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            recognizesMultipleItems: false
        )
        scanner.delegate = context.coordinator
        return scanner
    }

    func updateUIViewController(_ scanner: DataScannerViewController, context: Context) {
        context.coordinator.parent = self
        context.coordinator.start(scanner)
    }

    static func dismantleUIViewController(_ scanner: DataScannerViewController, coordinator: Coordinator) {
        scanner.delegate = nil
        coordinator.stop(scanner)
    }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        var parent: QRScanner
        private var finished = false
        private var startTask: Task<Void, Never>?

        init(_ parent: QRScanner) { self.parent = parent }

        func start(_ scanner: DataScannerViewController) {
            guard !finished, !scanner.isScanning, startTask == nil else { return }
            // Scanner failures update SwiftUI state, so start after its update pass.
            startTask = Task { [weak self, weak scanner] in
                guard let self, let scanner, !self.finished, !Task.isCancelled else { return }
                defer { self.startTask = nil }
                do { try scanner.startScanning() }
                catch { self.fail(scanner, message: error.localizedDescription) }
            }
        }

        func stop(_ scanner: DataScannerViewController) {
            finished = true
            startTask?.cancel()
            startTask = nil
            scanner.stopScanning()
        }

        private func fail(_ scanner: DataScannerViewController, message: String) {
            guard !finished else { return }
            stop(scanner)
            parent.failure(message)
        }

        func dataScanner(
            _ scanner: DataScannerViewController,
            didAdd addedItems: [RecognizedItem],
            allItems: [RecognizedItem]
        ) {
            guard !finished else { return }
            for item in addedItems {
                guard case .barcode(let barcode) = item, let raw = barcode.payloadStringValue else { continue }
                guard let input = LPAInput.parse(raw) else {
                    fail(scanner, message: detailText("detail.scanInvalid"))
                    return
                }
                // VisionKit can deliver another recognition before SwiftUI removes this controller.
                stop(scanner)
                parent.scanned(input)
                return
            }
        }

        func dataScanner(
            _ scanner: DataScannerViewController,
            becameUnavailableWithError error: DataScannerViewController.ScanningUnavailable
        ) {
            fail(scanner, message: error.localizedDescription)
        }
    }
}

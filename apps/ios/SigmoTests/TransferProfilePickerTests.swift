import SwiftUI
import XCTest
@testable import Sigmo

@MainActor
final class TransferProfilePickerTests: XCTestCase {
    func testLoadingAndLoadedListsHaveTheSameHeight() {
        let profiles = ["Travel", "Work", "Personal"].enumerated().map { index, name in
            TransferProfile(id: "source-\(index)", seId: "se-a", type: "esim", name: name,
                            serviceProviderName: nil, iccid: "898600000000000000\(index)",
                            icon: nil, regionCode: "CN", enabled: false, supported: true,
                            unsupportedReason: nil, carrierName: nil)
        }
        let loading = TransferProfilePicker(profiles: [], selectedID: nil, isLoading: true, showsEmptyState: true) { _ in }
        let loaded = TransferProfilePicker(profiles: profiles, selectedID: nil, isLoading: false, showsEmptyState: true) { _ in }

        for textSize in [DynamicTypeSize.large, .accessibility2] {
            let loadingHeight = height(of: loading, textSize: textSize)
            let loadedHeight = height(of: loaded, textSize: textSize)
            XCTAssertGreaterThan(loadingHeight, 0)
            XCTAssertEqual(loadingHeight, loadedHeight, accuracy: 0.5, "\(textSize)")
        }
    }

    private func height(of picker: TransferProfilePicker, textSize: DynamicTypeSize) -> CGFloat {
        let host = UIHostingController(rootView: picker.dynamicTypeSize(textSize))
        return host.sizeThatFits(in: CGSize(width: 300, height: 1000)).height
    }
}

import UIKit
import XCTest
@testable import Sigmo

@MainActor
final class BackendFilterViewTests: XCTestCase {
    func testSelectionSurvivesUpdatesAndConnectionRemoval() {
        let firstID = UUID(), secondID = UUID()
        let items = [BackendFilterItem(id: nil, title: "All"),
            BackendFilterItem(id: firstID, title: "Home"),
            BackendFilterItem(id: secondID, title: "Studio")]
        let filter = BackendFilterView()
        filter.frame = CGRect(x: 0, y: 0, width: 370, height: 32)
        filter.update(items: items, selection: nil)
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.segments.selectedSegmentIndex, 0)
        var selected: UUID?
        filter.onSelect = { selected = $0 }
        filter.segments.selectedSegmentIndex = 2
        filter.segments.sendActions(for: .valueChanged)
        XCTAssertEqual(selected, secondID)

        filter.update(items: items, selection: secondID)
        filter.setNeedsLayout()
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.segments.selectedSegmentIndex, 2)
        filter.update(items: Array(items.prefix(2)), selection: nil)
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.segments.numberOfSegments, 2)
        XCTAssertEqual(filter.segments.selectedSegmentIndex, 0)
    }

    func testCompactWidthFillsAndLongNamesCanScroll() {
        let cases: [(name: String, title: String, overflows: Bool)] = [
            ("short", "Home", false),
            ("long", "Travel Workbench With A Longer Connection Name", true),
        ]
        for item in cases {
            let filter = BackendFilterView()
            filter.frame = CGRect(x: 0, y: 0, width: 370, height: 32)
            filter.update(items: [.init(id: nil, title: "All"), .init(id: UUID(), title: item.title)], selection: nil)
            filter.layoutIfNeeded()
            XCTAssertEqual(filter.contentInsetAdjustmentBehavior, .never)
            XCTAssertEqual(filter.segments.frame.height, 32, item.name)
            if item.overflows {
                XCTAssertGreaterThan(filter.contentSize.width, filter.bounds.width, item.name)
            } else {
                XCTAssertEqual(filter.contentSize.width, filter.bounds.width, item.name)
            }
        }
    }

    func testRestoredSelectionAndProgrammaticChangesScrollIntoView() {
        let selectedID = UUID()
        var items = [BackendFilterItem(id: nil, title: "All")]
            + (1...6).map { BackendFilterItem(id: UUID(), title: "Connection \($0)") }
            + [BackendFilterItem(id: selectedID, title: "Selected")]
        let filter = BackendFilterView()
        // SwiftUI can supply the selection before the view receives its size.
        filter.update(items: items, selection: selectedID)
        filter.layoutIfNeeded()
        filter.frame = CGRect(x: 0, y: 0, width: 320, height: 32)
        filter.layoutIfNeeded()
        XCTAssertGreaterThan(filter.contentOffset.x, 0)
        XCTAssertEqual(filter.bounds.maxX, filter.contentSize.width, accuracy: 1)

        // A binding echo or unrelated layout must not undo manual scrolling.
        filter.setContentOffset(.zero, animated: false)
        filter.update(items: items, selection: selectedID)
        filter.setNeedsLayout()
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.contentOffset.x, 0)

        filter.frame.size.width = 240
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.bounds.maxX, filter.contentSize.width, accuracy: 1)

        items[1] = BackendFilterItem(id: items[1].id, title: "A much longer renamed connection")
        filter.update(items: items, selection: selectedID)
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.bounds.maxX, filter.contentSize.width, accuracy: 1)

        filter.update(items: items, selection: nil)
        filter.layoutIfNeeded()
        XCTAssertEqual(filter.contentOffset.x, 0, accuracy: 1)
        XCTAssertEqual(filter.segments.selectedSegmentIndex, 0)
    }

    func testFontUsesTheViewsContentSizeCategory() throws {
        let filter = BackendFilterView()
        filter.frame = CGRect(x: 0, y: 0, width: 320, height: 96)
        filter.update(items: [.init(id: nil, title: "All"), .init(id: UUID(), title: "Home")], selection: nil)
        var previousSize: CGFloat = 0
        for category in [UIContentSizeCategory.large, .accessibilityExtraExtraExtraLarge] {
            filter.traitOverrides.preferredContentSizeCategory = category
            filter.updateTraitsIfNeeded()
            filter.setNeedsLayout()
            filter.layoutIfNeeded()
            let font = try XCTUnwrap(filter.segments.titleTextAttributes(for: .normal)?[.font] as? UIFont)
            let expected = UIFont.preferredFont(forTextStyle: .subheadline, compatibleWith: filter.traitCollection)
            XCTAssertEqual(font.pointSize, expected.pointSize)
            XCTAssertGreaterThan(font.pointSize, previousSize)
            previousSize = font.pointSize
        }
    }
}

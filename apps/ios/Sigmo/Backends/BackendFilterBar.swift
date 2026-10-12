import SwiftUI
import UIKit

struct BackendFilterBar: View {
    @Environment(BackendStore.self) private var store
    @Binding var selection: UUID?
    @ScaledMetric(relativeTo: .subheadline) private var height: CGFloat = 32

    var body: some View {
        BackendFilterControl(items: [.init(id: nil, title: String(localized: "home.all"))]
            + store.onlineSessions.map { .init(id: $0.id, title: $0.configuration.name) }, selection: $selection)
            .frame(height: height)
    }
}

struct BackendFilterItem: Equatable {
    let id: UUID?
    let title: String
}

private struct BackendFilterControl: UIViewRepresentable {
    let items: [BackendFilterItem]
    @Binding var selection: UUID?

    func makeUIView(context: Context) -> BackendFilterView { BackendFilterView() }

    func updateUIView(_ view: BackendFilterView, context: Context) {
        view.onSelect = { selection = $0 }
        view.update(items: items, selection: selection)
    }
}

final class BackendFilterView: UIScrollView {
    let segments: UISegmentedControl = BackendFilterSegments()
    private var items: [BackendFilterItem] = []
    private var titleFont: UIFont?
    private var layoutSize = CGSize.zero
    private var needsSelectionScroll = true
    var onSelect: ((UUID?) -> Void)?

    init() {
        super.init(frame: .zero)
        accessibilityIdentifier = "backendFilters"
        contentInsetAdjustmentBehavior = .never
        showsHorizontalScrollIndicator = false
        addSubview(segments)
        segments.accessibilityIdentifier = "backendFilterSegments"
        segments.addTarget(self, action: #selector(selectSegment), for: .valueChanged)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func touchesShouldCancel(in view: UIView) -> Bool {
        // UIScrollView normally lets UIControl keep tracking a touch.
        contentSize.width > bounds.width || super.touchesShouldCancel(in: view)
    }

    func update(items: [BackendFilterItem], selection: UUID?) {
        if self.items != items {
            self.items = items
            segments.removeAllSegments()
            for (index, item) in items.enumerated() {
                segments.insertSegment(withTitle: item.title, at: index, animated: false)
            }
            needsSelectionScroll = true
        }
        let index = items.firstIndex(where: { $0.id == selection }) ?? UISegmentedControl.noSegment
        // Echoing SwiftUI's binding must not restart UIKit's touch transition.
        if segments.selectedSegmentIndex != index {
            segments.selectedSegmentIndex = index
            needsSelectionScroll = true
        }
        if needsSelectionScroll { setNeedsLayout() }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard bounds.width > 0, bounds.height > 0 else { return }
        let resized = layoutSize != bounds.size
        layoutSize = bounds.size
        let preferred = UIFont.preferredFont(forTextStyle: .subheadline, compatibleWith: traitCollection)
        let font = UIFont.systemFont(ofSize: preferred.pointSize, weight: .semibold)
        let fontChanged = titleFont != font
        if fontChanged {
            titleFont = font
            segments.setTitleTextAttributes([.font: font, .foregroundColor: UIColor.label], for: .normal)
            segments.setTitleTextAttributes([.font: font, .foregroundColor: UIColor.label], for: .selected)
        }
        let widths = items.map { max(60, ($0.title as NSString).size(withAttributes: [.font: font]).width + 32) }
        let total = widths.reduce(0, +)
        let extra = items.isEmpty ? 0 : max(0, bounds.width - total) / CGFloat(items.count)
        // Explicit widths also let us reveal a selection without inspecting
        // UISegmentedControl's private view hierarchy.
        for (index, width) in widths.enumerated() where segments.widthForSegment(at: index) != width + extra {
            segments.setWidth(width + extra, forSegmentAt: index)
        }
        let frame = CGRect(x: 0, y: 0, width: max(bounds.width, total), height: bounds.height)
        if segments.frame != frame { segments.frame = frame }
        if contentSize != frame.size { contentSize = frame.size }
        if needsSelectionScroll || resized || fontChanged {
            scrollToSelection(animated: false)
            needsSelectionScroll = false
        }
    }

    @objc private func selectSegment() {
        let index = segments.selectedSegmentIndex
        guard items.indices.contains(index) else { return }
        scrollToSelection(animated: !UIAccessibility.isReduceMotionEnabled)
        onSelect?(items[index].id)
    }

    private func scrollToSelection(animated: Bool) {
        let index = segments.selectedSegmentIndex
        guard items.indices.contains(index) else { return }
        var x: CGFloat = 0
        for preceding in 0..<index { x += segments.widthForSegment(at: preceding) }
        let rect = CGRect(x: x, y: 0, width: segments.widthForSegment(at: index), height: bounds.height)
        scrollRectToVisible(rect, animated: animated)
    }
}

private final class BackendFilterSegments: UISegmentedControl {
    override func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        // UISegmentedControl vetoes ancestor pans. Allow our scroll view to
        // reveal offscreen choices while retaining the control's own gestures.
        if let scroll = superview as? UIScrollView, gestureRecognizer === scroll.panGestureRecognizer {
            return scroll.contentSize.width > scroll.bounds.width
        }
        return super.gestureRecognizerShouldBegin(gestureRecognizer)
    }
}

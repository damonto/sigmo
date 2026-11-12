import SwiftUI

enum ButtonProgressPlacement {
    case trailing
    case replacingLabel
}

// The caller owns the operation; this view keeps its button feedback consistent.
struct LoadingButton<Label: View>: View {
    var role: ButtonRole?
    let isWorking: Bool
    var progressValue = Text("common.working")
    var progressPlacement: ButtonProgressPlacement = .trailing
    var contentMaxWidth: CGFloat?
    var contentAlignment: Alignment = .center
    let action: @MainActor () -> Void
    @ViewBuilder var label: () -> Label

    var body: some View {
        Button(role: role, action: action) {
            Group {
                switch progressPlacement {
                case .trailing:
                    HStack(spacing: 8) {
                        label()
                        if isWorking { progress }
                    }
                case .replacingLabel:
                    label()
                        .opacity(isWorking ? 0 : 1)
                        .overlay {
                            if isWorking { progress }
                        }
                }
            }
            .frame(maxWidth: contentMaxWidth, alignment: contentAlignment)
        }
        .disabled(isWorking)
        .accessibilityLabel { _ in label() }
        .accessibilityValue(progressValue, isEnabled: isWorking)
    }

    private var progress: some View {
        ProgressView().controlSize(.small).accessibilityHidden(true)
    }
}

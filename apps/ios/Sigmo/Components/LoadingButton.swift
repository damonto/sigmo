import SwiftUI

// The caller owns the operation; this view keeps its button feedback consistent.
struct LoadingButton<Label: View>: View {
    var role: ButtonRole?
    let isWorking: Bool
    var progressValue = Text("common.working")
    let action: @MainActor () -> Void
    @ViewBuilder var label: () -> Label

    var body: some View {
        Button(role: role, action: action) {
            label()
                .opacity(isWorking ? 0 : 1)
                .overlay {
                    if isWorking { ProgressView().controlSize(.small).accessibilityHidden(true) }
                }
        }
        .disabled(isWorking)
        .accessibilityLabel { _ in label() }
        .accessibilityValue(progressValue, isEnabled: isWorking)
    }
}

import SwiftUI

// Inline recovery actions share a size and label; alerts keep native alert buttons.
struct RetryButton: View {
    var id = ""
    let action: @MainActor () async -> Void

    var body: some View {
        AsyncActionButton(id: id, minimumLabelHeight: nil, action: action) {
            HStack(spacing: 8) {
                Image(systemName: "arrow.clockwise").accessibilityHidden(true)
                Text("common.retry").fixedSize(horizontal: false, vertical: true)
            }
        }
        .font(.body)
        .buttonStyle(.bordered)
        .controlSize(.large)
    }
}

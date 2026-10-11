import SwiftUI

// The view owns each action; leaving it or entering the background cancels the work.
struct AsyncActionButton<Label: View>: View {
    var role: ButtonRole?
    var id = ""
    // Let Form rows and button styles provide their own padding and minimum size.
    var minimumLabelHeight: CGFloat? = nil
    let action: @MainActor () async -> Void
    @ViewBuilder var label: () -> Label
    @Environment(\.scenePhase) private var scenePhase
    @State private var requestID: UUID?

    var body: some View {
        Button(role: role) { requestID = UUID() } label: {
            HStack {
                label()
                if requestID != nil {
                    Spacer()
                    ProgressView()
                }
            }
            .frame(minHeight: minimumLabelHeight)
        }
        .disabled(requestID != nil)
        .accessibilityIdentifier(id)
        .task(id: requestID) {
            guard let requestID, !Task.isCancelled else { return }
            await action()
            // A cancelled action may finish after the user has started another one.
            if self.requestID == requestID { self.requestID = nil }
        }
        .onDisappear { requestID = nil }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { requestID = nil }
        }
    }
}

extension AsyncActionButton where Label == Text {
    init(title: String, role: ButtonRole? = nil, id: String = "", minimumLabelHeight: CGFloat? = nil, action: @escaping @MainActor () async -> Void) {
        self.role = role
        self.id = id
        self.minimumLabelHeight = minimumLabelHeight
        self.action = action
        label = { Text(LocalizedStringKey(title)) }
    }
}

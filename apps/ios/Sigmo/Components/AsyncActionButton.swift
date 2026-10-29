import SwiftUI

// The view owns each action; backgrounding keeps it alive, leaving cancels it.
struct AsyncActionButton<Label: View>: View {
    var role: ButtonRole?
    var id = ""
    var isWorking = false
    let action: @MainActor () async -> Void
    @ViewBuilder var label: () -> Label
    @State private var requestID: UUID?

    var body: some View {
        LoadingButton(role: role, isWorking: requestID != nil || isWorking) {
            guard requestID == nil else { return }
            requestID = UUID()
        } label: {
            label()
        }
        .accessibilityIdentifier(id)
        .task(id: requestID) {
            guard let requestID, !Task.isCancelled else { return }
            await action()
            // A cancelled action may finish after the user has started another one.
            if self.requestID == requestID { self.requestID = nil }
        }
        .onDisappear { requestID = nil }
    }
}

extension AsyncActionButton where Label == Text {
    init(
        title: String,
        role: ButtonRole? = nil,
        id: String = "",
        isWorking: Bool = false,
        action: @escaping @MainActor () async -> Void
    ) {
        self.role = role
        self.id = id
        self.isWorking = isWorking
        self.action = action
        label = { Text(LocalizedStringKey(title)) }
    }
}

// Toggles keep their title and on/off value visible during a write.
struct InlineProgressLabel<Label: View>: View {
    let isWorking: Bool
    @ViewBuilder var label: () -> Label

    var body: some View {
        HStack(spacing: 8) {
            label()
            if isWorking {
                ProgressView().controlSize(.small).accessibilityLabel(Text("common.working"))
            }
        }
    }
}

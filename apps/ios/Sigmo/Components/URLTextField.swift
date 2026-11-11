import SwiftUI

struct URLTextField: View {
    let title: LocalizedStringKey
    @Binding var text: String
    var id = ""
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @FocusState private var focused: Bool

    private var layout: AnyLayout {
        if dynamicTypeSize.isAccessibilitySize {
            AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
        } else {
            AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 8))
        }
    }

    var body: some View {
        layout {
            Text(verbatim: "http(s)://")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize()
                .accessibilityHidden(true)
            TextField(title, text: $text, prompt: Text(verbatim: "sigmo.example.com"))
                .textFieldStyle(.plain)
                .keyboardType(.URL).textContentType(.URL)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .focused($focused)
                .accessibilityIdentifier(id)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
    }
}

import SwiftUI

struct URLTextField: View {
    let title: LocalizedStringKey
    @Binding var text: String
    var id = ""

    var body: some View {
        TextField(title, text: $text, prompt: Text(verbatim: "https://sigmo.example.com"))
            .textFieldStyle(.plain)
            .keyboardType(.URL).textContentType(.URL)
            .textInputAutocapitalization(.never).autocorrectionDisabled()
            .accessibilityIdentifier(id)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

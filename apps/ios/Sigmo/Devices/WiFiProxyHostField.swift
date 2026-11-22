import SwiftUI
import UIKit

struct WiFiProxyHostField: UIViewRepresentable {
    @Binding var text: String
    let onPaste: @MainActor (WiFiProxyPaste) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(field: self) }

    func makeUIView(context: Context) -> UITextField {
        let input = UITextField()
        input.font = .preferredFont(forTextStyle: .body)
        input.adjustsFontForContentSizeCategory = true
        input.autocapitalizationType = .none
        input.autocorrectionType = .no
        input.spellCheckingType = .no
        input.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        input.accessibilityIdentifier = "wifiProxyHost"
        input.pasteDelegate = context.coordinator
        input.addTarget(context.coordinator, action: #selector(Coordinator.textChanged(_:)), for: .editingChanged)
        return input
    }

    func updateUIView(_ input: UITextField, context: Context) {
        context.coordinator.field = self
        input.isEnabled = context.environment.isEnabled
        input.textAlignment = context.environment.layoutDirection == .leftToRight ? .right : .left
        input.accessibilityLabel = String(localized: "connection.wifiProxy.host", locale: context.environment.locale)
        input.placeholder = String(localized: "connection.wifiProxy.hostPlaceholder", locale: context.environment.locale)
        if input.text != text { input.text = text }
    }

    @MainActor
    final class Coordinator: NSObject, UITextPasteDelegate {
        var field: WiFiProxyHostField

        init(field: WiFiProxyHostField) { self.field = field }

        @objc func textChanged(_ input: UITextField) {
            let value = input.text ?? ""
            if field.text != value { field.text = value }
        }

        func textPasteConfigurationSupporting(_ supporting: any UITextPasteConfigurationSupporting,
                                              performPasteOf attributedString: NSAttributedString,
                                              to range: UITextRange) -> UITextRange {
            guard let input = supporting as? UITextField, input.isEnabled else { return range }

            let paste = WiFiProxyPaste(attributedString.string)
            let target: UITextRange
            let replacement: String
            switch paste {
            case .text:
                target = range
                replacement = attributedString.string
            case .invalid:
                field.onPaste(paste)
                return input.textRange(from: range.start, to: range.start) ?? range
            case .address(let address):
                // A complete proxy address replaces the host, even when the caret is inside a draft.
                target = input.textRange(from: input.beginningOfDocument, to: input.endOfDocument) ?? range
                replacement = address.host
            }

            let leading = input.offset(from: input.beginningOfDocument, to: target.start)
            let trailing = input.offset(from: target.end, to: input.endOfDocument)
            input.replace(target, withText: replacement)
            textChanged(input)
            field.onPaste(paste)
            guard let start = input.position(from: input.beginningOfDocument, offset: leading),
                  let end = input.position(from: input.endOfDocument, offset: -trailing),
                  let inserted = input.textRange(from: start, to: end) else { return range }
            input.selectedTextRange = input.textRange(from: end, to: end)
            return inserted
        }
    }
}

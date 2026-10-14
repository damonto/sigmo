import SwiftUI

// The surrounding page supplies the line and owns dialing/USSD. This surface is shared by both entry points.
struct CallDialpadSurface: View {
    @Binding var number: String
    let canDial: Bool
    let dial: () -> Void
    @FocusState private var editing: Bool

    var body: some View {
        VStack(spacing: 22) {
            TextField("", text: $number)
                .font(.system(size: 36)).multilineTextAlignment(.center).keyboardType(.phonePad)
                .textContentType(.telephoneNumber).focused($editing)
                .frame(minHeight: 96)
                .accessibilityLabel("calls.number").accessibilityIdentifier("calls-number")
            CallKeypad(allowsPlus: true) { digit in
                editing = false
                if number.count < 128 { number += digit }
            }
            HStack(spacing: 24) {
                Color.clear.frame(width: 80, height: 80).accessibilityHidden(true)
                Button {
                    editing = false
                    dial()
                } label: {
                    Image(systemName: "phone.fill").font(.system(size: 30))
                        .foregroundStyle(.white).frame(width: 80, height: 80)
                        .background(.green, in: Circle())
                }
                .buttonStyle(.plain).disabled(!canDial).opacity(canDial ? 1 : 0.45)
                .accessibilityLabel(CallDigits.isService(number) ? LocalizedStringKey("calls.sendCode") : LocalizedStringKey("calls.dial"))
                .accessibilityIdentifier("calls-dial")
                Button {
                    if !number.isEmpty { number.removeLast() }
                } label: {
                    Image(systemName: "delete.left.fill").font(.title2).frame(width: 80, height: 80)
                }
                .buttonStyle(.plain).disabled(number.isEmpty).opacity(number.isEmpty ? 0 : 1)
                .accessibilityLabel("calls.deleteDigit").accessibilityIdentifier("calls-delete-digit")
            }
        }
        .frame(maxWidth: 320).frame(maxWidth: .infinity)
    }
}

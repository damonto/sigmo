import Contacts
import SwiftUI

struct CallDialpadRequest: Identifiable, Equatable {
    let id = UUID()
    var number = ""
}

struct CallDialpad: View {
    let line: CallLine
    let session: BackendSession
    let dial: (String) -> Void
    @Environment(CallsState.self) private var calls
    @Environment(\.dismiss) private var dismiss
    @State private var number = ""
    @State private var contactsPresented = false
    @State private var accessPresented = false
    @State private var ussd: CallUSSD

    init(line: CallLine, session: BackendSession, initialNumber: String = "", dial: @escaping (String) -> Void) {
        self.line = line
        self.session = session
        self.dial = dial
        _number = State(initialValue: CallDigits.normalize(initialNumber))
        _ussd = State(initialValue: CallUSSD(session: session, line: line))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 20) {
                    HStack(spacing: 8) {
                        Image(systemName: "simcard.fill").foregroundStyle(.blue)
                        Text(line.label).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        Spacer()
                    }
                    if ussd.opened { CallServiceView(ussd: ussd) }
                    else {
                        CallDialpadSurface(number: $number, canDial: canDial, dial: dialNumber)
                        if !calls.available(line) { Text("calls.unavailable").font(.footnote).foregroundStyle(.secondary) }
                        contactAccess
                    }
                }.padding(24).frame(maxWidth: 408).frame(maxWidth: .infinity)
            }
            .background(Color(uiColor: .systemBackground))
            .navigationTitle(ussd.opened ? LocalizedStringKey("calls.ussd") : LocalizedStringKey("calls.keypad"))
            .navigationBarTitleDisplayMode(.inline)
            .detailErrorAlert(ussd.opened ? nil : ussd.failure)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.done", systemImage: "xmark") { ussd.close(); dismiss() }.labelStyle(.iconOnly) }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("calls.contacts", systemImage: "person.crop.circle") { contactsPresented = true }
                        .accessibilityIdentifier("calls-contacts")
                }
            }
            .sheet(isPresented: $contactsPresented) {
                CallContactPicker { selected, name in number = CallDigits.normalize(selected); calls.contacts.remember(number: selected, name: name) }
            }
            .contactAccessPicker(isPresented: $accessPresented) { _ in Task { await calls.reloadContacts() } }
            .onChange(of: session.accessRevision) { _, _ in ussd.close(); dismiss() }
            .onDisappear { ussd.close() }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }

    private var canDial: Bool {
        CallDigits.callable(number) && (CallDigits.isService(number) || (!calls.hasCall && calls.available(line)))
    }

    private func dialNumber() {
        if CallDigits.isService(number) {
            ussd.send(number, initial: true)
            return
        }
        dial(CallDigits.normalize(number))
        dismiss()
    }

    @ViewBuilder private var contactAccess: some View {
        switch calls.contacts.authorization {
        case .notDetermined:
            Button("calls.matchContacts") { Task { await calls.contacts.authorize() } }.font(.footnote)
        case .limited:
            Button("calls.manageContacts") { accessPresented = true }.font(.footnote)
        case .denied, .restricted:
            VStack(spacing: 8) {
                Text("calls.contactsDenied").font(.footnote).foregroundStyle(.secondary)
                if let url = URL(string: UIApplication.openSettingsURLString) { Link("common.settings", destination: url).font(.footnote) }
            }
        default: EmptyView()
        }
    }

}

struct CallServiceView: View {
    @Bindable var ussd: CallUSSD

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            ForEach(Array(ussd.messages.enumerated()), id: \.offset) { _, message in Text(message).textSelection(.enabled) }
            if ussd.busy { ProgressView("calls.ussdWaiting") }
            if !ussd.ended {
                TextField("calls.ussdReply", text: $ussd.reply).textFieldStyle(.roundedBorder).keyboardType(.numbersAndPunctuation)
                Button("calls.sendReply") { ussd.send(ussd.reply) }
                    .buttonStyle(.glassProminent).disabled(ussd.busy || ussd.reply.isEmpty)
            }
            Text("calls.ussdHint").font(.footnote).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .detailErrorAlert($ussd.failure)
    }
}

struct CallKeypad: View {
    var allowsPlus = false
    var compact = false
    let pressed: (String) -> Void
    @ScaledMetric(relativeTo: .largeTitle) private var digitSize: CGFloat = 34
    @ScaledMetric(relativeTo: .caption2) private var lettersSize: CGFloat = 11

    private struct Key: Identifiable {
        let digit: String
        let letters: String
        var id: String { digit }
    }

    private static let rows: [[Key]] = [
        [.init(digit: "1", letters: ""), .init(digit: "2", letters: "ABC"), .init(digit: "3", letters: "DEF")],
        [.init(digit: "4", letters: "GHI"), .init(digit: "5", letters: "JKL"), .init(digit: "6", letters: "MNO")],
        [.init(digit: "7", letters: "PQRS"), .init(digit: "8", letters: "TUV"), .init(digit: "9", letters: "WXYZ")],
        [.init(digit: "*", letters: ""), .init(digit: "0", letters: ""), .init(digit: "#", letters: "")]
    ]

    var body: some View {
        Group {
            if allowsPlus { keys.buttonStyle(.plain) }
            else { GlassEffectContainer(spacing: 12) { keys.buttonStyle(.glass).buttonBorderShape(.circle).tint(.primary) } }
        }.frame(maxWidth: 360).frame(maxWidth: .infinity)
    }

    private var keys: some View {
            Grid(horizontalSpacing: allowsPlus ? 24 : 20, verticalSpacing: compact ? 8 : 16) {
                ForEach(Self.rows.indices, id: \.self) { row in
                    GridRow {
                        ForEach(Self.rows[row]) { key in
                            if key.digit == "0", allowsPlus {
                                keyButton(key)
                                    .highPriorityGesture(LongPressGesture(minimumDuration: 0.5).onEnded { _ in pressed("+") })
                                    .accessibilityAction(named: Text("calls.plus")) { pressed("+") }
                            } else { keyButton(key) }
                        }
                    }
                }
            }
    }

    private func keyButton(_ key: Key) -> some View {
        let letters = key.digit == "0" && allowsPlus ? "+" : key.letters
        return Button { pressed(key.digit) } label: {
            VStack(spacing: 0) {
                // Bound the glyph sizes so the telephone grid stays usable at accessibility sizes.
                Text(key.digit).font(.system(size: allowsPlus ? 40 : min(digitSize, compact ? 24 : 44), design: allowsPlus ? .default : .rounded)).lineLimit(1).minimumScaleFactor(0.5)
                Text(letters.isEmpty ? " " : letters).font(.system(size: min(lettersSize, compact ? 10 : 16), weight: .semibold)).tracking(1.5)
                    .lineLimit(1).minimumScaleFactor(0.5)
                    .frame(height: compact ? 12 : 20)
            }
            .frame(width: allowsPlus ? 80 : nil, height: allowsPlus ? 80 : nil)
            .frame(maxWidth: allowsPlus ? nil : .infinity, minHeight: compact ? 36 : 64)
            .background { if allowsPlus { Color(uiColor: .systemGray5).clipShape(Circle()) } }
        }
        .accessibilityLabel(key.digit).accessibilityValue(letters).accessibilityIdentifier("call-key-\(key.digit)")
    }
}

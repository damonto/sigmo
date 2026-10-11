import Contacts
import SwiftUI

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
    @FocusState private var editing: Bool

    init(line: CallLine, session: BackendSession, dial: @escaping (String) -> Void) {
        self.line = line
        self.session = session
        self.dial = dial
        _ussd = State(initialValue: CallUSSD(session: session, line: line))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 20) {
                    Text("\(line.backendName) · \(line.label)").font(.subheadline).foregroundStyle(.secondary)
                    if ussd.opened { service }
                    else {
                        numberEntry
                        editingControls
                        CallKeypad(allowsPlus: true) { digit in
                            editing = false
                            if number.count < 128 { number += digit }
                        }
                        dialButton
                        if !calls.available(line) { Text("calls.upgrade").font(.footnote).foregroundStyle(.secondary) }
                        contactAccess
                    }
                }.padding(24).frame(maxWidth: 408).frame(maxWidth: .infinity)
            }
            .background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle(ussd.opened ? "calls.ussd" : "calls.keypad")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.done") { ussd.close(); dismiss() } } }
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

    private var numberEntry: some View {
        VStack(spacing: 8) {
            TextField("calls.number", text: $number)
                .font(.largeTitle).multilineTextAlignment(.center).keyboardType(.phonePad)
                .textContentType(.telephoneNumber).focused($editing)
                .accessibilityIdentifier("calls-number")
            Text(PhoneNumberDisplay.format(number, regionCode: line.region))
                .font(.callout).foregroundStyle(.secondary).accessibilityHidden(true)
        }.frame(minHeight: 84)
    }

    private var canDial: Bool {
        CallDigits.callable(number) && (CallDigits.isService(number) || (!calls.hasCall && calls.available(line)))
    }

    private var dialButton: some View {
        Button(action: dialNumber) {
            Label(CallDigits.isService(number) ? "calls.sendCode" : "calls.dial", systemImage: "phone.fill")
                .labelStyle(.iconOnly).font(.title2.weight(.semibold))
                .frame(width: 56, height: 56)
        }
        .buttonStyle(.glassProminent).buttonBorderShape(.circle).tint(.green)
        .disabled(!canDial)
        .accessibilityIdentifier("calls-dial")
    }

    private func dialNumber() {
        editing = false
        if CallDigits.isService(number) {
            ussd.send(number, initial: true)
            return
        }
        dial(CallDigits.normalize(number))
        dismiss()
    }

    private var editingControls: some View {
        GlassEffectContainer(spacing: 12) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    pasteButton
                    Spacer(minLength: 0)
                    contactsButton.fixedSize()
                    Spacer(minLength: 0)
                    deleteButton
                }
                VStack(spacing: 12) {
                    contactsButton
                    HStack {
                        pasteButton
                        Spacer()
                        deleteButton
                    }
                }
            }
            .buttonStyle(.glass).buttonBorderShape(.capsule).controlSize(.large)
        }
    }

    private var pasteButton: some View {
        PasteButton(payloadType: String.self) { values in
            if let value = values.first { number = CallDigits.normalize(value) }
        }.labelStyle(.iconOnly).buttonBorderShape(.circle)
    }

    private var contactsButton: some View {
        Button("calls.contacts", systemImage: "person.crop.circle") { editing = false; contactsPresented = true }
            .accessibilityIdentifier("calls-contacts")
    }

    private var deleteButton: some View {
        Button("calls.deleteDigit", systemImage: "delete.left") { if !number.isEmpty { number.removeLast() } }
            .labelStyle(.iconOnly).buttonBorderShape(.circle).disabled(number.isEmpty)
            .accessibilityIdentifier("calls-delete-digit")
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

    private var service: some View {
        VStack(alignment: .leading, spacing: 20) {
            ForEach(Array(ussd.messages.enumerated()), id: \.offset) { _, message in Text(message).textSelection(.enabled) }
            if let error = ussd.failure { FailureView(failure: error) }
            if ussd.busy { ProgressView("calls.ussdWaiting") }
            if !ussd.ended {
                TextField("calls.ussdReply", text: $ussd.reply).textFieldStyle(.roundedBorder).keyboardType(.numbersAndPunctuation)
                Button("calls.sendReply") { ussd.send(ussd.reply) }
                    .buttonStyle(.glassProminent).disabled(ussd.busy || ussd.reply.isEmpty)
            }
            Text("calls.ussdHint").font(.footnote).foregroundStyle(.secondary)
        }.frame(maxWidth: .infinity, alignment: .leading)
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
        GlassEffectContainer(spacing: 12) {
            Grid(horizontalSpacing: 20, verticalSpacing: compact ? 8 : 16) {
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
            .buttonStyle(.glass).buttonBorderShape(.circle).tint(.primary)
        }.frame(maxWidth: 360).frame(maxWidth: .infinity)
    }

    private func keyButton(_ key: Key) -> some View {
        let letters = key.digit == "0" && allowsPlus ? "+" : key.letters
        return Button { pressed(key.digit) } label: {
            VStack(spacing: 0) {
                // Bound the glyph sizes so the telephone grid stays usable at accessibility sizes.
                Text(key.digit).font(.system(size: min(digitSize, compact ? 24 : 44), design: .rounded)).lineLimit(1).minimumScaleFactor(0.5)
                Text(letters.isEmpty ? " " : letters).font(.system(size: min(lettersSize, compact ? 10 : 16), weight: .semibold)).tracking(1.5)
                    .lineLimit(1).minimumScaleFactor(0.5)
                    .frame(height: compact ? 12 : 20)
            }.frame(maxWidth: .infinity, minHeight: compact ? 36 : 64)
        }
        .accessibilityLabel(key.digit).accessibilityValue(letters).accessibilityIdentifier("call-key-\(key.digit)")
    }
}

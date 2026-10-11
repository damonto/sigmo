import Contacts
import ContactsUI
import Observation
import PhoneNumberKit
import SwiftUI

struct CallContactName: Sendable { let number, name: String }

@MainActor @Observable
final class CallContacts {
    private(set) var authorization: CNAuthorizationStatus
    private(set) var names: [String: String] = [:]
    var failure: BackendFailure?
    @ObservationIgnored private let formatter = PhoneNumberUtility()
    @ObservationIgnored private let status: @MainActor () -> CNAuthorizationStatus
    @ObservationIgnored private let readNames: @Sendable () async throws -> [CallContactName]
    @ObservationIgnored private var revision = 0

    init(status: @escaping @MainActor () -> CNAuthorizationStatus = { CNContactStore.authorizationStatus(for: .contacts) },
         readNames: @escaping @Sendable () async throws -> [CallContactName] = CallContacts.fetchNames) {
        self.status = status; self.readNames = readNames
        authorization = status()
    }

    func name(for number: String, region: String) -> String? {
        if let parsed = try? formatter.parse(number, withRegion: region.isEmpty ? "US" : region, ignoreType: true) {
            return names[formatter.format(parsed, toType: .e164)] ?? names[CallDigits.normalize(number)]
        }
        return names[CallDigits.normalize(number)]
    }

    func remember(number: String, name: String) {
        guard !name.isEmpty else { return }
        names[CallDigits.normalize(number)] = name
    }

    func authorize() async {
        do { _ = try await CNContactStore().requestAccess(for: .contacts); await reload() }
        catch { failure = .wrapping(error); await reload() }
    }

    func reload() async {
        guard !Task.isCancelled else { return }
        revision += 1
        let current = revision
        authorization = status()
        let startedWith = authorization
        guard authorization == .authorized || authorization == .limited else { names = [:]; return }
        // Limited selections may shrink without changing authorizationStatus.
        if authorization == .limited { names = [:] }
        do {
            let entries = try await readNames()
            try Task.checkCancellation()
            guard revision == current else { return }
            authorization = status()
            guard authorization == startedWith, authorization == .authorized || authorization == .limited else { names = [:]; return }
            names = Dictionary(entries.map { ($0.number, $0.name) }, uniquingKeysWith: { first, _ in first })
            failure = nil
        } catch {
            if current == revision, !Task.isCancelled {
                authorization = status()
                if authorization != .authorized { names = [:] }
                failure = .wrapping(error)
            }
        }
    }

    // Enumeration stays off the main actor while retaining the caller's cancellation.
    @concurrent private static func fetchNames() async throws -> [CallContactName] {
        try Task.checkCancellation()
        let store = CNContactStore()
        let formatter = PhoneNumberUtility()
        let region = Locale.current.region?.identifier ?? "US"
        let request = CNContactFetchRequest(keysToFetch: [
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
            CNContactPhoneNumbersKey as CNKeyDescriptor,
        ])
        var result: [CallContactName] = []
        try store.enumerateContacts(with: request) { contact, stop in
            guard !Task.isCancelled else { stop.pointee = true; return }
            let name = CNContactFormatter.string(from: contact, style: .fullName) ?? ""
            guard !name.isEmpty else { return }
            for phone in contact.phoneNumbers {
                let raw = phone.value.stringValue
                if let number = try? formatter.parse(raw, withRegion: region, ignoreType: true) {
                    result.append(CallContactName(number: formatter.format(number, toType: .e164), name: name))
                } else { result.append(CallContactName(number: CallDigits.normalize(raw), name: name)) }
            }
        }
        try Task.checkCancellation()
        return result
    }
}

struct CallContactPicker: UIViewControllerRepresentable {
    var selected: (String, String) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }
    func makeUIViewController(context: Context) -> CNContactPickerViewController {
        let picker = CNContactPickerViewController()
        picker.delegate = context.coordinator
        picker.displayedPropertyKeys = [CNContactPhoneNumbersKey]
        picker.predicateForEnablingContact = NSPredicate(format: "phoneNumbers.@count > 0")
        picker.predicateForSelectionOfContact = NSPredicate(value: false)
        picker.predicateForSelectionOfProperty = NSPredicate(format: "key == 'phoneNumbers'")
        return picker
    }
    func updateUIViewController(_ controller: CNContactPickerViewController, context: Context) {
        context.coordinator.parent = self
    }

    static func dismantleUIViewController(_ controller: CNContactPickerViewController, coordinator: Coordinator) {
        controller.delegate = nil
    }

    @MainActor final class Coordinator: NSObject, @preconcurrency CNContactPickerDelegate {
        var parent: CallContactPicker
        init(parent: CallContactPicker) { self.parent = parent }
        func contactPicker(_ picker: CNContactPickerViewController, didSelect contactProperty: CNContactProperty) {
            guard let phone = contactProperty.value as? CNPhoneNumber else { return }
            parent.selected(phone.stringValue, CNContactFormatter.string(from: contactProperty.contact, style: .fullName) ?? "")
            parent.dismiss()
        }
        func contactPickerDidCancel(_ picker: CNContactPickerViewController) { parent.dismiss() }
    }
}

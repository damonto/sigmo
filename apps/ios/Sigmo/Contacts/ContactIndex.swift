import UIKit

struct ContactIndexSection: Identifiable {
    let title: String
    var contacts: [CommunicationRecord<SigmoContact>] = []
    var id: String { title }

    @MainActor static func sections(_ contacts: [CommunicationRecord<SigmoContact>]) -> [Self] {
        let collation = UILocalizedIndexedCollation.current()
        var sections = collation.sectionTitles.map { Self(title: $0) }
        for contact in contacts {
            let index = collation.section(for: ContactIndexName(contact.value.displayName), collationStringSelector: #selector(getter: ContactIndexName.name))
            sections[index].contacts.append(contact)
        }
        // The source already uses the user's localized name ordering.
        return sections
    }
}

// UIKit's locale-specific collation requires an Objective-C string getter.
private final class ContactIndexName: NSObject {
    @objc let name: String
    init(_ name: String) { self.name = name }
}

// Preserve ambiguous matches while indexing each backend's current address book.
@MainActor
struct ContactIndex {
    private let contacts: [SigmoContact]
    private var keys: [String: Set<Int>] = [:]
    private var numbers: [String: Set<Int>] = [:]

    init(_ contacts: [SigmoContact]) {
        self.contacts = contacts
        for (index, contact) in contacts.enumerated() {
            for phone in contact.numbers {
                keys[phone.key, default: []].insert(index)
                numbers[phone.number, default: []].insert(index)
            }
        }
    }

    func matches(_ number: String) -> [SigmoContact] {
        let key = PhoneNumberDisplay.submission(number, regionCode: nil)
        let indices = (keys[key] ?? []).union(numbers[number] ?? [])
        return indices.sorted().map { contacts[$0] }
    }
}

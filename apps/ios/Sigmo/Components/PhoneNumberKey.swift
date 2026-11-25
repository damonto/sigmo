import Foundation

enum PhoneNumberKey {
    static func compact(_ value: String) -> String {
        var number = ""
        for character in value {
            if character >= "0" && character <= "9" {
                number.append(character)
            } else if character == "+" && number.isEmpty {
                number.append(character)
            }
        }
        return number == "+" ? "" : number
    }

    private static let phoneCharacters = CharacterSet.decimalDigits
        .union(.whitespacesAndNewlines).union(CharacterSet(charactersIn: "+()-."))

    // Text sender IDs must keep their identity, even if they contain a short code.
    static func smsSender(_ value: String) -> String? {
        guard value.unicodeScalars.allSatisfy(phoneCharacters.contains) else { return nil }
        let number = compact(value)
        return number.isEmpty ? nil : number
    }
}

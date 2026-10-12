import PhoneNumberKit
import SwiftUI

@MainActor
enum PhoneNumberDisplay {
    // Metadata is expensive to load; all number views share one parser on the UI actor.
    private static let utility = PhoneNumberUtility()

    static func submission(_ value: String, regionCode: String?) -> String {
        var chars = ""
        for character in value {
            if character >= "0" && character <= "9" { chars.append(character) }
            else if character == "+" && chars.isEmpty { chars.append(character) }
        }
        guard !chars.isEmpty, chars != "+" else { return "" }
        if !chars.hasPrefix("+"), chars.count <= 6 { return chars }
        let region = regionCode?.uppercased() ?? ""
        let callingCode = utility.countryCode(for: region)
        if chars.hasPrefix("+") { return chars }
        guard let callingCode else { return chars }
        if let parsed = try? utility.parse(chars, withRegion: region, ignoreType: true) {
            let international = utility.format(parsed, toType: .e164)
            return international == "+" + chars || parsed.countryCode != callingCode ? international : chars
        }
        if let parsed = try? utility.parse("+" + chars, withRegion: region, ignoreType: true), parsed.countryCode != callingCode {
            return utility.format(parsed, toType: .e164)
        }
        return chars
    }

    static func format(_ number: String, regionCode: String?) -> String {
        let raw = number.trimmingCharacters(in: .whitespacesAndNewlines)
        let region = regionCode?.trimmingCharacters(in: .whitespacesAndNewlines).uppercased() ?? ""
        let countryCode = utility.countryCode(for: region)
        // Without a SIM region, a national number must not inherit this iPhone's region.
        guard !raw.isEmpty, raw.allSatisfy({ "0123456789+ -.()".contains($0) }),
              raw.filter(\.isNumber).count > 6,
              raw.hasPrefix("+") || countryCode != nil,
              let parsed = try? utility.parse(raw, withRegion: countryCode == nil ? "US" : region, ignoreType: true) else {
            return raw
        }
        // The fallback region is used only with an explicit + country code; the parser
        // requires valid metadata even when that prefix determines the number’s country.
        // NANP national formatting has no trunk prefix, so it can safely follow +1.
        if parsed.countryCode == 1 { return "+1 " + utility.format(parsed, toType: .national) }
        return utility.format(parsed, toType: .international)
    }
}

struct PhoneNumberText: View {
    let number: String
    var regionCode: String?

    var body: some View {
        let formatted = PhoneNumberDisplay.format(number, regionCode: regionCode)
        Text(formatted.isEmpty ? String(localized: "modem.noNumber") : formatted)
            .monospacedDigit()
    }
}

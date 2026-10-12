import SwiftUI

struct RegionFlag: View {
    let regionCode: String
    var size: CGFloat = 48
    var background: Color = Color(uiColor: .tertiarySystemGroupedBackground)

    private var code: String { regionCode.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() }

    var body: some View {
        Group {
            if code.count == 2, code.utf8.allSatisfy({ (97...122).contains($0) }),
               let flag = UIImage(named: "flag-\(code)") {
                Image(uiImage: flag).resizable().aspectRatio(4 / 3, contentMode: .fit)
            } else {
                Image(systemName: "globe").foregroundStyle(.secondary)
            }
        }
        .frame(width: size, height: size * 3 / 4)
        .clipShape(RoundedRectangle(cornerRadius: 2))
        .frame(width: size, height: size)
        .background(background, in: RoundedRectangle(cornerRadius: 12))
        .accessibilityLabel(Locale.current.localizedString(forRegionCode: code.uppercased()) ?? String(localized: "modem.region"))
    }
}

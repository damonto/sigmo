import SwiftUI

/// A profile's embedded brand takes priority over the shared carrier catalog.
struct CarrierLogo: View {
    var icon: String? = nil
    var plmn: String? = nil
    let regionCode: String
    var name = ""
    var size: CGFloat = 32

    static func embeddedImage(_ icon: String?) -> UIImage? {
        guard let icon, icon.hasPrefix("data:image/"), let comma = icon.firstIndex(of: ","),
              icon[..<comma].hasSuffix(";base64"),
              let data = Data(base64Encoded: String(icon[icon.index(after: comma)...])) else { return nil }
        return UIImage(data: data)
    }

    nonisolated static func url(plmn: String?) -> URL? {
        guard let plmn, (5...6).contains(plmn.utf8.count),
              plmn.utf8.allSatisfy({ (48...57).contains($0) }) else { return nil }
        return URL(string: "https://sigmo-pro.952007.xyz/logo/\(plmn)")
    }

    var body: some View {
        Group {
            if let bitmap = Self.embeddedImage(icon) {
                Image(uiImage: bitmap).resizable().scaledToFit()
                    .frame(width: size * 2 / 3, height: size / 2)
            } else if !AppLaunch.usesFixtures, let url = Self.url(plmn: plmn) {
                AsyncImage(url: url) { phase in
                    if let image = phase.image {
                        image.resizable().scaledToFit()
                            .frame(width: size * 2 / 3, height: size / 2)
                    } else { flag }
                }
                .id(url)
            } else {
                flag
            }
        }
        .frame(width: size, height: size)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(name)
        .accessibilityHidden(name.isEmpty)
    }

    private var flag: some View {
        RegionFlag(regionCode: regionCode, size: size, background: .clear)
    }
}

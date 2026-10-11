import SwiftUI

struct SettingsLabel: View {
    let title: String
    let symbol: String
    let color: Color
    @ScaledMetric(relativeTo: .body) private var iconSize = 30.0

    var body: some View {
        Label {
            Text(LocalizedStringKey(title))
                .fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: symbol)
                .symbolRenderingMode(.monochrome)
                .font(.system(size: iconSize * 0.53, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: iconSize, height: iconSize)
                .background(color, in: RoundedRectangle(cornerRadius: iconSize * 0.22, style: .continuous))
                .accessibilityHidden(true)
        }
        .labelStyle(.titleAndIcon)
    }
}

import SwiftUI

struct ModemCard: View {
    let modem: Modem
    let capabilities: Set<String>
    let backendID: UUID
    let unreadCount: Int
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private let outline = RoundedRectangle(cornerRadius: 20)
    private var identity: String { "\(backendID)-\(modem.id)" }
    private var secondaryForeground: Color { Color.primary.opacity(0.75) }

    var body: some View {
        CarrierLogo(icon: modem.sim.icon, plmn: modem.sim.operatorIdentifier,
                    regionCode: modem.sim.regionCode, size: 44).withTint { logo, tint in
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 12) {
                    logo
                    VStack(alignment: .leading, spacing: 4) {
                        carrier
                        number
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "chevron.forward")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                }
                Divider().accessibilityHidden(true)
                footer
            }
            .padding(14)
            .background { cardBackground(tint: tint) }
            .contentShape(outline)
            .accessibilityElement(children: .combine)
            .accessibilityHint(modem.displayName)
        }
    }

    private var footer: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(spacing: 8))
        return layout {
            metadata.frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 8) {
                indicators
                if unreadCount > 0 { unreadBadge }
            }
        }
    }

    private func cardBackground(tint: Color?) -> some View {
        let dark = colorScheme == .dark
        return outline.fill(Color(uiColor: .secondarySystemGroupedBackground))
            .overlay {
                if let tint {
                    LinearGradient(colors: [tint.opacity(dark ? 0.10 : 0.06), tint.opacity(dark ? 0.22 : 0.20)],
                                   startPoint: .topLeading, endPoint: .bottomTrailing)
                        .overlay(alignment: .bottomTrailing) {
                            Circle()
                                .strokeBorder(tint.opacity(dark ? 0.09 : 0.10), lineWidth: 28)
                                .frame(width: 230, height: 230)
                                .offset(x: 86, y: 92)
                        }
                        .clipShape(outline)
                }
            }
            .overlay { outline.strokeBorder(tint?.opacity(dark ? 0.30 : 0.24) ?? Color.primary.opacity(0.10)) }
            .shadow(color: .black.opacity(dark ? 0.22 : 0.06), radius: 5, y: 2)
            .accessibilityHidden(true)
    }

    private var carrier: some View {
        Text(modem.carrier.isEmpty ? modemValue(nil) : modem.carrier)
            .font(.headline)
            .foregroundStyle(.primary)
            .accessibilityIdentifier("modemCarrier-\(identity)")
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
    }

    @ViewBuilder
    private var number: some View {
        Group {
            if modem.number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text("home.noNumber")
                    .foregroundStyle(secondaryForeground)
            } else {
                Text(PhoneNumberDisplay.format(modem.number))
                    .foregroundStyle(.primary)
            }
        }
        .font(.subheadline)
        .monospacedDigit()
        .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityIdentifier("modemNumber-\(identity)")
    }

    private var metadata: some View {
        let sim = modem.sim.identifier.isEmpty ? String(localized: "modem.simUnavailable") : modemValue(modem.simKind)
        return Text("\(modem.displayName) · \(sim)")
            .font(.caption)
            .foregroundStyle(secondaryForeground)
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
            .truncationMode(.middle)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("modemMetadata-\(identity)")
    }

    private var indicators: some View {
        ModemStatusIcons(modem: modem, capabilities: capabilities, identity: identity, foreground: secondaryForeground)
            // Preserve a compact glyph row; VoiceOver still announces every status.
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .fixedSize()
    }

    private var unreadBadge: some View {
        Text(unreadCount > 99 ? "99+" : unreadCount.formatted())
            .font(.caption.weight(.semibold))
            .foregroundStyle(.white)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(.red, in: Capsule())
            .fixedSize()
            .accessibilityLabel(Text("messages.unread"))
            .accessibilityValue(unreadCount.formatted())
            .accessibilityIdentifier("modemUnread-\(identity)")
    }
}

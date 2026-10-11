import SwiftUI

struct ModemRow: View {
    let modem: Modem
    let capabilities: Set<String>
    let backendID: UUID
    let backendName: String
    let unreadCount: Int

    private var identity: String { "\(backendID)-\(modem.id)" }

    var body: some View {
        HStack(spacing: 12) {
            RegionFlag(regionCode: modem.sim.regionCode, size: 56)
            VStack(alignment: .leading, spacing: 5) {
                header
                number
                HStack(spacing: 6) {
                    backendBadge
                    modemBadge
                    simBadge.fixedSize()
                    Spacer(minLength: 4)
                    if unreadCount > 0 {
                        Text(unreadCount > 99 ? "99+" : unreadCount.formatted())
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 7).padding(.vertical, 3)
                            .background(.red, in: Capsule())
                            .fixedSize()
                            .accessibilityLabel(Text("messages.unread"))
                            .accessibilityValue(unreadCount.formatted())
                            .accessibilityIdentifier("modemUnread-\(identity)")
                    }
                }
                .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityHint(modem.displayName)
    }

    private var header: some View {
        HStack(spacing: 8) {
            carrier.frame(maxWidth: .infinity, alignment: .leading)
            indicators
        }
        .font(.headline)
        .lineLimit(1)
    }

    private var carrier: some View {
        Text(modem.carrier.isEmpty ? modemValue(nil) : modem.carrier)
            .foregroundStyle(.primary)
            .accessibilityIdentifier("modemCarrier-\(identity)")
            .lineLimit(1)
    }

    @ViewBuilder
    private var number: some View {
        Group {
            if modem.number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text("home.noNumber")
            } else {
                PhoneNumberText(number: modem.number, regionCode: modem.sim.regionCode)
            }
        }
        .font(.subheadline)
        .minimumScaleFactor(0.5)
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .accessibilityIdentifier("modemNumber-\(identity)")
    }

    private var simBadge: some View {
        tag(modem.sim.identifier.isEmpty ? String(localized: "modem.simUnavailable") : modemValue(modem.simKind))
            .accessibilityIdentifier("modemSIM-\(identity)")
    }

    private var backendBadge: some View {
        tag(backendName).accessibilityIdentifier("modemBackend-\(identity)")
    }

    private var modemBadge: some View {
        tag(modem.displayName)
            .truncationMode(.tail)
            .accessibilityIdentifier("modemName-\(identity)")
    }

    private func tag(_ title: String) -> some View {
        Text(title)
            .font(.caption.weight(.medium)).foregroundStyle(.secondary)
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Color(uiColor: .tertiarySystemGroupedBackground), in: Capsule())
            .lineLimit(1)
    }

    private var indicators: some View {
        ModemStatusIcons(modem: modem, capabilities: capabilities, identity: identity)
            // Preserve a compact glyph row; VoiceOver still announces every status.
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .fixedSize()
    }
}

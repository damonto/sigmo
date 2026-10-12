import SwiftUI

struct ModemRow: View {
    let modem: Modem
    let capabilities: Set<String>
    let backendID: UUID
    let unreadCount: Int
    var session: BackendSession? = nil
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var identity: String { "\(backendID)-\(modem.id)" }

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            if let session {
                HomeModemLogo(session: session, modem: modem)
            } else {
                CarrierLogo(plmn: modem.sim.operatorIdentifier, regionCode: modem.sim.regionCode, size: 40)
            }
            VStack(alignment: .leading, spacing: 4) {
                header
                number
                HStack(spacing: 6) {
                    metadata
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
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityHint(modem.displayName)
    }

    @ViewBuilder
    private var header: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                carrier
                indicators
            }
        } else {
            HStack(spacing: 8) {
                carrier.frame(maxWidth: .infinity, alignment: .leading)
                indicators
            }
        }
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
                    .foregroundStyle(.secondary)
            } else {
                PhoneNumberText(number: modem.number, regionCode: modem.sim.regionCode)
                    .foregroundStyle(.primary)
            }
        }
        .font(.subheadline)
        .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityIdentifier("modemNumber-\(identity)")
    }

    private var metadata: some View {
        let sim = modem.sim.identifier.isEmpty ? String(localized: "modem.simUnavailable") : modemValue(modem.simKind)
        return Text("\(modem.displayName) · \(sim)")
            .font(.caption)
            .foregroundStyle(.secondary)
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
            .truncationMode(.middle)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("modemMetadata-\(identity)")
    }

    private var indicators: some View {
        ModemStatusIcons(modem: modem, capabilities: capabilities, identity: identity, foreground: .secondary)
            // Preserve a compact glyph row; VoiceOver still announces every status.
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .fixedSize()
    }
}

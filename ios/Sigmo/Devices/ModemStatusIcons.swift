import SwiftUI

struct ModemStatusIcons: View {
    let modem: Modem
    let capabilities: Set<String>
    var identity: String?
    @ScaledMetric(relativeTo: .caption) private var iconSize = 14.0

    var body: some View {
        HStack(spacing: 7) {
            if modem.internetConnected == true {
                Image(systemName: "arrow.up.arrow.down")
                    .accessibilityLabel(Text("modem.dataConnected"))
            }
            if capabilities.contains("wifiCalling"), modem.wifiCallingStatus == .connected {
                Image("ims-vowifi")
                    .resizable().scaledToFit()
                    .frame(width: iconSize * 1.4, height: iconSize)
                    .accessibilityLabel(Text("modem.wifiConnected"))
                    .accessibilityValue(Text("common.connected"))
            }
            if !modem.airplaneMode, capabilities.contains("volte"), modem.volteStatus == .connected {
                Image("ims-volte")
                    .resizable().scaledToFit()
                    .frame(width: iconSize * 1.4, height: iconSize)
                    .accessibilityLabel(Text("modem.volteConnected"))
                    .accessibilityIdentifier("volte-\(identity ?? modem.id)")
            }
            if !modem.airplaneMode, let technology = modem.accessTechnology, !technology.isEmpty {
                // Radio generations use their familiar compact glyphs, without a text label or badge.
                Group {
                    if Self.radioGlyph(technology) == "?" {
                        Image(systemName: "questionmark")
                    } else {
                        Image("radio-\(Self.radioGlyph(technology).lowercased())")
                            .resizable().scaledToFit()
                            // Compensate for the PDF's transparent margins without enlarging the status row.
                            .scaleEffect(1.35)
                            .frame(width: iconSize * 1.6, height: iconSize)
                    }
                }
                .accessibilityLabel(Text("modem.technology"))
                .accessibilityValue(modemValue(technology))
            }
            signal
        }
        .font(.system(size: iconSize, weight: .medium))
        .foregroundStyle(.primary)
    }

    private var signal: some View {
        Group {
            if modem.airplaneMode {
                Image(systemName: "airplane")
                    .accessibilityLabel(Text("modem.airplane"))
                    .accessibilityIdentifier("airplane-\(identity ?? modem.id)")
            } else {
                HStack(alignment: .bottom, spacing: 2) {
                    ForEach(0..<4) { index in
                        RoundedRectangle(cornerRadius: 1.5)
                            .fill(modem.signalPercent > index * 25 ? Color.primary : Color(uiColor: .systemGray4))
                            .frame(width: iconSize / 4, height: iconSize * Double(index + 1) / 4)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text("modem.signal"))
                .accessibilityValue("\(modem.signalPercent)%")
                .accessibilityIdentifier("signal-\(identity ?? modem.id)")
            }
        }
    }

    static func radioGlyph(_ technology: String) -> String {
        switch technology.uppercased() {
        case "LTE", "LTE-M", "LTE CAT-M", "LTE NB-IOT": "4G"
        case "5GNR", "5G NR", "NR", "5G": "5G"
        case "UMTS", "HSDPA", "HSUPA", "HSPA", "HSPA+": "3G"
        case "GSM", "GPRS", "EDGE": "2G"
        default: "?"
        }
    }
}

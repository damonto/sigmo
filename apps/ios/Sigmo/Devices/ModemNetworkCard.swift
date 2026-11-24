import SwiftUI

struct ModemNetworkCard<SlotPicker: View>: View {
    let modem: Modem
    let capabilities: Set<String>
    let accent: Color
    let iconTint: Color
    @ViewBuilder let slotPicker: () -> SlotPicker

    @ScaledMetric(relativeTo: .largeTitle) private var signalSize = 36.0
    @ScaledMetric(relativeTo: .title2) private var symbolSize = 26.0
    @ScaledMetric(relativeTo: .caption) private var voiceIconSize = 14.0

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 22)
        return VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    heading
                    Spacer(minLength: 4)
                    registrationBadge
                }
                VStack(alignment: .leading, spacing: 8) {
                    heading
                    registrationBadge
                }
            }
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    signal
                    Spacer(minLength: 0)
                    controls(alignment: .trailing)
                }
                VStack(alignment: .leading, spacing: 8) {
                    signal
                    controls(alignment: .leading)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemGroupedBackground), in: shape)
        .overlay { shape.strokeBorder(accent.opacity(0.18)).allowsHitTesting(false) }
    }

    private var heading: some View {
        Label {
            title
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("modemNetworkTitle")
        } icon: {
            Image(systemName: "waveform.path")
                .foregroundStyle(iconTint)
                .accessibilityHidden(true)
        }
    }

    private var title: Text {
        modem.networkOverviewCarrier.map { Text(verbatim: $0) } ?? Text("detail.networkStatus")
    }

    private var registration: (label: LocalizedStringKey, color: Color) {
        if modem.airplaneMode { return ("modem.airplane", .secondary) }
        switch modem.cellularRegistration {
        case .home: return ("modem.value.home", .green)
        case .roaming: return ("modem.value.roaming", .orange)
        case .searching: return ("modem.value.searching", .orange)
        case .denied: return ("modem.value.denied", .red)
        case .idle: return ("detail.networkNotRegistered", .secondary)
        case .unknown: return ("common.unknown", .secondary)
        }
    }

    private var registrationBadge: some View {
        let registration = registration
        return Label {
            Text(registration.label)
        } icon: {
            Image(systemName: modem.airplaneMode ? "airplane" : "circle.fill")
                .font(.caption2)
                .imageScale(modem.airplaneMode ? .medium : .small)
                .foregroundStyle(registration.color)
                .accessibilityHidden(true)
        }
        .font(.caption.weight(.semibold))
        .padding(.horizontal, 9)
        .padding(.vertical, 5)
        .background(registration.color.opacity(0.14), in: Capsule())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(registration.label))
        .accessibilityIdentifier("modemNetworkRegistration")
    }

    private var signal: some View {
        let signal = modem.cellularSignalFraction
        let value = signal.map { Text($0, format: .percent) }
        return HStack(alignment: .center, spacing: 8) {
            Group {
                if modem.airplaneMode {
                    Image(systemName: "airplane")
                        .accessibilityHidden(true)
                } else {
                    Image(systemName: "cellularbars", variableValue: signal ?? 0)
                        .symbolRenderingMode(.hierarchical)
                        .accessibilityLabel(Text("modem.signal"))
                        .accessibilityValue(value ?? Text("common.unknown"))
                        .accessibilityIdentifier("signal-\(modem.id)")
                }
            }
            .font(.system(size: symbolSize, weight: .medium))
            .foregroundStyle(iconTint)
            VStack(alignment: .leading, spacing: 0) {
                (value ?? Text(verbatim: "—"))
                    .font(.system(size: signalSize, weight: .semibold))
                    .monospacedDigit()
                Text("modem.signal")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .accessibilityHidden(true)
        }
    }

    private func controls(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 4) {
            HStack(spacing: 6) {
                if let technology = modem.cellularTechnology {
                    let generation = ModemStatusIcons.radioGlyph(technology)
                    connectionBadge(Text(generation == "?" ? modemValue(technology) : generation))
                        .accessibilityLabel(Text("modem.technology"))
                        .accessibilityValue(modemValue(technology))
                }
                if capabilities.contains("volte"), modem.hasVoLTEConnection {
                    connectionBadge(Text("capability.volte"))
                        .accessibilityLabel(Text("modem.volteConnected"))
                        .accessibilityIdentifier("volte-\(modem.id)")
                }
                if capabilities.contains("wifiCalling"), modem.wifiCallingStatus == .connected {
                    Image("ims-vowifi")
                        .resizable().scaledToFit()
                        .frame(width: voiceIconSize * 1.4, height: voiceIconSize)
                        .accessibilityLabel(Text("modem.wifiConnected"))
                        .accessibilityValue(Text("common.connected"))
                }
                if modem.internetConnected == true {
                    Image(systemName: "arrow.up.arrow.down")
                        .accessibilityLabel(Text("modem.dataConnected"))
                }
            }
            .font(.caption.weight(.semibold))
            slotPicker()
                .font(.footnote.weight(.medium))
                .tint(.primary)
        }
    }

    private func connectionBadge(_ title: Text) -> some View {
        title.padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(iconTint.opacity(0.12), in: Capsule())
    }
}

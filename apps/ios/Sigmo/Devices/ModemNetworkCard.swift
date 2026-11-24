import SwiftUI

struct ModemNetworkCard<SlotPicker: View>: View {
    let modem: Modem
    let capabilities: Set<String>
    let accent: Color
    let iconTint: Color
    @ViewBuilder let slotPicker: () -> SlotPicker

    @ScaledMetric(relativeTo: .largeTitle) private var signalSize = 34.0
    @ScaledMetric(relativeTo: .title2) private var symbolSize = 24.0
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 22)
        return VStack(alignment: .leading, spacing: 12) {
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
            carrierRow
            radioMetrics
            services
            Divider()
            networkDetails
        }
        .padding(.horizontal, 16)
        .padding(.top, 13)
        .padding(.bottom, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemGroupedBackground), in: shape)
        .overlay { shape.strokeBorder(accent.opacity(0.18)).allowsHitTesting(false) }
    }

    private var heading: some View {
        HStack(spacing: 6) {
            Image(systemName: "waveform.path")
                .foregroundStyle(iconTint)
                .accessibilityHidden(true)
            Text("detail.networkStatus")
                .accessibilityAddTraits(.isHeader)
        }
        .font(.headline)
    }

    private var carrierRow: some View {
        let label: LocalizedStringKey = modem.cellularOperator?.displayName == nil && modem.simOperatorName != nil
            ? "detail.simOperator" : "detail.registeredNetwork"
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(alignment: .bottom, spacing: 8))
        return layout {
            VStack(alignment: .leading, spacing: 2) {
                Text(label)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                (modem.networkOverviewCarrier.map { Text(verbatim: $0) } ?? Text("common.unknown"))
                    .font(.title3.weight(.semibold))
                    .accessibilityIdentifier("modemNetworkTitle")
            }
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            slotPicker()
                .font(.footnote.weight(.medium))
                .tint(.primary)
        }
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
        return HStack(spacing: 4) {
            Image(systemName: modem.airplaneMode ? "airplane" : "circle.fill")
                .font(.caption2)
                .imageScale(modem.airplaneMode ? .medium : .small)
                .foregroundStyle(registration.color)
                .accessibilityHidden(true)
            Text(registration.label)
        }
        .font(.caption.weight(.semibold))
        .padding(.horizontal, 9)
        .padding(.vertical, 5)
        .background(registration.color.opacity(0.14), in: Capsule())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(registration.label))
        .accessibilityIdentifier("modemNetworkRegistration")
    }

    private var radioMetrics: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .top, spacing: 12) {
                signal
                Divider()
                technology
            }
            .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 8) {
                signal
                Divider()
                technology
            }
        }
        .padding(12)
        .background(accent.opacity(0.09), in: RoundedRectangle(cornerRadius: 14))
    }

    private var signal: some View {
        let signal = modem.cellularSignalFraction
        let value = signal.map { Text($0, format: .percent) }
        return VStack(alignment: .leading, spacing: 2) {
            Text("modem.signal")
                .font(.caption)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            HStack(spacing: 8) {
                Group {
                    if modem.airplaneMode {
                        Image(systemName: "airplane")
                            .accessibilityLabel(Text("modem.airplane"))
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
                (value ?? Text(verbatim: "—"))
                    .font(.system(size: signalSize, weight: .semibold))
                    .monospacedDigit()
                    .accessibilityHidden(true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var technology: some View {
        let value = modem.cellularTechnology.map(modemValue)
        return VStack(alignment: .leading, spacing: 4) {
            Text("modem.technology")
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(verbatim: value ?? "—")
                .font(.title2.weight(.semibold))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("modem.technology"))
        .accessibilityValue(value.map { Text(verbatim: $0) } ?? Text("common.unknown"))
        .accessibilityIdentifier("modemNetworkTechnology")
    }

    private var services: some View {
        ViewThatFits(in: .horizontal) {
            if !dynamicTypeSize.isAccessibilitySize {
                // Only use the horizontal layout if the service labels fit at their natural widths.
                HStack(alignment: .top, spacing: 0) { serviceItems(horizontal: true) }
            }
            VStack(alignment: .leading, spacing: 10) { serviceItems(horizontal: false) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func serviceItems(horizontal: Bool) -> some View {
        service("detail.mobileData", icon: "arrow.up.arrow.down", status: modem.mobileDataStatus)
            .fixedSize(horizontal: horizontal, vertical: false)
            .accessibilityIdentifier("modemNetworkData")
        if capabilities.contains("volte") {
            if horizontal { Spacer(minLength: 14) }
            service("capability.volte", icon: "phone", status: modem.volteStatus)
                .fixedSize(horizontal: horizontal, vertical: false)
                .accessibilityIdentifier("modemNetworkVoLTE")
        }
        if capabilities.contains("wifiCalling") {
            if horizontal { Spacer(minLength: 14) }
            service("capability.wifiCalling", icon: "wifi", status: modem.wifiCallingStatus)
                .fixedSize(horizontal: horizontal, vertical: false)
                .accessibilityIdentifier("modemNetworkWiFiCalling")
        }
    }

    private func service(_ title: LocalizedStringKey, icon: String, status: ModemConnectionStatus) -> some View {
        let connected = status == .connected
        let value = Text(verbatim: status.displayName)
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .foregroundStyle(iconTint)
                Text(title)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .font(.caption)
            HStack(spacing: 4) {
                Image(systemName: "circle.fill")
                    .font(.caption2)
                    .imageScale(.small)
                    .foregroundStyle(connected ? Color.green : Color.secondary)
                value
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(connected ? .primary : .secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(title))
        .accessibilityValue(value)
    }

    private var networkDetails: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                DetailValue(title: "detail.simOperator", value: modem.simOperatorName)
                DetailValue(title: "detail.registeredNetwork", value: modem.cellularOperator?.displayName)
                DetailValue(title: "detail.registeredOperatorCode",
                            value: modem.cellularOperator?.code.trimmingCharacters(in: .whitespacesAndNewlines))
            }
            .padding(.top, 4)
        } label: {
            Text("detail.networkDetails")
                .fontWeight(.semibold)
                .foregroundStyle(accent)
        }
        .font(.footnote)
        .tint(accent)
        .accessibilityIdentifier("modemNetworkDetails")
    }
}

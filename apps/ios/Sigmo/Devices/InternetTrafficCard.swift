import SwiftUI

struct InternetTrafficCard: View {
    let state: InternetTrafficState
    let accent: Color
    let iconTint: Color
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.locale) private var locale

    var body: some View {
        let history = state.history
        let shape = RoundedRectangle(cornerRadius: 22)
        VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    title
                    Spacer(minLength: 0)
                    connected
                }
                VStack(alignment: .leading, spacing: 4) {
                    title
                    connected
                }
            }
            let layout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                rate("detail.trafficDownload", icon: "arrow.down", bitsPerSecond: history.rates?.download)
                    .accessibilityIdentifier("internetTrafficDownload")
                rate("detail.trafficUpload", icon: "arrow.up", bitsPerSecond: history.rates?.upload)
                    .accessibilityIdentifier("internetTrafficUpload")
            }
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    usage(history.usedBytes)
                    Spacer(minLength: 0)
                    window(history.rates)
                }
                VStack(alignment: .leading, spacing: 4) {
                    usage(history.usedBytes)
                    window(history.rates)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            shape.fill(Color(uiColor: .secondarySystemGroupedBackground))
                .overlay(alignment: .bottom) {
                    chart(history.points)
                        .frame(height: 58)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
                .clipShape(shape)
        }
        .overlay { shape.strokeBorder(accent.opacity(0.18)).allowsHitTesting(false) }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("internetTrafficCard")
    }

    private var title: some View {
        HStack(spacing: 6) {
            Image(systemName: "globe")
                .foregroundStyle(iconTint)
            Text("connection.internet")
        }
        .font(.headline)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("connection.internet"))
        .accessibilityAddTraits(.isHeader)
        .accessibilityIdentifier("internetTrafficHeading")
    }

    private var connected: some View {
        HStack(spacing: 4) {
            Image(systemName: "circle.fill")
                .imageScale(.small)
                .foregroundStyle(.green)
                .accessibilityHidden(true)
            Text("common.connected")
        }
        .font(.caption.weight(.semibold))
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(.green.opacity(0.12), in: Capsule())
    }

    private func rate(_ label: LocalizedStringKey, icon: String, bitsPerSecond: Double?) -> some View {
        let value = bitsPerSecond.map { formatRate($0) }
        return VStack(alignment: .leading, spacing: 2) {
            Label {
                Text(label)
            } icon: {
                Image(systemName: icon).foregroundStyle(accent)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            Text(value ?? "—")
                .font(.title3.weight(.semibold))
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(label))
        .accessibilityValue(value.map { Text(verbatim: $0) } ?? Text("common.unknown"))
    }

    private func formatRate(_ bitsPerSecond: Double) -> String {
        let units = ["b/s", "kb/s", "Mb/s", "Gb/s", "Tb/s"]
        var value = bitsPerSecond
        var unit = 0
        while value >= 1_000, unit < units.count - 1 {
            value /= 1_000
            unit += 1
        }
        return "\(value.formatted(.number.locale(locale).precision(.fractionLength(0...2)))) \(units[unit])"
    }

    private func usage(_ bytes: UInt64?) -> some View {
        let value = ConnectionFormat.bytes(bytes)
        return HStack(spacing: 4) {
            Text("detail.trafficUsage")
                .foregroundStyle(.secondary)
            Text(value ?? "—")
                .fontWeight(.semibold)
                .monospacedDigit()
        }
        .font(.caption)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text("detail.trafficUsage"))
        .accessibilityValue(value.map { Text(verbatim: $0) } ?? Text("common.unknown"))
        .accessibilityIdentifier("internetTrafficUsage")
    }

    private func window(_ rates: InternetTrafficHistory.Rates?) -> some View {
        Text(rates?.total == 0 ? "detail.trafficIdle" : "detail.trafficWindow")
            .font(.caption)
            .foregroundStyle(.secondary)
    }

    private func chart(_ points: [InternetTrafficHistory.Point]) -> some View {
        Canvas { context, size in
            guard let latest = points.last, points.count > 1 else { return }
            let peak = max(1_000, points.map(\.bitsPerSecond).max() ?? 0)
            let coordinates = points.map { point in
                let age = Double(latest.elapsedMilliseconds - point.elapsedMilliseconds)
                let x = size.width * (1 - age / Double(InternetTrafficHistory.windowMilliseconds))
                let y = (size.height - 1) * (1 - point.bitsPerSecond / peak)
                return CGPoint(x: x, y: y)
            }
            var line = Path()
            line.addLines(coordinates)
            var area = line
            area.addLine(to: CGPoint(x: size.width, y: size.height))
            area.addLine(to: CGPoint(x: coordinates[0].x, y: size.height))
            area.closeSubpath()
            context.fill(area, with: .color(accent.opacity(0.09)))
            context.stroke(line, with: .color(accent.opacity(0.38)), lineWidth: 1)
        }
    }
}

import SwiftUI

struct InternetSpeedTestSection: View {
    let state: InternetState
    let updatingConnection: Bool
    @State private var size: InternetSpeedTestSize = .mb10
    @Environment(\.locale) private var locale

    var body: some View {
        Section {
            if let result = state.speedTest {
                rate("detail.trafficDownload", bps: result.downloadBps, id: "internetSpeedDownload")
                rate("detail.trafficUpload", bps: result.uploadBps, id: "internetSpeedUpload")
            }
            if let failure = state.speedTestFailure {
                Text(failure.message).foregroundStyle(.secondary)
                    .accessibilityIdentifier("internetSpeedTestFailure")
            } else if state.speedTestReadFailed {
                Text("connection.speedTestReadFailed").foregroundStyle(.secondary)
            }
            Picker("connection.speedTestSize", selection: $size) {
                ForEach(InternetSpeedTestSize.allCases, id: \.self) { option in
                    Text(verbatim: "\(option.rawValue) MB").tag(option)
                }
            }
            .pickerStyle(.menu)
            .disabled(state.speedTesting)
            .accessibilityIdentifier("internetSpeedTestSize")
            AsyncActionButton(id: "internetSpeedTestButton", isWorking: state.speedTesting,
                              action: { await state.runSpeedTest(size: size) }) {
                if state.speedTesting { Text("connection.speedTesting") }
                else if state.speedTest != nil { Text("connection.speedTestAgain") }
                else { Text("connection.speedTestStart") }
            }
            .disabled(!state.canRunSpeedTest || updatingConnection)
        } header: {
            Text("connection.speedTest")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if let result = state.speedTest {
                    let time = result.testedAt.formatted(.dateTime.year().month().day().hour().minute().locale(locale))
                    Text(String(format: detailText("connection.speedTestLast"), time))
                }
                Text(String(format: detailText("connection.speedTestDataUsage"), size.rawValue))
            }
        }
    }

    private func rate(_ label: LocalizedStringKey, bps: Int64, id: String) -> some View {
        let mbps = Double(bps) / 1_000_000
        let format = FloatingPointFormatStyle<Double>.number.precision(.fractionLength(0...2)).locale(locale)
        let value = mbps < 0.01 ? "<\(0.01.formatted(format))" : mbps.formatted(format)
        return LabeledContent {
            Text("\(value) Mbps").monospacedDigit()
        } label: {
            Text(label)
        }
        .accessibilityIdentifier(id)
    }
}

import SwiftUI

func detailText(_ key: String) -> String { NSLocalizedString(key, comment: "") }

func secureElementTitle(_ id: String, elements: [SecureElement]) -> String {
    guard let index = elements.firstIndex(where: { $0.id == id }) else { return id }
    if elements.count == 1 { return detailText("modem.value.euicc") }
    return "SE \(index + 1)"
}

struct DetailValue: View {
    let title: String
    let value: String?
    var copyable = false

    var body: some View {
        LabeledContent {
            Text(modemValue(value))
                .multilineTextAlignment(.trailing)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .textSelection(.enabled)
        } label: { Text(detailText(title)) }
        .contextMenu {
            if copyable, let value, !value.isEmpty {
                Button("detail.copy", systemImage: "document.on.document") { UIPasteboard.general.string = value }
            }
        }
    }
}

extension View {
    func detailErrorAlert(_ failure: Binding<BackendFailure?>, retry: (() -> Void)? = nil) -> some View {
        alert(
            "detail.operationFailed",
            isPresented: Binding(get: { failure.wrappedValue != nil }, set: { if !$0 { failure.wrappedValue = nil } }),
            presenting: failure.wrappedValue
        ) { _ in
            if let retry { Button("common.retry", action: retry) }
            Button("common.ok", role: .cancel) { failure.wrappedValue = nil }
        } message: { failure in
            Text(failure.message)
        }
    }

    func detailSkeleton(_ label: String) -> some View {
        redacted(reason: .placeholder)
            .disabled(true)
            .allowsHitTesting(false)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(detailText(label)))
    }
}

struct ProfileAvatar: View {
    let icon: String?
    let region: String?
    @ScaledMetric(relativeTo: .body) private var size = 32.0
    private var bitmap: UIImage? {
        guard let icon, icon.hasPrefix("data:image/"), let comma = icon.firstIndex(of: ","),
              icon[..<comma].hasSuffix(";base64"),
              let data = Data(base64Encoded: String(icon[icon.index(after: comma)...])) else { return nil }
        return UIImage(data: data)
    }
    var body: some View {
        Group {
            if let bitmap { Image(uiImage: bitmap).resizable().scaledToFit() }
            else { RegionFlag(regionCode: region ?? "").scaleEffect(size / 48).frame(width: size, height: size) }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

struct ReminderSummary: View {
    let reminder: SIMReminder
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label { Text(reminder.nextAt, format: .dateTime) } icon: { Image(systemName: "bell") }
            Text(reminder.repeatDays.map { String(format: detailText("detail.repeatEvery"), $0) } ?? detailText("detail.once"))
            Text(reminder.content).fixedSize(horizontal: false, vertical: true)
        }.font(.subheadline).foregroundStyle(.secondary)
    }
}

struct ProfileSkeletonRows: View {
    var body: some View {
        ForEach(0..<3) { _ in
            HStack(spacing: 12) {
                ProfileAvatar(icon: nil, region: nil)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Example Mobile")
                    Text("8986000000000000000").font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.right").font(.caption.weight(.semibold))
            }
            .detailSkeleton("detail.loadingProfiles")
            .accessibilityIdentifier("profileLoading")
        }
    }
}


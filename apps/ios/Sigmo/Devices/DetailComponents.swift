import SwiftUI

func detailText(_ key: String) -> String { NSLocalizedString(key, comment: "") }

func secureElementTitle(_ id: String, elements: [SecureElement]) -> String {
    guard let index = elements.firstIndex(where: { $0.id == id }) else { return id }
    if elements.count == 1 { return detailText("modem.value.euicc") }
    return "SE \(index + 1)"
}

struct SecureElementMetadata: View {
    let element: SecureElement

    static func storageText(_ bytes: Int64?) -> String {
        bytes.map { "\($0 / 1024)KiB" } ?? "—"
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                eid.fixedSize()
                Spacer(minLength: 0)
                storage.fixedSize()
            }
            VStack(alignment: .leading, spacing: 4) {
                eid.fixedSize(horizontal: false, vertical: true)
                storage
            }
        }
        .font(.caption.monospaced())
        .foregroundStyle(Color.secondary)
        .textCase(nil)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var eid: some View {
        Menu {
            if let eid = element.eid, !eid.isEmpty {
                Button("detail.copy", systemImage: "document.on.document") {
                    UIPasteboard.general.string = eid
                }
            }
        } label: {
            Text(modemValue(element.eid))
                .multilineTextAlignment(.leading)
        }
        .buttonStyle(.plain)
        .disabled(element.eid?.isEmpty != false)
        .accessibilityLabel(Text("detail.eid"))
        .accessibilityValue(modemValue(element.eid))
        .accessibilityIdentifier("seEID-" + element.id)
    }

    private var storage: some View {
        Text(Self.storageText(element.freeSpace))
            .accessibilityLabel(Text("detail.storage"))
            .accessibilityValue(Self.storageText(element.freeSpace))
            .accessibilityIdentifier("seStorage-" + element.id)
    }
}

struct DetailEditRow: View {
    let title: String
    var value = ""
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                LabeledContent {
                    if !value.isEmpty {
                        Text(value).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                    }
                } label: {
                    Text(detailText(title))
                }
                .frame(maxWidth: .infinity)
                Image(systemName: "chevron.right")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .foregroundStyle(.primary)
        .accessibilityLabel(Text(detailText(title)))
        .accessibilityValue(value)
    }
}

struct DetailValue: View {
    let title: String
    let value: String?
    var copyable = false
    var displayValue: String?

    var body: some View {
        LabeledContent {
            Text(modemValue(displayValue ?? value))
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
    func detailErrorAlert(_ failure: Binding<BackendFailure?>) -> some View {
        alert(
            "detail.operationFailed",
            isPresented: Binding(get: { failure.wrappedValue != nil }, set: { if !$0 { failure.wrappedValue = nil } }),
            presenting: failure.wrappedValue
        ) { _ in
            Button("common.ok", role: .cancel) { failure.wrappedValue = nil }
        } message: { failure in
            Text(failure.message)
        }
    }

    func detailErrorAlert(_ failure: BackendFailure?) -> some View {
        modifier(DetailErrorAlert(failure: failure))
    }

    func detailSkeleton(_ label: String) -> some View {
        redacted(reason: .placeholder)
            .disabled(true)
            .allowsHitTesting(false)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(detailText(label)))
    }
}

private struct DetailErrorAlert: ViewModifier {
    let failure: BackendFailure?
    // Dismissing an alert must not clear a read failure that protects unavailable controls.
    @State private var presentedFailure: BackendFailure?
    @State private var lastFailure: BackendFailure?
    @State private var visible = false

    func body(content: Content) -> some View {
        content
            .detailErrorAlert($presentedFailure)
            .onAppear {
                visible = true
                presentFailure()
            }
            .onChange(of: failure) { _, _ in
                if visible { presentFailure() }
            }
            .onDisappear {
                visible = false
                presentedFailure = nil
            }
    }

    private func presentFailure() {
        guard failure != lastFailure else { return }
        lastFailure = failure
        presentedFailure = failure
    }
}

struct ProfileAvatar: View {
    let icon: String?
    let region: String?
    var plmn: String? = nil
    @ScaledMetric(relativeTo: .body) private var size = 32.0
    var body: some View {
        CarrierLogo(icon: icon, plmn: plmn, regionCode: region ?? "", size: size)
    }
}

struct ReminderRow: View {
    let reminder: SIMReminder?
    let action: () -> Void
    @ScaledMetric(relativeTo: .body) private var iconSize = 34.0

    var body: some View {
        if let reminder {
            Button(action: action) {
                HStack(spacing: 12) {
                    Image(systemName: "bell")
                        .font(.body)
                        .foregroundStyle(Color.accentColor)
                        .frame(width: iconSize, height: iconSize)
                        .background(Color.accentColor.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(reminder.content)
                            .font(.body.weight(.medium))
                        Text(scheduleText(reminder))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "chevron.right")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
                .padding(.vertical, 4)
                .contentShape(Rectangle())
            }
            .foregroundStyle(.primary)
            .accessibilityLabel(Text("detail.reminder"))
            .accessibilityValue("\(reminder.content), \(scheduleText(reminder))")
        } else {
            DetailEditRow(title: "detail.addReminder", action: action)
                .accessibilityLabel(Text("detail.reminder"))
                .accessibilityValue(Text("detail.addReminder"))
        }
    }

    private func scheduleText(_ reminder: SIMReminder) -> String {
        let date = reminder.nextAt.formatted(date: .numeric, time: .shortened)
        let repetition = reminder.repeatDays.map { String(format: detailText("detail.repeatEvery"), $0) } ?? detailText("detail.once")
        return "\(date) · \(repetition)"
    }
}

struct ProfileSkeletonRows: View {
    var body: some View {
        ForEach(0..<3) { _ in
            HStack(spacing: 12) {
                ProfileAvatar(icon: nil, region: nil)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Example Mobile").font(.body)
                    Text("8986000000000000000").font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.right").font(.caption.weight(.semibold))
            }
            .detailSkeleton("detail.loadingProfiles")
            .accessibilityIdentifier("profileLoading")
        }
    }
}


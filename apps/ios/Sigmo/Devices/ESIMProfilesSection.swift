import SwiftUI

struct ESIMProfilesSection: View {
    let state: ModemDetailState
    let accent: Color
    let iconTint: Color
    let operation: ESIMProfileOperation?
    let isAddingESIM: Bool
    let preparingElementID: String?
    let selectProfile: (String) -> Void
    let addESIM: @MainActor (String) async -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if !state.elements.isEmpty {
            ForEach(state.elements) { element in
                card(for: element)
            }
        } else if !state.elementsLoaded, state.failure == nil {
            card {
                heading()
                loadingRows
            }
        } else if state.elementsFailure != nil || state.profilesFailure != nil {
            card {
                heading()
                unavailableContent
            }
        }
    }

    private func card(for element: SecureElement) -> some View {
        let profiles = state.groups.first { $0.id == element.id }?.profiles ?? []
        let loading = !state.profilesLoaded && state.failure == nil
        let unavailable = profiles.isEmpty && state.profilesFailure != nil
        let count = state.profilesLoaded && !unavailable ? profiles.count : nil
        let subtitle = state.elements.count > 1 ? secureElementTitle(element.id, elements: state.elements) : nil
        let headerLayout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 0))
            : AnyLayout(HStackLayout(spacing: 8))

        return card {
            headerLayout {
                heading(subtitle: subtitle)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if !loading, !unavailable {
                    addButton(for: element.id)
                }
            }
            SecureElementMetadata(element: element, profileCount: count)
            Divider().padding(.top, 12)

            if loading {
                loadingRows
            } else if unavailable {
                unavailableContent
            } else if profiles.isEmpty, state.profilesLoaded {
                Text("detail.noProfiles")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 12)
            } else {
                ForEach(profiles) { profile in
                    if profile.id != profiles.first?.id { Divider().opacity(0.7) }
                    profileButton(profile)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("esimProfilesCard-" + element.id)
    }

    private func card<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 22)
        return VStack(alignment: .leading, spacing: 0, content: content)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: shape)
            .overlay { shape.strokeBorder(accent.opacity(0.18)).allowsHitTesting(false) }
            .modifier(ModemOverviewCardRow())
    }

    private func heading(subtitle: String? = nil) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "simcard")
                .foregroundStyle(iconTint)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text("detail.profiles").font(.headline).accessibilityAddTraits(.isHeader)
                if let subtitle {
                    Text(verbatim: subtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .frame(minHeight: 44)
    }

    private func addButton(for elementID: String) -> some View {
        AsyncActionButton(id: "addESIM-" + elementID, action: {
            await addESIM(elementID)
        }) {
            HStack(spacing: 5) {
                Image(systemName: "plus").accessibilityHidden(true)
                Text("detail.setupTitle")
            }
            .font(.subheadline)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
        .disabled(!state.canInstall || isAddingESIM)
        .accessibilityValue(preparingElementID == elementID ? detailText("detail.checkingESIM") : "")
    }

    private func profileButton(_ profile: ESIMProfile) -> some View {
        let pendingOperation = operation?.profile.id == profile.id ? operation?.kind : nil
        let hasSchedule = profile.reminder != nil || state.automationSummary(profileID: profile.iccid, seID: profile.seId) != nil
        return Button {
            selectProfile(profile.id)
        } label: {
            ESIMProfileRowLabel(profile: profile, operation: pendingOperation, hasSchedule: hasSchedule)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(pendingOperation != nil || state.loading)
        .accessibilityIdentifier("profileActions-" + profile.id)
    }

    private var loadingRows: some View {
        ForEach(0..<3) { _ in
            ESIMProfileRowLabel(profile: nil)
                .detailSkeleton("detail.loadingProfiles")
                .accessibilityIdentifier("profileLoading")
        }
    }

    private var unavailableContent: some View {
        ContentUnavailableView("detail.profilesUnavailable", systemImage: "simcard")
    }
}

private struct ESIMProfileRowLabel: View {
    let profile: ESIMProfile?
    var operation: ESIMProfileOperation.Kind?
    var hasSchedule = false

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var avatarSize = 26.0

    var body: some View {
        HStack(spacing: 12) {
            CarrierLogo(icon: profile?.icon, plmn: profile?.profileOwner.plmn,
                        regionCode: profile?.regionCode ?? "", size: avatarSize)
            VStack(alignment: .leading, spacing: 5) {
                Text(verbatim: profile?.name ?? "Example Mobile")
                    .font(.body.weight(.semibold))
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
                    .fixedSize(horizontal: false, vertical: true)
                Text(verbatim: profile?.numberLabel ?? "8986000000000000000")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                if dynamicTypeSize.isAccessibilitySize { status }
            }
            Spacer(minLength: 0)
            if !dynamicTypeSize.isAccessibilitySize { status }
            Image(systemName: "chevron.forward")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
        .padding(.vertical, 10)
        .frame(minHeight: 68)
    }

    private var status: some View {
        HStack(spacing: 6) {
            if let operation {
                Text(operation == .enable ? "modem.value.enabling" : "detail.deletingProfile")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                let enabled = profile?.enabled == true
                let color: Color = enabled ? .green : .secondary
                HStack(spacing: 5) {
                    Circle().fill(color).frame(width: 6, height: 6).accessibilityHidden(true)
                    Text(enabled ? "detail.lineOn" : "detail.lineOff")
                }
                .font(.caption.weight(.semibold))
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(color.opacity(0.12), in: Capsule())
            }
            if hasSchedule {
                Image(systemName: "bell").font(.caption).foregroundStyle(.secondary)
                    .accessibilityLabel(Text("detail.lineSchedule"))
            }
        }
    }
}

struct SecureElementMetadata: View {
    let element: SecureElement
    let profileCount: Int?

    static func storageText(_ bytes: Int64?) -> String {
        bytes.map { "\($0 / 1024) KiB" } ?? "—"
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                eid.fixedSize()
                Spacer(minLength: 0)
                summary.fixedSize()
            }
            VStack(alignment: .leading, spacing: 4) {
                eid.fixedSize(horizontal: false, vertical: true)
                summary
            }
        }
        .font(.caption)
        .monospacedDigit()
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var eid: some View {
        let value = element.eid ?? ""
        let suffix = String(value.suffix(4))
        return Menu {
            if !value.isEmpty {
                Button("detail.copy", systemImage: "document.on.document") {
                    UIPasteboard.general.string = value
                }
            }
        } label: {
            Text(verbatim: value.isEmpty ? "EID · —" : "EID · •••• " + suffix)
                .multilineTextAlignment(.leading)
        }
        .buttonStyle(.plain)
        .disabled(value.isEmpty)
        .accessibilityLabel(Text(value.isEmpty ? "detail.eid" : "detail.eidSuffix"))
        .accessibilityValue(value.isEmpty ? Text("common.unknown") : Text(verbatim: suffix))
        .accessibilityIdentifier("seEID-" + element.id)
    }

    private var summary: some View {
        HStack(spacing: 4) {
            if let profileCount {
                Text("detail.profileCount \(profileCount)")
                Text(verbatim: "·").accessibilityHidden(true)
            }
            let storage = Self.storageText(element.freeSpace)
            Text(verbatim: storage)
                .accessibilityLabel(Text("detail.storage"))
                .accessibilityValue(storage)
                .accessibilityIdentifier("seStorage-" + element.id)
        }
    }
}

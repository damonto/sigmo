import SwiftUI

struct ESIMDownloadSummaryView: View {
    let profile: ESIMPreview?
    let deviceName: String
    let elementName: String
    let eid: String?
    let completed: Bool

    var body: some View {
        Form {
            Section {
                if let profile {
                    profileRows(profile)
                }
            } header: {
                // Section headers avoid the rounded clipping applied to grouped rows.
                heading
                    .textCase(nil)
                    .padding(.bottom, 8)
            }

            destinationSection

            if let profile {
                Section {
                    DisclosureGroup("detail.moreInformation") {
                        DetailValue(title: "GID1", value: profile.profileOwner.gid1)
                        DetailValue(title: "GID2", value: profile.profileOwner.gid2)
                    }
                    .accessibilityIdentifier("downloadProfileDetails")
                }
            }
        }
        .formStyle(.grouped)
        .scrollContentBackground(.hidden)
        .frame(maxWidth: 560)
        .frame(maxWidth: .infinity)
    }

    private var heading: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(completed ? "detail.setupDone" : "detail.setupReady")
                .font(.title2.bold())
                .foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            Text(completed ? "detail.downloadCompletedDescription" : "detail.downloadPreviewDescription")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    @ViewBuilder private func profileRows(_ profile: ESIMPreview) -> some View {
        profileIdentity(profile)
        if let nickname = profile.profileNickname, !nickname.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            DetailValue(title: "detail.nickname", value: nickname)
        }
        iccidRow(profile.iccid)
        DetailValue(
            title: "detail.owner",
            value: "MCC \(modemValue(profile.profileOwner.mcc)) · MNC \(modemValue(profile.profileOwner.mnc))"
        )
    }

    private func profileIdentity(_ profile: ESIMPreview) -> some View {
        HStack(spacing: 12) {
            ProfileAvatar(icon: profile.icon, region: profile.regionCode, plmn: profile.profileOwner.plmn)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(profile.profileName.isEmpty ? modemValue(profile.serviceProviderName) : profile.profileName)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("downloadProfileName")
                Text(modemValue(profile.serviceProviderName))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("downloadProfileProvider")
            }
        }
        .padding(.vertical, 4)
        .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
    }

    private func iccidRow(_ iccid: String) -> some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 6) {
                Text("modem.iccid").font(.subheadline).foregroundStyle(.secondary)
                Text(modemValue(iccid))
                    .font(.subheadline.monospaced())
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("downloadProfileICCID")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                UIPasteboard.general.string = iccid
            } label: {
                Label("detail.copyICCID", systemImage: "document.on.document")
                    .labelStyle(.iconOnly)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .disabled(iccid.isEmpty)
            .accessibilityIdentifier("copyDownloadICCID")
        }
    }

    private var destinationSection: some View {
        Section(completed ? "detail.addedTo" : "detail.downloadTo") {
            Label {
                VStack(alignment: .leading, spacing: 4) {
                    Text(deviceName).fontWeight(.medium)
                    Text(elementName).font(.subheadline).foregroundStyle(.secondary)
                        .accessibilityIdentifier("downloadDestinationSE")
                }
            } icon: {
                Image(systemName: "externaldrive").foregroundStyle(.secondary)
            }
            .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            if let eid, !eid.isEmpty {
                DisclosureGroup {
                    Text(eid)
                        .font(.subheadline.monospaced())
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                        .contextMenu {
                            Button("detail.copy", systemImage: "document.on.document") { UIPasteboard.general.string = eid }
                        }
                } label: {
                    LabeledContent("detail.eidSuffix", value: String(eid.suffix(4)))
                }
                .accessibilityIdentifier("downloadEIDDetails")
            }
        }
    }
}

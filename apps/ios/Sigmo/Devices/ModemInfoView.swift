import SwiftUI

struct ModemInfoView: View {
    let state: ModemDetailState
    @State private var presentedFailure: BackendFailure?
    var body: some View {
        Form {
            if let modem = state.modem {
                ModemInfoSections(modem: modem, elements: state.elements, capabilities: state.session.capabilities ?? [])
                if state.esim {
                    Section {
                        NavigationLink { ESIMNotificationsView(session: state.session, modemID: state.modemID, elements: state.elements) } label: {
                            Label("detail.notifications", systemImage: "tray.full")
                        }.accessibilityIdentifier("esimNotifications")
                    }
                }
            } else {
                ContentUnavailableView("devices.unavailable", systemImage: "simcard")
            }
        }
        .environment(\.defaultMinListRowHeight, 44)
        .navigationTitle("common.details")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await reload() }
        .detailErrorAlert($presentedFailure)
        .foregroundTask {
            await reload()
        }
    }

    private func reload() async {
        await state.load()
        guard !Task.isCancelled else { return }
        presentedFailure = state.failure ?? state.elementsFailure
    }
}

struct ModemInfoSections: View {
    let modem: Modem
    let elements: [SecureElement]
    let capabilities: Set<String>

    var body: some View {
        Section("modem.device") {
            DetailValue(title: "modem.id", value: modem.id, copyable: true)
            DetailValue(title: "modem.port", value: modem.primaryPort)
            DetailValue(title: "configuration.name", value: modem.displayName)
            DetailValue(title: "modem.manufacturer", value: modem.manufacturer)
            DetailValue(title: "modem.firmware", value: modem.firmwareRevision)
            DetailValue(title: "modem.hardware", value: modem.hardwareRevision)
        }
        if modem.simKind == "euicc" {
            ForEach(elements) { element in
                Section(secureElementTitle(element.id, elements: elements)) {
                    DetailValue(title: "detail.eid", value: element.eid, copyable: true)
                    DetailValue(title: "detail.storage", value: element.freeSpace.map { ByteCountFormatter.string(fromByteCount: $0, countStyle: .binary) })
                    DetailValue(title: "detail.accreditation", value: element.sasUp?.name)
                    DetailValue(title: "modem.region", value: element.sasUp?.region)
                    LabeledContent("detail.certificates") {
                        Text(element.certificates?.joined(separator: "\n").nilIfEmpty ?? modemValue(nil))
                            .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        }
        SIMInfoSections(modem: modem, capabilities: capabilities)
        Section("detail.features") { DetailValue(title: "modem.simKind", value: modem.simKind) }
    }
}

struct SIMInfoSections: View {
    let modem: Modem
    let capabilities: Set<String>

    var body: some View {
        Section("modem.sim") {
            if !modem.number.isEmpty { DetailValue(title: "modem.number", value: modem.number, copyable: true) }
            DetailValue(title: "modem.carrier", value: modem.sim.operatorName)
            DetailValue(title: "modem.operatorCode", value: modem.sim.operatorIdentifier)
            DetailValue(title: "modem.region", value: modem.sim.regionCode)
            DetailValue(title: "modem.iccid", value: modem.sim.identifier, copyable: true)
            DetailValue(title: "modem.active", value: detailText(modem.sim.active ? "common.yes" : "common.no"))
            if modem.slots?.count != 1 {
                DetailValue(title: "modem.activeSlot", value: modem.sim.slot.formatted())
            }
            DetailValue(title: "modem.lock", value: modem.unlockRequired)
        }
        Section("modem.network") {
            if modem.airplaneMode {
                Label("modem.airplane", systemImage: "airplane")
            } else {
                DetailValue(title: "modem.technology", value: modem.accessTechnology)
                DetailValue(title: "modem.registration", value: modem.registrationState)
                DetailValue(title: "modem.carrier", value: modem.registeredOperator.name)
                DetailValue(title: "modem.operatorCode", value: modem.registeredOperator.code)
                DetailValue(title: "modem.signal", value: "\(modem.signalPercent)%")
            }
            DetailValue(title: "modem.data", value: modem.internetConnected.map { detailText($0 ? "common.connected" : "common.disconnected") })
            if capabilities.contains("wifiCalling") {
                DetailValue(title: "capability.wifiCalling", value: modem.wifiCallingStatus.displayName)
                    .accessibilityIdentifier("modemWiFiCallingStatus")
            }
            if capabilities.contains("volte") {
                DetailValue(title: "capability.volte", value: modem.volteStatus.displayName)
                    .accessibilityIdentifier("modemVoLTEStatus")
            }
        }
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

struct ESIMProfileInfoView: View {
    let state: ModemDetailState
    let profileID: String
    var body: some View {
        if state.session.canAccessDevices, let profile = state.groups.flatMap(\.profiles).first(where: { $0.id == profileID }) {
            ESIMProfileInfoContent(profile: profile)
        } else {
            ContentUnavailableView("devices.unavailable", systemImage: "simcard")
        }
    }
}

private struct ESIMProfileInfoContent: View {
    let profile: ESIMProfile
    var body: some View {
        Form {
            Section {
                DetailValue(title: "configuration.name", value: profile.name)
                DetailValue(title: "detail.provider", value: profile.serviceProviderName)
                DetailValue(title: "detail.profileName", value: profile.profileName)
                DetailValue(title: "detail.nickname", value: profile.profileNickname)
                DetailValue(title: "modem.iccid", value: profile.iccid, copyable: true)
                DetailValue(title: "detail.isdp", value: profile.isdPAID)
                DetailValue(title: "detail.profileState", value: detailText(profile.enabled ? "detail.lineOn" : "detail.lineOff"))
                DetailValue(title: "detail.profileClass", value: profile.profileClass)
                DetailValue(title: "detail.eid", value: profile.seEid, copyable: true)
            }
            Section("detail.owner") {
                DetailValue(title: "MCC", value: profile.profileOwner.mcc)
                DetailValue(title: "MNC", value: profile.profileOwner.mnc)
                DetailValue(title: "GID1", value: profile.profileOwner.gid1)
                DetailValue(title: "GID2", value: profile.profileOwner.gid2)
            }
        }.navigationTitle("detail.profileDetails").navigationBarTitleDisplayMode(.inline)
    }
}

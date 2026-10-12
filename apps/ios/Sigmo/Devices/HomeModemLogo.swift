import SwiftUI

struct HomeModemLogo: View {
    let session: BackendSession
    let modem: Modem
    private struct LoadIdentity: Equatable {
        let modem: ModemIconIdentity
        let revision: Int
    }

    var body: some View {
        CarrierLogo(icon: session.modemIcons.icon(for: modem),
                    plmn: session.modemIcons.isPending(modem) ? nil : modem.sim.operatorIdentifier,
                    regionCode: modem.sim.regionCode, size: 40)
            .task(id: LoadIdentity(modem: ModemIconIdentity(modem), revision: session.modemIcons.revision)) {
                await session.modemIcons.load(modem, session: session)
            }
    }
}

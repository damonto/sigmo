import SwiftUI

struct ReminderDestinationView: View {
    let session: BackendSession
    let profileID: String
    let profileType: String
    let seID: String
    @State private var state: ModemDetailState
    @State private var loaded = false
    @State private var resolvedProfileID: String?

    init(session: BackendSession, modemID: String, profileID: String, profileType: String, seID: String) {
        self.session = session
        self.profileID = profileID
        self.profileType = profileType
        self.seID = seID
        _state = State(initialValue: ModemDetailState(session: session, modemID: modemID))
    }

    var body: some View {
        SettingsAccessView(session: session) {
            Group {
                // Deleting the profile must not destroy the page that owns the
                // operation and presents the subsequent history assignment.
                if let resolvedProfileID {
                    ESIMProfileActionsView(state: state, profileID: resolvedProfileID)
                } else if !loaded { ProgressView() }
                else if state.failure != nil || state.profilesFailure != nil {
                    ContentUnavailableView("push.resourceUnavailable", systemImage: "simcard")
                }
                else if profileType == "psim", state.modem?.sim.identifier == profileID {
                    DetailReminderForm(state: state, profile: nil)
                } else {
                    ContentUnavailableView("push.resourceUnavailable", systemImage: "simcard", description: Text("push.lineUnavailable"))
                }
            }
            .detailErrorAlert(state.failure ?? state.profilesFailure)
            .foregroundTask(id: session.hasLoadedModems) {
                // Detail refreshes update devices already in the session inventory.
                guard session.hasLoadedModems else { return }
                await state.load()
                guard !Task.isCancelled else { return }
                if resolvedProfileID == nil, profileType == "esim", state.currentProfile(seID + ":" + profileID) != nil {
                    resolvedProfileID = seID + ":" + profileID
                }
                loaded = true
            }
        }
        .navigationTitle("push.generic.reminder")
        .onChange(of: session.accessRevision) { _, _ in
            state.clear()
            loaded = false
            resolvedProfileID = nil
        }
    }
}

import SwiftUI

struct VoLTEView: View {
    private enum Action {
        case enabled(Bool)
        case dataPath(VoLTEDataPath)
    }

    @State private var state: VoLTEState
    @State private var action: ConnectionAction<Action>?

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: VoLTEState(detail: detail, profileID: profileID))
    }

    var body: some View {
        Form {
            ConnectionRadioNotice(scope: state.scope)
            Section {
                let settings = state.settings
                IMSConnectionStatusCard(access: .volte,
                                        status: settings?.status ?? (state.loadFailure == nil ? .loading : .unknown),
                                        enabled: settings?.enabled == true, progress: settings?.progress,
                                        seconds: settings?.durationSeconds, observedAt: state.observedAt,
                                        detail: settings.map { dataPathName($0.dataPath) })
            }
            if let settings = state.settings {
                Section {
                    Toggle(isOn: Binding(
                        get: { settings.enabled }, set: { action = .init(value: .enabled($0)) }
                    )) {
                        InlineProgressLabel(isWorking: changingEnabled) { Text("connection.enableVoLTE") }
                    }
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("volteEnabled")
                } header: {
                    Text("connection.callSettings")
                } footer: {
                    Text("connection.volteHint")
                }
                dataPathSection(settings)
            }
        }
        .navigationTitle("capability.volte")
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil },
                                      observe: { await state.observePending() }))
        .task(id: action?.id) { [action] in
            if let action { await perform(action) }
        }
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }

    private func dataPathSection(_ settings: VoLTESettings) -> some View {
        Section {
            if settings.dataPath == "mbim" {
                LabeledContent("connection.dataPath", value: "MBIM")
                    .accessibilityIdentifier("volteFixedDataPath")
            } else {
                Picker("connection.dataPath", selection: Binding(
                    get: { settings.dataPath }, set: { selection in
                        if let path = VoLTEDataPath(rawValue: selection) { action = .init(value: .dataPath(path)) }
                    }
                )) {
                    ForEach(VoLTEDataPath.allCases, id: \.self) { path in
                        Text(path.title).tag(path.rawValue)
                    }
                    if !settings.knownDataPath { Text(settings.dataPath).tag(settings.dataPath) }
                }
                .pickerStyle(.menu)
                .disabled(!state.canChangeDataPath || action != nil)
                .accessibilityIdentifier("volteDataPath")
                if case .dataPath = action?.value { ConnectionProgress() }
            }
        } header: {
            Text("connection.networkConfiguration")
        } footer: {
            if settings.dataPath == "mbim" {
                Text("connection.mbimHint")
            } else if settings.enabled {
                Text("connection.dataPathHint")
            } else if let path = VoLTEDataPath(rawValue: settings.dataPath) {
                Text(detailText(path.explanation))
            }
        }
    }

    private func dataPathName(_ path: String) -> String {
        path == "mbim" ? "MBIM" : VoLTEDataPath(rawValue: path)?.title ?? path
    }

    private var changingEnabled: Bool {
        if case .enabled = action?.value { return true }
        return false
    }

    private func perform(_ action: ConnectionAction<Action>) async {
        guard self.action?.id == action.id, !Task.isCancelled else { return }
        switch action.value {
        case .enabled(let enabled): await state.setEnabled(enabled)
        case .dataPath(let path): await state.selectDataPath(path)
        }
        if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
    }
}

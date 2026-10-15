import SwiftUI

struct VoLTEView: View {
    @State private var state: VoLTEState
    @State private var action: ConnectionAction<Bool>?

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: VoLTEState(detail: detail, profileID: profileID))
    }

    var body: some View {
        Form {
            ConnectionRadioNotice(scope: state.scope)
            Section {
                if let settings = state.settings {
                    Toggle("connection.enableVoLTE", isOn: Binding(
                        get: { settings.enabled }, set: { action = .init(value: $0) }
                    ))
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("volteEnabled")
                } else { ConnectionInitialValue(loading: state.loadFailure == nil) }
                if state.updating { ConnectionProgress() }
            } footer: { Text("connection.volteHint") }
            if let settings = state.settings {
                Section {
                    DetailValue(title: "connection.status", value: detailText(settings.statusKey))
                        .accessibilityIdentifier("volteStatus")
                    ConnectionDurationRow(seconds: settings.modemManaged ? nil : settings.durationSeconds,
                                          observedAt: state.observedAt, running: settings.enabled && settings.connected)
                }
                Section {
                    NavigationLink {
                        VoLTEDataPathView(state: state)
                    } label: {
                        LabeledContent("connection.dataPath", value: dataPathName(settings.dataPath))
                    }
                    .accessibilityIdentifier("volteDataPath")
                }
            }
        }
        .navigationTitle("capability.volte")
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil },
                                      observe: { await state.observePending() }))
        .task(id: action?.id) {
            guard let action else { return }
            await state.setEnabled(action.value)
            if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
        }
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }
}

private func dataPathName(_ path: String) -> String {
    path == "mbim" ? "MBIM" : VoLTEDataPath(rawValue: path)?.title ?? path
}

private struct VoLTEDataPathView: View {
    @Bindable var state: VoLTEState
    @State private var action: ConnectionAction<VoLTEDataPath>?

    var body: some View {
        Form {
            if let settings = state.settings {
                if settings.dataPath == "mbim" {
                    Section {
                        LabeledContent("connection.dataPath", value: "MBIM")
                            .accessibilityIdentifier("volteFixedDataPath")
                    } footer: { Text("connection.mbimHint") }
                } else {
                    Section {
                        ForEach(VoLTEDataPath.allCases, id: \.self) { path in
                            Button { action = .init(value: path) } label: {
                                HStack(spacing: 12) {
                                    VStack(alignment: .leading, spacing: 4) {
                                        Text(path.title).foregroundStyle(.primary)
                                        Text(detailText(path.explanation)).font(.footnote).foregroundStyle(.secondary)
                                    }
                                    Spacer(minLength: 8)
                                    if settings.dataPath == path.rawValue { Image(systemName: "checkmark").foregroundStyle(.tint) }
                                }
                            }
                            .disabled(!state.canChangeDataPath || action != nil)
                            .accessibilityIdentifier("voltePath-" + path.rawValue)
                            .accessibilityAddTraits(settings.dataPath == path.rawValue ? .isSelected : [])
                        }
                    } footer: { Text("connection.dataPathHint") }
                }
                if state.updating { Section { ConnectionProgress() } }
            }
        }
        .navigationTitle("connection.dataPath")
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil },
                                      observe: { await state.observePending() }))
        .task(id: action?.id) {
            guard let action else { return }
            await state.selectDataPath(action.value)
            if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
        }
        .detailErrorAlert($state.failure)
    }
}

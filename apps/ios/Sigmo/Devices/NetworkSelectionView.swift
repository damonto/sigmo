import SwiftUI

struct NetworkSelectionView: View {
    private enum Action { case automatic(Bool), select(NetworkOperator), scan }
    @State private var state: NetworkSelectionState
    @State private var action: ConnectionAction<Action>?

    init(detail: ModemDetailState, profileID: String?) {
        _state = State(initialValue: NetworkSelectionState(detail: detail, profileID: profileID))
    }

    var body: some View {
        Form {
            ConnectionRadioNotice(scope: state.scope)
            Section {
                if state.registration?.automatic == nil {
                    ConnectionInitialValue(title: "connection.automatic", loading: state.registration == nil && state.loadFailure == nil)
                } else {
                    Toggle(isOn: Binding(
                        get: { !state.manual },
                        set: { action = .init(value: .automatic($0)) }
                    )) {
                        InlineProgressLabel(isWorking: changingAutomatic && state.updating) { Text("connection.automatic") }
                    }
                    .disabled(!state.canChange || action != nil)
                    .accessibilityIdentifier("networkAutomatic")
                }
            } footer: {
                if state.manual { Text("connection.manualHint") }
            }
            if state.manual {
                Section {
                    if state.networks.isEmpty {
                        if !state.scanning, state.scanFailure == nil {
                            Text("connection.noNetworks").foregroundStyle(.secondary)
                        }
                    } else {
                        ForEach(state.networks) { network in
                            Button { action = .init(value: .select(network)) } label: {
                                HStack(spacing: 12) {
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(network.name).foregroundStyle(.primary)
                                        if network.status == "Forbidden" {
                                            Text("connection.networkUnavailable").font(.caption).foregroundStyle(.secondary)
                                        } else if state.networks.contains(where: { $0.name == network.name && $0.id != network.id }) {
                                            Text(network.operatorCode).font(.caption).foregroundStyle(.secondary)
                                        }
                                    }
                                    Spacer(minLength: 8)
                                    if state.pendingOperator == network.id {
                                        ProgressView()
                                    } else if state.selectedOperator == network.id {
                                        Image(systemName: "checkmark").foregroundStyle(.tint)
                                    }
                                }
                            }
                            .disabled(!state.canChange || action != nil || state.scanning)
                            .accessibilityIdentifier("networkOperator-" + network.id)
                            .accessibilityAddTraits(state.selectedOperator == network.id ? .isSelected : [])
                        }
                    }
                    if state.scanning || !state.networks.isEmpty || state.scanFailure == nil {
                        scanButton
                    }
                }
            }
        }
        .navigationTitle("connection.networkSelection")
        .modifier(ConnectionLifecycle(scope: state.scope,
                                      reload: { await state.reload() }, cancelAction: { action = nil; state.leave() }))
        .task(id: action?.id) { await perform() }
        .refreshable { await state.reload() }
        .detailErrorAlert($state.failure)
    }

    private var changingAutomatic: Bool {
        if case .automatic = action?.value { return true }
        return false
    }

    private var scanButton: some View {
        LoadingButton(isWorking: state.scanning, progressValue: Text("connection.searchingNetworks")) {
            action = .init(value: .scan)
        } label: {
            Text("connection.searchAgain")
        }
        .disabled(!state.canChange || action != nil)
        .accessibilityIdentifier("rescanNetworks")
    }

    private func perform() async {
        guard let action else { return }
        switch action.value {
        case .automatic(let enabled): await state.setAutomatic(enabled)
        case .select(let network): await state.select(network)
        case .scan: await state.scan()
        }
        if self.action?.id == action.id, !Task.isCancelled { self.action = nil }
    }
}

import SwiftUI
import Observation

@MainActor @Observable
final class ESIMNotificationsState {
    let session: BackendSession
    let modemID: String
    private(set) var groups: [ESIMNotificationGroup] = []
    private(set) var busy = false
    private(set) var loaded = false
    private(set) var failure: BackendFailure?
    private(set) var message: String?
    @ObservationIgnored private var revision = 0
    init(session: BackendSession, modemID: String) { self.session = session; self.modemID = modemID }
    func clear() { revision += 1; groups = []; failure = nil; message = nil; busy = false; loaded = false }
    func load() async {
        guard !busy, !Task.isCancelled else { return }
        busy = true
        let current = revision, id = modemID
        defer { if current == revision { busy = false } }
        do {
            let result = try await session.protectedRequest { api, token in try await api.esimNotifications(id, token: token) }
            if current == revision, !Task.isCancelled { groups = result.ses; failure = nil; loaded = true }
        } catch { if current == revision, !Task.isCancelled { failure = .wrapping(error); loaded = true } }
    }
    func perform(_ notification: ESIMNotification, delete: Bool) async {
        guard !busy, !Task.isCancelled else { return }
        busy = true
        let current = revision, id = modemID
        failure = nil
        message = nil
        defer { if current == revision { busy = false } }
        do {
            try await session.protectedRequest { api, token in
                var path = ["ses", notification.seId, "notifications", notification.sequenceNumber]
                if !delete { path.append("deliveries") }
                try await api.detailMutation(id, path: path, method: delete ? "DELETE" : "POST", token: token)
            }
            guard current == revision, !Task.isCancelled else { return }
            busy = false
            await load()
            guard current == revision, !Task.isCancelled else { return }
            message = detailText("detail.saved")
        } catch { if current == revision { busy = false; if !Task.isCancelled { failure = .wrapping(error) } } }
    }
}

struct ESIMNotificationsView: View {
    let session: BackendSession
    let elements: [SecureElement]

    private enum Action { case resend, delete }
    private struct PendingAction: Identifiable {
        let id = UUID()
        let notification: ESIMNotification
        let action: Action
    }

    @State private var state: ESIMNotificationsState
    @State private var deleting: ESIMNotification?
    @State private var pendingAction: PendingAction?
    @State private var presentedFailure: BackendFailure?
    init(session: BackendSession, modemID: String, elements: [SecureElement]) {
        self.session = session
        self.elements = elements
        _state = State(initialValue: ESIMNotificationsState(session: session, modemID: modemID))
    }
    var body: some View {
        List {
            if !session.canAccessDevices {
                ContentUnavailableView("devices.unavailable", systemImage: "tray", description: Text("devices.accessRequired"))
            } else if !state.loaded {
                Section {
                    ForEach(0..<3) { _ in
                        HStack(spacing: 12) {
                            Image(systemName: "arrow.down.circle").font(.title2)
                            VStack(alignment: .leading, spacing: 6) {
                                Text("#123  8986000000000000000").font(.subheadline)
                                HStack {
                                    Text("detail.notificationInstall").font(.caption)
                                    Text("smdp.example").font(.footnote)
                                }
                            }
                        }.detailSkeleton("detail.loadingNotifications")
                    }
                }
            } else if state.groups.allSatisfy({ $0.notifications.isEmpty }) {
                if state.failure == nil {
                    ContentUnavailableView("detail.noNotifications", systemImage: "tray")
                } else {
                    ContentUnavailableView("detail.notificationsUnavailable", systemImage: "tray")
                }
            }
            ForEach(state.groups.filter { !$0.notifications.isEmpty }) { group in
                Section {
                    ForEach(group.notifications) { notification in
                        ESIMNotificationRow(notification: notification)
                            .accessibilityIdentifier("esimNotification-" + notification.id)
                            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                // The row stays visible until deletion is confirmed and succeeds.
                                Button { deleting = notification } label: { Image(systemName: "trash") }
                                    .tint(.red)
                                    .accessibilityLabel(Text("common.delete"))
                                    .accessibilityIdentifier("deleteNotification-" + notification.id)
                                Button { begin(notification, action: .resend) } label: {
                                    Image(systemName: "arrow.clockwise")
                                }
                                .tint(.blue)
                                .accessibilityLabel(Text("detail.resend"))
                                .accessibilityIdentifier("resendNotification-" + notification.id)
                            }
                    }
                } header: {
                    if elements.count > 1 || state.groups.count > 1 {
                        Text(secureElementTitle(group.id, elements: elements))
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.defaultMinListRowHeight, 44)
        .disabled(state.busy || pendingAction != nil)
        .navigationTitle("detail.notifications").navigationBarTitleDisplayMode(.inline)
        .refreshable { await reload() }
        .detailErrorAlert($presentedFailure)
        .foregroundTask { await reload() }
        .task(id: pendingAction?.id) {
            guard let pendingAction else { return }
            await state.perform(pendingAction.notification, delete: pendingAction.action == .delete)
            guard self.pendingAction?.id == pendingAction.id, !Task.isCancelled else { return }
            presentedFailure = state.failure
            self.pendingAction = nil
        }
        .alert("detail.deleteNotification", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { notification in
            Button("common.delete", role: .destructive) { begin(notification, action: .delete); deleting = nil }.accessibilityIdentifier("confirmNotificationDeletion")
            Button("common.cancel", role: .cancel) { deleting = nil }
        } message: { notification in
            Text(String(format: detailText("detail.deleteNotificationMessage"), notification.sequenceNumber))
        }
        .onChange(of: session.accessRevision) { _, _ in state.clear(); pendingAction = nil; deleting = nil; presentedFailure = nil }
        .onChange(of: session.canAccessDevices) { _, value in if !value { state.clear(); pendingAction = nil; deleting = nil } }
        .onDisappear { pendingAction = nil; deleting = nil }
    }

    private func begin(_ notification: ESIMNotification, action: Action) {
        guard !state.busy, pendingAction == nil else { return }
        pendingAction = PendingAction(notification: notification, action: action)
    }

    private func reload() async {
        await state.load()
        guard !Task.isCancelled else { return }
        presentedFailure = state.failure
    }
}

private struct ESIMNotificationRow: View {
    let notification: ESIMNotification
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var iconSize = 26.0

    private var operation: (title: String, symbol: String, color: Color) {
        switch notification.operation {
        case "install": (detailText("detail.notificationInstall"), "arrow.down.circle", .blue)
        case "enable": (detailText("detail.notificationEnable"), "checkmark.circle", .green)
        case "disable": (detailText("detail.notificationDisable"), "pause.circle", .orange)
        case "delete": (detailText("detail.notificationDelete"), "trash", .red)
        default: (modemValue(notification.operation), "bell", .secondary)
        }
    }

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: operation.symbol)
                .font(.system(size: iconSize)).foregroundStyle(operation.color)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 6) {
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        sequence
                        iccid
                    }.fixedSize()
                    VStack(alignment: .leading, spacing: 4) {
                        sequence
                        iccid
                    }
                }
                if dynamicTypeSize.isAccessibilitySize {
                    VStack(alignment: .leading, spacing: 6) {
                        operationTag
                        smdp
                    }
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        operationTag.fixedSize()
                        smdp
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
        .padding(.vertical, 4)
        .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
        .alignmentGuide(.listRowSeparatorTrailing) { $0[.trailing] }
        .textSelection(.enabled)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(String(
            format: detailText("detail.notificationAccessibility"),
            notification.sequenceNumber, operation.title,
            modemValue(notification.iccid), modemValue(notification.smdp)
        )))
    }

    private var sequence: some View {
        Text("#" + notification.sequenceNumber)
            .font(.subheadline.weight(.semibold)).monospacedDigit()
            .fixedSize(horizontal: false, vertical: true)
    }

    private var iccid: some View {
        Text(modemValue(notification.iccid))
            .font(.subheadline.monospaced())
            .fixedSize(horizontal: false, vertical: true)
    }

    private var smdp: some View {
        Text(modemValue(notification.smdp))
            .font(.footnote).foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }

    private var operationTag: some View {
        Text(operation.title)
            .font(.caption.weight(.medium)).foregroundStyle(operation.color)
            .padding(.horizontal, 7).padding(.vertical, 2)
            .background(operation.color.opacity(0.12), in: .capsule)
            .fixedSize(horizontal: false, vertical: true)
    }
}

import SwiftUI

extension SIMAutomationSummary {
    var actionTitle: String { detailText(actionType.label) }

    var nextRunText: String? {
        nextAt?.formatted(date: .numeric, time: .shortened)
    }
}

struct SIMAutomationRow: View {
    let summary: SIMAutomationSummary?
    let action: () -> Void

    var body: some View {
        if let summary {
            let schedule = summary.nextRunText ?? detailText("automation.unscheduled")
            ScheduledLineRow(title: summary.actionTitle, subtitle: schedule,
                             systemImage: "clock", action: action)
                .accessibilityLabel(Text("automation.title"))
                .accessibilityValue("\(summary.actionTitle), \(schedule)")
        } else {
            DetailEditRow(title: "automation.add", action: action)
                .accessibilityLabel(Text("automation.title"))
                .accessibilityValue(Text("automation.add"))
        }
    }
}

struct AutomationDraft {
    enum Unit: String, CaseIterable {
        case KiB, MiB
        var multiplier: Int { self == .KiB ? 1024 : 1_048_576 }
    }

    var kind: SIMAutomationKind = .data
    var date = Date().addingTimeInterval(3600)
    var repeatDays = ""
    var amount = ""
    var unit: Unit = .MiB
    var to = ""
    var text = ""
    var duration = "10"
    var wifiCallingOnly = false

    init(task: SIMAutomationTask? = nil) {
        guard let task else { return }
        date = task.nextAt.flatMap { $0 > Date() ? $0 : nil } ?? date
        repeatDays = task.repeatDays.map(String.init) ?? ""
        kind = task.action.type
        to = task.action.to ?? ""
        text = task.action.text ?? ""
        duration = task.action.durationSeconds.map(String.init) ?? duration
        wifiCallingOnly = task.action.wifiCallingOnly ?? false
        if let bytes = task.action.bytes {
            unit = bytes.isMultiple(of: Unit.MiB.multiplier) ? .MiB : .KiB
            amount = String(bytes / unit.multiplier)
        }
    }

    var schedule: SIMAutomationSchedule? {
        guard date > Date() else { return nil }
        let repeatValue = Int(repeatDays)
        guard repeatDays.isEmpty || repeatValue.map({ (1...3650).contains($0) }) == true else { return nil }
        let recipient = to.trimmingCharacters(in: .whitespacesAndNewlines)
        let action: SIMAutomationAction
        switch kind {
        case .data:
            let formatter = NumberFormatter()
            formatter.numberStyle = .decimal
            guard let number = formatter.number(from: amount)?.doubleValue else { return nil }
            let bytes = number * Double(unit.multiplier)
            guard let count = Int(exactly: bytes), (1024...104_857_600).contains(count), count.isMultiple(of: 1024) else { return nil }
            action = SIMAutomationAction(type: kind, bytes: count)
        case .sms:
            guard !recipient.isEmpty, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
            action = SIMAutomationAction(type: kind, to: recipient, text: text, wifiCallingOnly: wifiCallingOnly)
        case .call:
            guard !recipient.isEmpty, let seconds = Int(duration), (1...300).contains(seconds) else { return nil }
            action = SIMAutomationAction(type: kind, to: recipient, durationSeconds: seconds, wifiCallingOnly: wifiCallingOnly)
        }
        return SIMAutomationSchedule(scheduledAt: date, repeatDays: repeatValue, action: action)
    }
}

struct SIMAutomationForm: View {
    let state: ModemDetailState
    let line: SIMAutomationLine
    @State private var draft = AutomationDraft()
    @State private var target: ModemSIMTarget
    @State private var task: SIMAutomationTask?
    @State private var loaded = false
    @State private var loading = true
    @State private var busy = false
    @State private var reloadID = 0
    @State private var needsRefresh = false
    @State private var confirmClear = false
    @State private var clearing = false
    @State private var failure: BackendFailure?
    @Environment(\.dismiss) private var dismiss

    init(state: ModemDetailState, line: SIMAutomationLine) {
        self.state = state
        self.line = line
        _target = State(initialValue: state.simTarget)
    }

    private var locked: Bool { task?.running == true || task?.restoring == true }

    var body: some View {
        Form {
            if loaded {
                fields.disabled(busy || locked || needsRefresh)
                if let run = task?.lastRun {
                    Section { runDetails(run) }
                }
                if locked || needsRefresh {
                    Section {
                        if locked { Text(task?.restoring == true ? "automation.restoring" : "automation.running") }
                        Button("automation.refresh") { reloadID += 1 }
                    }
                }
                if task != nil {
                    Section {
                        Button("automation.clear", role: .destructive) { confirmClear = true }
                            .disabled(busy || locked || needsRefresh)
                    }
                }
            } else if loading {
                ProgressView("automation.loading")
            } else {
                Button("automation.refresh") { reloadID += 1 }
            }
        }
        .navigationTitle("automation.title")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("common.cancel") { dismiss() }.disabled(busy)
            }
            ToolbarItem(placement: .confirmationAction) {
                AsyncActionButton(title: "automation.save", id: "saveSIMAutomation") {
                    guard let schedule = draft.schedule else { return }
                    let line = line
                    let id = task?.id
                    await change { api, token in
                        try await api.saveAutomation(line: line, id: id, schedule: schedule, token: token)
                    }
                }
                .disabled(!loaded || draft.schedule == nil || busy || locked || needsRefresh)
            }
        }
        .alert("automation.deletePrompt", isPresented: $confirmClear) {
            Button("automation.clear", role: .destructive) { clearing = true }
            Button("common.cancel", role: .cancel) {}
        }
        .task(id: reloadID) { await load() }
        .task(id: clearing) {
            guard clearing, let id = task?.id else { return }
            defer { clearing = false }
            let profileID = line.profileID
            await change { api, token in
                try await api.deleteAutomation(profileID: profileID, id: id, token: token)
            }
        }
        .onChange(of: state.simTarget) { _, current in
            if line.seID == nil, current != target { dismiss() }
        }
        .onChange(of: state.session.accessRevision) { _, _ in dismiss() }
        .interactiveDismissDisabled(busy)
        .detailErrorAlert($failure)
    }

    @ViewBuilder private var fields: some View {
        Section {
            Text(line.label).font(.headline)
            Picker("automation.action", selection: $draft.kind) {
                ForEach(SIMAutomationKind.allCases) { kind in Text(detailText(kind.label)).tag(kind) }
            }
            DatePicker("automation.time", selection: $draft.date)
            TextField("automation.repeat", text: $draft.repeatDays).keyboardType(.numberPad)
        } footer: {
            Text("automation.repeatHint")
        }
        switch draft.kind {
        case .data:
            Section {
                TextField("automation.amount", text: $draft.amount).keyboardType(.decimalPad)
                Picker("automation.unit", selection: $draft.unit) {
                    ForEach(AutomationDraft.Unit.allCases, id: \.self) { unit in Text(unit.rawValue).tag(unit) }
                }
                .accessibilityIdentifier("automationDataUnit")
            } footer: { Text("automation.dataHint") }
        case .sms:
            Section {
                recipient
                TextField("automation.message", text: $draft.text, axis: .vertical).lineLimit(3...8)
            }
        case .call:
            Section {
                recipient
                TextField("automation.duration", text: $draft.duration).keyboardType(.numberPad)
            } footer: { Text("automation.callHint") }
        }
        if draft.kind != .data {
            Section {
                Toggle("automation.wifiCallingOnly", isOn: $draft.wifiCallingOnly)
                    .accessibilityIdentifier("automationWiFiCallingOnly")
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("automation.wifiCallingOnlyHint")
                        .accessibilityIdentifier("automationWiFiCallingOnlyHint")
                    if draft.kind == .sms {
                        Text("automation.wifiCallingOnlySmsHint")
                    }
                }
            }
        }
    }

    private var recipient: some View {
        TextField("automation.number", text: $draft.to).keyboardType(.phonePad)
    }

    @ViewBuilder private func runDetails(_ run: SIMAutomationTask.Run) -> some View {
        Text(String(format: detailText("automation.last"), run.finishedAt.formatted(date: .abbreviated, time: .shortened),
                    result(code: run.code, fallback: run.status)))
        if let bytes = run.bytesReceived {
            Text(String(format: detailText("automation.received"), bytes.formatted()))
        }
        if let restoration = run.restoreCode { Text(result(code: restoration, fallback: "failed")) }
    }

    private func result(code: String?, fallback: String) -> String {
        if let code {
            let key = "automation.result." + code
            let value = detailText(key)
            if value != key { return value }
        }
        return detailText("automation." + fallback)
    }

    private func load() async {
        loaded = false
        loading = true
        defer { if !Task.isCancelled { loading = false } }
        failure = nil
        let line = line
        do {
            let value = try await state.session.protectedRequest { api, token in
                try await api.automation(line: line, token: token)
            }
            guard !Task.isCancelled else { return }
            task = value
            draft = AutomationDraft(task: value)
            needsRefresh = false
            loaded = true
        } catch {
            if !Task.isCancelled { failure = .wrapping(error) }
        }
    }

    private func change(_ operation: @escaping @Sendable (BackendAPI, String?) async throws -> Void) async {
        guard loaded, !busy, !locked, !needsRefresh else { return }
        busy = true
        defer { busy = false }
        do {
            let target = target
            let isPhysical = line.seID == nil
            let detail = state
            try await state.session.protectedRequest { api, token in
                if isPhysical, await detail.simTarget != target { throw CancellationError() }
                try await operation(api, token)
            }
            await state.refreshAutomationSummaries()
            if !Task.isCancelled { dismiss() }
        } catch {
            if !Task.isCancelled, !(error is CancellationError) {
                let problem = BackendFailure.wrapping(error)
                needsRefresh = problem.status == 409
                failure = problem
            }
        }
    }
}

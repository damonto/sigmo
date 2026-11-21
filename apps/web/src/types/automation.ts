/** The action determines which fields are sent to the automation service. */
export type AutomationAction =
  | { type: 'data'; bytes: number }
  | { type: 'sms'; to: string; text: string }
  | { type: 'call'; to: string; durationSeconds: number }

/** A schedule is submitted as an ISO timestamp and an optional interval in days. */
export interface AutomationSchedule {
  scheduledAt: string
  repeatDays: number | null
  action: AutomationAction
}

/** A line is selected from live modem or eSIM details. */
export interface AutomationLine {
  profileId: string
  modemId: string
  seId?: string
  label: string
}

/** The last run can carry a separate eSIM restoration result. */
export interface AutomationRun {
  finishedAt: string
  status: 'succeeded' | 'failed' | 'unconfirmed' | 'interrupted'
  code?: string
  restoreCode?: string
  bytesReceived?: number
}

/** Task identity stays bound to the modem on which it was created. */
export interface SimAutomation extends AutomationSchedule {
  id: string
  modemId: string
  seId?: string
  nextAt: string | null
  running: boolean
  restoring: boolean
  lastRun: AutomationRun | null
}

/** Configured action and its next run, if scheduled. */
export interface ScheduledAutomation {
  id: string
  profileId: string
  seId?: string
  actionType: AutomationAction['type']
  nextAt: string | null
}

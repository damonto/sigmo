export type Reminder = {
  nextAt: string
  repeatDays?: number | null
  content: string
}

export type ReminderPayload = {
  scheduledAt: string
  repeatDays?: number | null
  content: string
}

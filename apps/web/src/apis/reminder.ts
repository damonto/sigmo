import { fetchJson } from '@/lib/fetch'

import type { Reminder, ReminderPayload } from '@/types/reminder'

export const useReminderApi = () => {
  const savePhysicalReminder = (modemId: string, iccid: string, payload: ReminderPayload) =>
    fetchJson<Reminder>(`modems/${modemId}/sims/${encodeURIComponent(iccid)}/reminder`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })

  const deletePhysicalReminder = (modemId: string, iccid: string) =>
    fetchJson<void>(`modems/${modemId}/sims/${encodeURIComponent(iccid)}/reminder`, {
      method: 'DELETE',
    })

  const saveEsimReminder = (
    modemId: string,
    seId: string,
    iccid: string,
    payload: ReminderPayload,
  ) =>
    fetchJson<Reminder>(
      `modems/${modemId}/ses/${encodeURIComponent(seId)}/esims/${encodeURIComponent(iccid)}/reminder`,
      {
        method: 'PUT',
        body: JSON.stringify(payload),
      },
    )

  const deleteEsimReminder = (modemId: string, seId: string, iccid: string) =>
    fetchJson<void>(
      `modems/${modemId}/ses/${encodeURIComponent(seId)}/esims/${encodeURIComponent(iccid)}/reminder`,
      { method: 'DELETE' },
    )

  return {
    savePhysicalReminder,
    deletePhysicalReminder,
    saveEsimReminder,
    deleteEsimReminder,
  }
}

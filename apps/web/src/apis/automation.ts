import { fetchJson } from '@/lib/fetch'
import type {
  AutomationLine,
  AutomationSchedule,
  ScheduledAutomation,
  SimAutomation,
} from '@/types/automation'

const collection = (profileId: string) =>
  `sim-profiles/${encodeURIComponent(profileId)}/automations`

const value = async <T>(path: string, options: RequestInit): Promise<T> => {
  const { data } = await fetchJson<T>(path, options, true)
  if (data.value === undefined) throw new Error('Empty automation response')
  return data.value
}

/** Uses authenticated, quiet requests so task content stays out of fetch logs. */
export const automationApi = {
  list: (profileId: string, signal: AbortSignal) =>
    value<{ items: SimAutomation[] }>(collection(profileId), { signal }),
  scheduled: (modemId: string, signal: AbortSignal) =>
    value<{ items: ScheduledAutomation[] }>(`modems/${encodeURIComponent(modemId)}/automations`, {
      signal,
    }),
  save: (line: AutomationLine, schedule: AutomationSchedule, id?: string) =>
    value<SimAutomation>(
      id ? `${collection(line.profileId)}/${encodeURIComponent(id)}` : collection(line.profileId),
      {
        method: id ? 'PUT' : 'POST',
        body: JSON.stringify(
          id ? schedule : { ...schedule, modemId: line.modemId, seId: line.seId },
        ),
      },
    ),
  remove: (profileId: string, id: string) =>
    fetchJson<void>(
      `${collection(profileId)}/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
      true,
    ),
}

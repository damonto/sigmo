import { fetchJson, fetchJsonQuietly } from '@/lib/fetch'

import type { UpdateSettings, UpdateSnapshot } from '@/types/update'

export const useUpdateApi = () => ({
  settings: () => fetchJson<UpdateSnapshot>('settings/updates'),
  saveSettings: (settings: UpdateSettings) =>
    fetchJson<UpdateSnapshot>('settings/updates', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
  check: () => fetchJson<UpdateSnapshot>('update-checks', { method: 'POST' }),
  install: (signal?: AbortSignal) =>
    fetchJson<UpdateSnapshot>('update-installations', { method: 'POST', signal }),
  installation: async (signal?: AbortSignal) => {
    const snapshot = await fetchJsonQuietly<UpdateSnapshot>('update-installations/current', {
      cache: 'no-store',
      signal,
    })
    if (
      !snapshot ||
      !['idle', 'checking', 'downloading', 'verifying', 'restarting', 'failed'].includes(
        snapshot.state,
      )
    ) {
      throw new Error('invalid update installation response')
    }
    return snapshot
  },
})

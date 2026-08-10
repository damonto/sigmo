import { fetchJson } from '@/lib/fetch'

import type { UpdateSettings, UpdateSnapshot } from '@/types/update'

export const useUpdateApi = () => ({
  settings: () => fetchJson<UpdateSnapshot>('settings/updates'),
  saveSettings: (settings: UpdateSettings) =>
    fetchJson<UpdateSnapshot>('settings/updates', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
  check: () => fetchJson<UpdateSnapshot>('update-checks', { method: 'POST' }),
  install: () => fetchJson<UpdateSnapshot>('update-installations', { method: 'POST' }),
  installation: () => fetchJson<UpdateSnapshot>('update-installations/current'),
})

import { fetchJson } from '@/lib/fetch'

import type { UpdateVoLTESettingsRequest, VoLTESettingsResponse } from '@/types/volte'

export const useVoLTEApi = () => {
  const settings = (id: string) => {
    return fetchJson<VoLTESettingsResponse>(`modems/${id}/volte/settings`)
  }

  const updateSettings = (id: string, payload: UpdateVoLTESettingsRequest) => {
    return fetchJson<void>(`modems/${id}/volte/settings`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  return { settings, updateSettings }
}

import { fetchJson } from '@/lib/fetch'

import type { ConfigResponse, ConfigValues } from '@/types/config'

export const useConfigApi = () => {
  const getConfig = () => {
    return fetchJson<ConfigResponse>('config')
  }

  const updateConfig = (payload: ConfigValues) => {
    return fetchJson<ConfigResponse>('config', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  return {
    getConfig,
    updateConfig,
  }
}

import { useFetch } from '@/lib/fetch'

import type { ConfigResponse, ConfigValues } from '@/types/config'

export const useConfigApi = () => {
  const getConfig = () => {
    return useFetch<ConfigResponse>('config').get().json()
  }

  const updateConfig = (payload: ConfigValues) => {
    return useFetch<ConfigResponse>('config', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).json()
  }

  return {
    getConfig,
    updateConfig,
  }
}

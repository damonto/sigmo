import { fetchJson } from '@/lib/fetch'

import type { AppInfoResponse } from '@/types/app'

export const useAppApi = () => {
  const getAppInfo = () => {
    return fetchJson<AppInfoResponse>('app')
  }

  return {
    getAppInfo,
  }
}

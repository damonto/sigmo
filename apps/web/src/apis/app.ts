import { fetchJson } from '@/lib/fetch'

import type { AppInfoResponse } from '@/types/app'

const rawBaseURL = import.meta.env.VITE_API_BASE_URL
const baseURL = rawBaseURL?.trim() ? rawBaseURL.replace(/\/+$/, '') : '/api/v1'

export const useAppApi = () => {
  const getAppInfo = () => {
    return fetchJson<AppInfoResponse>('app')
  }

  const ready = async () => {
    const response = await fetch(`${baseURL}/app`, {
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    })
    return response.ok
  }

  return {
    getAppInfo,
    ready,
  }
}

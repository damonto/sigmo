import { fetchJson } from '@/lib/fetch'

import { parseInternetTrafficResponse } from '@/types/internet'
import type {
  ConnectInternetPayload,
  InternetConnectionPreferencesPayload,
  InternetConnectionResponse,
  InternetPublicResponse,
} from '@/types/internet'

export const useInternetApi = () => {
  const getCurrentConnection = (id: string) => {
    return fetchJson<InternetConnectionResponse>(`modems/${id}/internet-connections/current`)
  }

  const getPublic = (id: string) => {
    return fetchJson<InternetPublicResponse>(`modems/${id}/internet-connections/public`)
  }

  const getTraffic = async (id: string, signal: AbortSignal) => {
    const { data } = await fetchJson<unknown>(
      `modems/${encodeURIComponent(id)}/internet-connections/current/traffic`,
      { signal, cache: 'no-store' },
      true,
    )
    return parseInternetTrafficResponse(data.value)
  }

  const connect = (id: string, payload: ConnectInternetPayload) => {
    return fetchJson<InternetConnectionResponse>(`modems/${id}/internet-connections`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  const disconnect = (id: string) => {
    return fetchJson<void>(`modems/${id}/internet-connections/current`, {
      method: 'DELETE',
    })
  }

  const updatePreferences = (id: string, payload: InternetConnectionPreferencesPayload) => {
    return fetchJson<InternetConnectionResponse>(
      `modems/${id}/internet-connections/current/preferences`,
      {
        method: 'PUT',
        body: JSON.stringify(payload),
      },
    )
  }

  return {
    getCurrentConnection,
    getPublic,
    getTraffic,
    connect,
    disconnect,
    updatePreferences,
  }
}

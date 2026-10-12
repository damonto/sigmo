import { fetchJson } from '@/lib/fetch'

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
    connect,
    disconnect,
    updatePreferences,
  }
}

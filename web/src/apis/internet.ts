import { fetchJson } from '@/lib/fetch'

import type {
  ConnectInternetPayload,
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

  return {
    getCurrentConnection,
    getPublic,
    connect,
    disconnect,
  }
}

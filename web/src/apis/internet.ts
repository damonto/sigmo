import { useFetch } from '@/lib/fetch'

import type {
  ConnectInternetPayload,
  InternetConnectionResponse,
  InternetPublicResponse,
} from '@/types/internet'

export const useInternetApi = () => {
  const getCurrentConnection = (id: string) => {
    return useFetch<InternetConnectionResponse>(`modems/${id}/internet-connections/current`)
      .get()
      .json()
  }

  const getPublic = (id: string) => {
    return useFetch<InternetPublicResponse>(`modems/${id}/internet-connections/public`)
      .get()
      .json()
  }

  const connect = (id: string, payload: ConnectInternetPayload) => {
    return useFetch<InternetConnectionResponse>(`modems/${id}/internet-connections`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }).json()
  }

  const disconnect = (id: string) => {
    return useFetch<void>(`modems/${id}/internet-connections/current`, {
      method: 'DELETE',
    }).json()
  }

  return {
    getCurrentConnection,
    getPublic,
    connect,
    disconnect,
  }
}

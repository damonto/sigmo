import { fetchJson } from '@/lib/fetch'

import {
  parseInternetPublicResponse,
  parseInternetSpeedTestResponse,
  parseInternetTrafficResponse,
} from '@/types/internet'
import type {
  ConnectInternetPayload,
  InternetConnectionPreferencesPayload,
  InternetConnectionResponse,
  InternetSpeedTestSize,
} from '@/types/internet'

export const useInternetApi = () => {
  const getCurrentConnection = (id: string) => {
    return fetchJson<InternetConnectionResponse>(`modems/${id}/internet-connections/current`)
  }

  const getPublic = async (id: string, signal: AbortSignal) => {
    const { data } = await fetchJson<unknown>(
      `modems/${encodeURIComponent(id)}/internet-connections/public`,
      { signal },
      true,
    )
    return parseInternetPublicResponse(data.value)
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

  const getLatestSpeedTest = async (id: string, signal: AbortSignal) => {
    const { data } = await fetchJson<unknown>(
      `modems/${encodeURIComponent(id)}/internet-connections/speed-tests/latest`,
      { signal },
      true,
    )
    return parseInternetSpeedTestResponse(data.value)
  }

  const runSpeedTest = async (
    id: string,
    iccid: string,
    sizeMB: InternetSpeedTestSize,
    signal: AbortSignal,
  ) => {
    const { data } = await fetchJson<unknown>(
      `modems/${encodeURIComponent(id)}/internet-connections/speed-tests`,
      { method: 'POST', body: JSON.stringify({ iccid, sizeMB }), signal },
      true,
    )
    return parseInternetSpeedTestResponse(data.value)
  }

  return {
    getCurrentConnection,
    getPublic,
    getTraffic,
    connect,
    disconnect,
    updatePreferences,
    getLatestSpeedTest,
    runSpeedTest,
  }
}

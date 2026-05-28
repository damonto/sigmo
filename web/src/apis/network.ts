import { fetchJson } from '@/lib/fetch'

import type {
  BandsResponse,
  ModesResponse,
  NetworksResponse,
  SetCurrentBandsRequest,
  SetCurrentModesRequest,
} from '@/types/network'

export const useNetworkApi = () => {
  const scanNetworks = (id: string) => {
    return fetchJson<NetworksResponse>(`modems/${id}/networks`)
  }

  const registerNetwork = (id: string, operatorCode: string) => {
    const encoded = encodeURIComponent(operatorCode)
    return fetchJson<void>(`modems/${id}/networks/${encoded}`, {
      method: 'PUT',
    })
  }

  const getModes = (id: string) => {
    return fetchJson<ModesResponse>(`modems/${id}/networks/modes`)
  }

  const setCurrentModes = (id: string, payload: SetCurrentModesRequest) => {
    return fetchJson<void>(`modems/${id}/networks/current-modes`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  const getBands = (id: string) => {
    return fetchJson<BandsResponse>(`modems/${id}/networks/bands`)
  }

  const setCurrentBands = (id: string, payload: SetCurrentBandsRequest) => {
    return fetchJson<void>(`modems/${id}/networks/current-bands`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  return {
    scanNetworks,
    registerNetwork,
    getModes,
    setCurrentModes,
    getBands,
    setCurrentBands,
  }
}

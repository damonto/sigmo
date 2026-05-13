import { useFetch } from '@/lib/fetch'

import type {
  BandsResponse,
  CellInfoListResponse,
  ModesResponse,
  NetworksResponse,
  SetCurrentBandsRequest,
  SetCurrentModesRequest,
} from '@/types/network'

export const useNetworkApi = () => {
  const scanNetworks = (id: string) => {
    return useFetch<NetworksResponse>(`modems/${id}/networks`).get().json()
  }

  const registerNetwork = (id: string, operatorCode: string) => {
    const encoded = encodeURIComponent(operatorCode)
    return useFetch<void>(`modems/${id}/networks/${encoded}`, {
      method: 'PUT',
    }).json()
  }

  const getModes = (id: string) => {
    return useFetch<ModesResponse>(`modems/${id}/networks/modes`).get().json()
  }

  const setCurrentModes = (id: string, payload: SetCurrentModesRequest) => {
    return useFetch<void>(`modems/${id}/networks/current-modes`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).json()
  }

  const getBands = (id: string) => {
    return useFetch<BandsResponse>(`modems/${id}/networks/bands`).get().json()
  }

  const setCurrentBands = (id: string, payload: SetCurrentBandsRequest) => {
    return useFetch<void>(`modems/${id}/networks/current-bands`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).json()
  }

  const getCells = (id: string) => {
    return useFetch<CellInfoListResponse>(`modems/${id}/networks/cells`).get().json()
  }

  return {
    scanNetworks,
    registerNetwork,
    getModes,
    setCurrentModes,
    getBands,
    setCurrentBands,
    getCells,
  }
}

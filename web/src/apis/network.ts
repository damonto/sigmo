import { fetchJson } from '@/lib/fetch'

import type {
  AirplaneModeResponse,
  BandsResponse,
  ModesResponse,
  NetworksResponse,
  SetAirplaneModeRequest,
  SetCurrentBandsRequest,
  SetCurrentModesRequest,
  SetVoLTERequest,
  VoLTEResponse,
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

  const getAirplaneMode = (id: string) => {
    return fetchJson<AirplaneModeResponse>(`modems/${id}/networks/airplane-mode`)
  }

  const setAirplaneMode = (id: string, payload: SetAirplaneModeRequest) => {
    return fetchJson<void>(`modems/${id}/networks/airplane-mode`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  const getVoLTE = (id: string) => {
    return fetchJson<VoLTEResponse>(`modems/${id}/networks/volte`)
  }

  const setVoLTE = (id: string, payload: SetVoLTERequest) => {
    return fetchJson<void>(`modems/${id}/networks/volte`, {
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
    getAirplaneMode,
    setAirplaneMode,
    getVoLTE,
    setVoLTE,
  }
}

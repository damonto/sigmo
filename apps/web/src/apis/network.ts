import { fetchJson } from '@/lib/fetch'

import type {
  AirplaneModeResponse,
  BandsResponse,
  ModesResponse,
  NetworkScanResponse,
  NetworksResponse,
  RegistrationResponse,
  SetAirplaneModeRequest,
  SetCurrentBandsRequest,
  SetCurrentModesRequest,
  SetRegistrationRequest,
} from '@/types/network'

export const useNetworkApi = () => {
  const scanNetworks = (id: string) => {
    return fetchJson<NetworksResponse>(`modems/${id}/networks`)
  }

  const startNetworkScan = (id: string) => {
    return fetchJson<NetworkScanResponse>(`modems/${id}/network-scans`, {
      method: 'POST',
    })
  }

  const getNetworkScan = (id: string, scanID: string) => {
    return fetchJson<NetworkScanResponse>(
      `modems/${id}/network-scans/${encodeURIComponent(scanID)}`,
    )
  }

  const getRegistration = (id: string) => {
    return fetchJson<RegistrationResponse>(`modems/${id}/networks/registration`)
  }

  const setRegistration = (id: string, payload: SetRegistrationRequest) => {
    return fetchJson<void>(`modems/${id}/networks/registration`, {
      method: 'PUT',
      body: JSON.stringify(payload),
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

  return {
    scanNetworks,
    startNetworkScan,
    getNetworkScan,
    getRegistration,
    setRegistration,
    getModes,
    setCurrentModes,
    getBands,
    setCurrentBands,
    getAirplaneMode,
    setAirplaneMode,
  }
}

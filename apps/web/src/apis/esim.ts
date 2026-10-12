import { fetchJson } from '@/lib/fetch'

import type {
  EsimDiscoverResponse,
  EsimProfilesResponse,
  EsimTransferProfile,
  EsimTransferSourcesResponse,
} from '@/types/esim'

export const useEsimApi = () => {
  const getEsims = (id: string) => {
    return fetchJson<EsimProfilesResponse>(`modems/${id}/esims`)
  }

  const discoverEsims = (id: string, seId: string) => {
    return fetchJson<EsimDiscoverResponse>(`modems/${id}/ses/${seId}/esim-discoveries`, {
      method: 'POST',
    })
  }

  const updateEsimNickname = (id: string, seId: string, iccid: string, nickname: string) => {
    return fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}/nickname`, {
      method: 'PUT',
      body: JSON.stringify({ nickname }),
    })
  }

  const enableEsim = (id: string, seId: string, iccid: string) => {
    return fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}/activation`, {
      method: 'PUT',
    })
  }

  const deleteEsim = (id: string, seId: string, iccid: string) => {
    return fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}`, {
      method: 'DELETE',
    })
  }

  const getTransferSources = (id: string) => {
    return fetchJson<EsimTransferSourcesResponse>(`modems/${id}/esim-transfers/sources`)
  }

  const getTransferProfiles = (
    id: string,
    payload: { sourceType: string; sourceId: string; sourceImei?: string },
  ) => {
    return fetchJson<EsimTransferProfile[]>(`modems/${id}/esim-transfers/source-profiles`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  return {
    getEsims,
    discoverEsims,
    updateEsimNickname,
    enableEsim,
    deleteEsim,
    getTransferSources,
    getTransferProfiles,
  }
}

import { fetchJson } from '@/lib/fetch'
import { invalidateCommunicationDirectory } from '@/apis/communication'

import type {
  EsimDiscoverResponse,
  EsimProfilesResponse,
  EsimTransferProfile,
  EsimTransferSourcesResponse,
} from '@/types/esim'

export const useEsimApi = () => {
  const getEsims = async (id: string) => {
    const result = await fetchJson<EsimProfilesResponse>(`modems/${id}/esims`)
    invalidateCommunicationDirectory()
    return result
  }

  const discoverEsims = (id: string, seId: string) => {
    return fetchJson<EsimDiscoverResponse>(`modems/${id}/ses/${seId}/esim-discoveries`, {
      method: 'POST',
    })
  }

  const updateEsimNickname = async (id: string, seId: string, iccid: string, nickname: string) => {
    const result = await fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}/nickname`, {
      method: 'PUT',
      body: JSON.stringify({ nickname }),
    })
    invalidateCommunicationDirectory()
    return result
  }

  const enableEsim = async (id: string, seId: string, iccid: string) => {
    const result = await fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}/activation`, {
      method: 'PUT',
    })
    invalidateCommunicationDirectory()
    return result
  }

  const deleteEsim = async (id: string, seId: string, iccid: string) => {
    const result = await fetchJson<void>(`modems/${id}/ses/${seId}/esims/${iccid}`, {
      method: 'DELETE',
    })
    invalidateCommunicationDirectory()
    return result
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

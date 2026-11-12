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
    const result = await fetchJson<EsimProfilesResponse>(`modems/${id}/esim-profiles`)
    invalidateCommunicationDirectory()
    return result
  }

  const discoverEsims = (id: string, seId: string) => {
    return fetchJson<EsimDiscoverResponse>(
      `modems/${id}/secure-elements/${seId}/esim-discoveries`,
      {
        method: 'POST',
      },
    )
  }

  const updateEsimNickname = async (id: string, seId: string, iccid: string, nickname: string) => {
    const result = await fetchJson<void>(
      `modems/${id}/secure-elements/${seId}/esim-profiles/${iccid}/nickname`,
      {
        method: 'PUT',
        body: JSON.stringify({ nickname }),
      },
    )
    invalidateCommunicationDirectory()
    return result
  }

  const enableEsim = async (id: string, seId: string, iccid: string) => {
    const result = await fetchJson<void>(
      `modems/${id}/secure-elements/${seId}/active-esim-profile`,
      {
        method: 'PUT',
        body: JSON.stringify({ iccid }),
      },
    )
    invalidateCommunicationDirectory()
    return result
  }

  const deleteEsim = async (id: string, seId: string, iccid: string) => {
    const result = await fetchJson<void>(
      `modems/${id}/secure-elements/${seId}/esim-profiles/${iccid}`,
      {
        method: 'DELETE',
      },
    )
    invalidateCommunicationDirectory()
    return result
  }

  // The active dialog owns these read errors; abandoned reads must not show global notifications.
  const getTransferSources = (id: string) => {
    return fetchJson<EsimTransferSourcesResponse>(
      `modems/${id}/esim-transfers/sources`,
      undefined,
      true,
    )
  }

  const getTransferProfiles = (
    id: string,
    payload: { sourceType: string; sourceId: string; sourceImei?: string },
  ) => {
    return fetchJson<EsimTransferProfile[]>(
      `modems/${id}/esim-transfers/source-profiles`,
      { method: 'POST', body: JSON.stringify(payload) },
      true,
    )
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

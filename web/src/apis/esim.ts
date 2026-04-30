import { useFetch } from '@/lib/fetch'

import type { EsimDiscoverResponse, EsimProfilesResponse } from '@/types/esim'

export const useEsimApi = () => {
  const getEsims = (id: string) => {
    return useFetch<EsimProfilesResponse>(`modems/${id}/esims`).get().json()
  }

  const discoverEsims = (id: string) => {
    return useFetch<EsimDiscoverResponse>(`modems/${id}/esims/discover`).get().json()
  }

  const updateEsimNickname = (id: string, iccid: string, nickname: string) => {
    return useFetch<void>(`modems/${id}/esims/${iccid}/nickname`, {
      method: 'PUT',
      body: JSON.stringify({ nickname }),
    }).json()
  }

  const enableEsim = (id: string, iccid: string) => {
    return useFetch<void>(`modems/${id}/esims/${iccid}/activation`, {
      method: 'PUT',
    }).json()
  }

  const deleteEsim = (id: string, iccid: string) => {
    return useFetch<void>(`modems/${id}/esims/${iccid}`, {
      method: 'DELETE',
    }).json()
  }

  return {
    getEsims,
    discoverEsims,
    updateEsimNickname,
    enableEsim,
    deleteEsim,
  }
}

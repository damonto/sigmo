import { fetchJson } from '@/lib/fetch'

import type { EuiccDetailResponse } from '@/types/euicc'

export const useEuiccApi = () => {
  const getEuicc = (id: string) => {
    return fetchJson<EuiccDetailResponse>(`modems/${id}/euicc`)
  }

  return {
    getEuicc,
  }
}

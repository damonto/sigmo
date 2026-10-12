import { fetchJson } from '@/lib/fetch'

import type { SEsResponse } from '@/types/se'

export const useSEApi = () => {
  const getSEs = (id: string) => {
    return fetchJson<SEsResponse>(`modems/${id}/ses`)
  }

  return {
    getSEs,
  }
}

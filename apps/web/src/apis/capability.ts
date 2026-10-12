import { fetchJson } from '@/lib/fetch'

import type { CapabilitiesResponse } from '@/types/capability'

export const useCapabilityApi = () => {
  const getCapabilities = () => {
    return fetchJson<CapabilitiesResponse>('capabilities')
  }

  return {
    getCapabilities,
  }
}

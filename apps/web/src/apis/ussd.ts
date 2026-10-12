import { fetchJson } from '@/lib/fetch'

import type { UssdAction, UssdExecuteResponse } from '@/types/ussd'

export const useUssdApi = () => {
  const executeUssd = async (id: string, action: UssdAction, code: string) => {
    return fetchJson<UssdExecuteResponse>(`modems/${id}/ussd`, {
      method: 'POST',
      body: JSON.stringify({ action, code }),
    })
  }

  return {
    executeUssd,
  }
}

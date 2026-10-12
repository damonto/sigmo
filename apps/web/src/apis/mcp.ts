import { fetchJson, useFetch } from '@/lib/fetch'

import type {
  CreateMCPAPIKey,
  CreateMCPAPIKeyResponse,
  MCPAPIKeysResponse,
  MCPAuditEventsResponse,
  MCPSettings,
} from '@/types/mcp'

export const useMCPApi = () => {
  const getSettings = () => fetchJson<MCPSettings>('settings/mcp')

  const updateSettings = (enabled: boolean) =>
    fetchJson<MCPSettings>('settings/mcp', {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    })

  const getAPIKeys = () => fetchJson<MCPAPIKeysResponse>('settings/mcp/api-keys')

  const createAPIKey = (payload: CreateMCPAPIKey) =>
    fetchJson<CreateMCPAPIKeyResponse>('settings/mcp/api-keys', {
      method: 'POST',
      body: JSON.stringify(payload),
    })

  const revokeAPIKey = (id: string) =>
    fetchJson<void>(`settings/mcp/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })

  const getAuditEvents = () =>
    fetchJson<MCPAuditEventsResponse>('settings/mcp/audit-events?limit=50')

  const downloadSkill = async () => {
    const request = useFetch<Blob>(
      'settings/mcp/skills/sigmo-control',
      { method: 'GET' },
      { immediate: false },
    ).blob()
    await request.execute(true)
    return request.data.value
  }

  return {
    createAPIKey,
    downloadSkill,
    getAPIKeys,
    getAuditEvents,
    getSettings,
    revokeAPIKey,
    updateSettings,
  }
}

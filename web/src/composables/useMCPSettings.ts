import { computed, ref } from 'vue'

import { useMCPApi } from '@/apis/mcp'
import { useModemApi } from '@/apis/modem'
import type { CreateMCPAPIKey, MCPAPIKey, MCPAuditEvent, MCPSettings } from '@/types/mcp'
import type { Modem } from '@/types/modem'

export const useMCPSettings = () => {
  const api = useMCPApi()
  const modemApi = useModemApi()
  const settings = ref<MCPSettings | null>(null)
  const apiKeys = ref<MCPAPIKey[]>([])
  const auditEvents = ref<MCPAuditEvent[]>([])
  const modems = ref<Modem[]>([])
  const isLoading = ref(false)
  const isSaving = ref(false)
  const isCreating = ref(false)
  const revokingId = ref('')

  const permissionGroups = computed(() => {
    const groups = new Map<string, string[]>()
    for (const permission of settings.value?.permissions ?? []) {
      const values = groups.get(permission.module) ?? []
      values.push(permission.name)
      groups.set(permission.module, values)
    }
    return [...groups.entries()].map(([module, permissions]) => ({ module, permissions }))
  })

  const load = async () => {
    if (isLoading.value) return
    isLoading.value = true
    try {
      const [settingsResult, keysResult, auditResult, modemsResult] = await Promise.all([
        api.getSettings(),
        api.getAPIKeys(),
        api.getAuditEvents(),
        modemApi.getModems(),
      ])
      settings.value = settingsResult.data.value ?? null
      apiKeys.value = keysResult.data.value?.apiKeys ?? []
      auditEvents.value = auditResult.data.value?.events ?? []
      modems.value = modemsResult.data.value ?? []
    } finally {
      isLoading.value = false
    }
  }

  const setEnabled = async (enabled: boolean) => {
    if (isSaving.value) return false
    isSaving.value = true
    try {
      const { data } = await api.updateSettings(enabled)
      if (!data.value) return false
      settings.value = data.value
      return true
    } finally {
      isSaving.value = false
    }
  }

  const createAPIKey = async (payload: CreateMCPAPIKey) => {
    if (isCreating.value) return null
    isCreating.value = true
    try {
      const { data } = await api.createAPIKey(payload)
      if (!data.value) return null
      apiKeys.value = [data.value.apiKey, ...apiKeys.value]
      return data.value
    } finally {
      isCreating.value = false
    }
  }

  const revokeAPIKey = async (id: string) => {
    if (revokingId.value) return false
    revokingId.value = id
    try {
      await api.revokeAPIKey(id)
      const key = apiKeys.value.find((value) => value.id === id)
      if (key) key.status = 'revoked'
      return true
    } finally {
      revokingId.value = ''
    }
  }

  return {
    apiKeys,
    auditEvents,
    createAPIKey,
    downloadSkill: api.downloadSkill,
    isCreating,
    isLoading,
    isSaving,
    load,
    modems,
    permissionGroups,
    revokeAPIKey,
    revokingId,
    setEnabled,
    settings,
  }
}

<script setup lang="ts">
import { ShieldAlert } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import MCPAPIKeysCard from '@/components/settings/mcp/MCPAPIKeysCard.vue'
import MCPAuditCard from '@/components/settings/mcp/MCPAuditCard.vue'
import MCPCreateKeyDialog from '@/components/settings/mcp/MCPCreateKeyDialog.vue'
import MCPSecretDialog from '@/components/settings/mcp/MCPSecretDialog.vue'
import MCPServiceCard from '@/components/settings/mcp/MCPServiceCard.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Spinner } from '@/components/ui/spinner'
import { useMCPSettings } from '@/composables/useMCPSettings'
import type { CreateMCPAPIKey, MCPAPIKey } from '@/types/mcp'

const { t } = useI18n()
const {
  apiKeys,
  auditEvents,
  createAPIKey,
  downloadSkill,
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
} = useMCPSettings()

const createOpen = ref(false)
const secretOpen = ref(false)
const secret = ref('')
const createdName = ref('')

const endpointURL = computed(() => {
  if (!settings.value || typeof window === 'undefined')
    return settings.value?.endpointPath ?? '/mcp'
  return new URL(settings.value.endpointPath, window.location.origin).toString()
})

const handleToggle = async (enabled: boolean) => {
  if (await setEnabled(enabled)) toast.success(t('settings.mcp.saved'))
}
const handleCreate = async (payload: CreateMCPAPIKey) => {
  const result = await createAPIKey(payload)
  if (!result) return
  secret.value = result.token
  createdName.value = result.apiKey.name
  createOpen.value = false
  secretOpen.value = true
}
const handleRevoke = async (apiKey: MCPAPIKey) => {
  if (await revokeAPIKey(apiKey.id)) toast.success(t('settings.mcp.revoked'))
}
const handleSecretOpen = (open: boolean) => {
  secretOpen.value = open
  if (!open) secret.value = ''
}
const handleDownload = async () => {
  const blob = await downloadSkill()
  if (!blob) return
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'sigmo-control.zip'
  link.click()
  URL.revokeObjectURL(url)
}

onMounted(() => void load())
</script>

<template>
  <div class="space-y-4 pb-8">
    <SettingsHeader :title="t('settings.mcp.title')" :description="t('settings.mcp.description')" />

    <div v-if="isLoading" class="flex items-center justify-center py-24">
      <Spinner class="size-6 text-muted-foreground" />
    </div>

    <template v-else-if="settings">
      <Alert>
        <ShieldAlert />
        <AlertTitle>{{ t('settings.mcp.securityTitle') }}</AlertTitle>
        <AlertDescription>{{ t('settings.mcp.securityDescription') }}</AlertDescription>
      </Alert>

      <MCPServiceCard
        :enabled="settings.enabled"
        :endpoint-url="endpointURL"
        :is-saving="isSaving"
        @toggle="handleToggle"
        @download="handleDownload"
      />
      <MCPAPIKeysCard
        :api-keys="apiKeys"
        :revoking-id="revokingId"
        @create="createOpen = true"
        @revoke="handleRevoke"
      />
      <MCPAuditCard :events="auditEvents" :retention-days="settings.auditRetentionDays" />
    </template>

    <MCPCreateKeyDialog
      v-model:open="createOpen"
      :is-creating="isCreating"
      :modems="modems"
      :permission-groups="permissionGroups"
      @create="handleCreate"
    />
    <MCPSecretDialog
      :open="secretOpen"
      :endpoint-url="endpointURL"
      :key-name="createdName"
      :secret="secret"
      @update:open="handleSecretOpen"
    />
  </div>
</template>

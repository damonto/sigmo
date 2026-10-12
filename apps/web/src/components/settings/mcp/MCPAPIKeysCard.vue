<script setup lang="ts">
import { Plus, Trash2 } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'
import type { MCPAPIKey } from '@/types/mcp'

const props = defineProps<{
  apiKeys: MCPAPIKey[]
  revokingId: string
}>()

const emit = defineEmits<{
  create: []
  revoke: [apiKey: MCPAPIKey]
}>()

const { t, locale } = useI18n()
const revokeTarget = ref<MCPAPIKey | null>(null)

const permissionLabel = (permission: string) =>
  t(`settings.mcp.permissionLabels.${permission.split('.').join('_')}`)
const formatTime = (value: string) =>
  new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
const statusVariant = (status: MCPAPIKey['status']) =>
  status === 'active' ? 'default' : status === 'revoked' ? 'destructive' : 'secondary'

const confirmRevoke = () => {
  if (!revokeTarget.value) return
  emit('revoke', revokeTarget.value)
  revokeTarget.value = null
}
</script>

<template>
  <Card>
    <CardHeader>
      <div>
        <CardTitle>{{ t('settings.mcp.keysTitle') }}</CardTitle>
        <CardDescription>{{ t('settings.mcp.keysDescription') }}</CardDescription>
      </div>
      <Button size="sm" class="w-fit" data-testid="open-mcp-key-form" @click="emit('create')">
        <Plus class="size-4" />
        {{ t('settings.mcp.createKey') }}
      </Button>
    </CardHeader>
    <CardContent class="space-y-3">
      <p v-if="props.apiKeys.length === 0" class="py-6 text-center text-sm text-muted-foreground">
        {{ t('settings.mcp.noKeys') }}
      </p>
      <div v-for="key in props.apiKeys" :key="key.id" class="space-y-2 rounded-lg border p-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <p class="font-medium">{{ key.name }}</p>
            <p class="font-mono text-xs text-muted-foreground">{{ key.tokenHint }}</p>
          </div>
          <div class="flex items-center gap-2">
            <Badge :variant="statusVariant(key.status)">
              {{ t(`settings.mcp.status.${key.status}`) }}
            </Badge>
            <Button
              v-if="key.status === 'active'"
              data-testid="revoke-mcp-key"
              :aria-label="t('settings.mcp.revoke')"
              variant="ghost"
              size="icon"
              :disabled="props.revokingId !== ''"
              @click="revokeTarget = key"
            >
              <Spinner v-if="props.revokingId === key.id" class="size-4" />
              <Trash2 v-else class="size-4" />
            </Button>
          </div>
        </div>
        <p class="text-xs text-muted-foreground">
          {{ key.allModems ? t('settings.mcp.allModems') : key.modemIds.join(', ') }} ·
          {{ t('settings.mcp.expiresAt', { value: formatTime(key.expiresAt) }) }}
        </p>
        <div class="flex flex-wrap gap-1">
          <Badge v-for="permission in key.permissions" :key="permission" variant="outline">
            {{ permissionLabel(permission) }}
          </Badge>
        </div>
      </div>
    </CardContent>
  </Card>

  <AlertDialog :open="revokeTarget !== null" @update:open="!$event && (revokeTarget = null)">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ t('settings.mcp.revokeTitle') }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ t('settings.mcp.revokeDescription', { name: revokeTarget?.name }) }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{{ t('settings.cancel') }}</AlertDialogCancel>
        <AlertDialogAction data-testid="confirm-revoke-mcp-key" @click="confirmRevoke">
          {{ t('settings.mcp.revoke') }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>

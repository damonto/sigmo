<script setup lang="ts">
import { computed } from 'vue'
import { CheckCircle2, CircleX, LoaderCircle, RefreshCw, Settings2, Wifi } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

const props = defineProps<{
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  isLoading: boolean
  isUpdating: boolean
  isReconnecting: boolean
}>()

const emit = defineEmits<{
  (event: 'reconnect'): void
}>()

const { t } = useI18n()

const normalizedState = computed(() => {
  if (props.isLoading) return 'loading'
  if (!props.enabled) return 'disabled'
  if (props.connected || props.state === 'connected') return 'connected'
  if (props.isReconnecting || props.state === 'connecting') return 'connecting'
  if (props.state === 'websheet_required') return 'websheet_required'
  return 'disconnected'
})

const showReconnect = computed(() => props.enabled && !props.isLoading)

const isReconnectDisabled = computed(() => props.isUpdating || props.isReconnecting)

const statusLabel = computed(() => {
  switch (normalizedState.value) {
    case 'loading':
      return t('modemDetail.settings.wifiCallingStatusLoading')
    case 'connected':
      return t('modemDetail.settings.wifiCallingConnected')
    case 'connecting':
      return t('modemDetail.settings.wifiCallingConnecting')
    case 'websheet_required':
      return t('modemDetail.settings.wifiCallingSetupRequired')
    case 'disabled':
      return t('modemDetail.settings.wifiCallingDisabled')
    default:
      return t('modemDetail.settings.wifiCallingDisconnected')
  }
})

const statusDescription = computed(() => {
  switch (normalizedState.value) {
    case 'loading':
      return t('modemDetail.settings.wifiCallingStatusLoadingDescription')
    case 'connected':
      return t('modemDetail.settings.wifiCallingConnectedDescription')
    case 'connecting':
      return t('modemDetail.settings.wifiCallingConnectingDescription')
    case 'websheet_required':
      return t('modemDetail.settings.wifiCallingSetupRequiredDescription')
    case 'disabled':
      return t('modemDetail.settings.wifiCallingDisabledDescription')
    default:
      return t('modemDetail.settings.wifiCallingDisconnectedDescription')
  }
})

const statusTone = computed(() => {
  switch (normalizedState.value) {
    case 'connected':
      return 'connected'
    case 'connecting':
      return 'connecting'
    case 'websheet_required':
      return 'setup'
    case 'disabled':
      return 'disabled'
    default:
      return 'disconnected'
  }
})

const durationLabel = computed(() => formatDuration(props.connected ? props.durationSeconds : 0))

const formatDuration = (seconds: number) => {
  const normalized = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(normalized / 3600)
  const minutes = Math.floor((normalized % 3600) / 60)
  const remainingSeconds = normalized % 60
  if (hours > 0) {
    return `${hours}h ${minutes}m ${remainingSeconds}s`
  }
  if (minutes > 0) {
    return `${minutes}m ${remainingSeconds}s`
  }
  return `${remainingSeconds}s`
}
</script>

<template>
  <Card
    class="overflow-hidden py-0 shadow-sm"
    :class="
      statusTone === 'connected'
        ? 'border-emerald-500/20 bg-emerald-50/40 dark:bg-emerald-950/10'
        : 'border-primary/10 bg-card'
    "
  >
    <CardContent class="p-0">
      <div class="flex items-start gap-3 border-b border-border/70 p-4">
        <div
          class="relative flex size-11 shrink-0 items-center justify-center rounded-full"
          :class="
            statusTone === 'connected'
              ? 'bg-emerald-500/10 text-emerald-600'
              : 'bg-primary/10 text-primary'
          "
        >
          <Wifi class="size-6" />
          <span
            class="absolute -bottom-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full border-2 border-card"
            :class="
              statusTone === 'connected'
                ? 'bg-emerald-500 text-white'
                : 'bg-primary text-primary-foreground'
            "
          >
            <CheckCircle2 v-if="statusTone === 'connected'" class="size-3" />
            <LoaderCircle v-else-if="statusTone === 'connecting'" class="size-3 animate-spin" />
            <Settings2 v-else-if="statusTone === 'setup'" class="size-3" />
            <CircleX v-else class="size-3" />
          </span>
        </div>

        <div class="min-w-0 flex-1">
          <p
            class="text-base font-semibold"
            :class="statusTone === 'connected' ? 'text-emerald-600' : 'text-primary'"
          >
            {{ statusLabel }}
          </p>
          <p class="truncate text-sm text-muted-foreground">
            {{ statusDescription }}
          </p>
        </div>

        <Button
          v-if="showReconnect"
          type="button"
          size="icon-sm"
          variant="ghost"
          :disabled="isReconnectDisabled"
          :title="t('modemDetail.settings.wifiCallingReconnect')"
          :aria-label="t('modemDetail.settings.wifiCallingReconnect')"
          @click="emit('reconnect')"
        >
          <LoaderCircle v-if="props.isReconnecting" class="size-4 animate-spin" />
          <RefreshCw v-else class="size-4" />
        </Button>
      </div>

      <div class="flex items-center justify-between gap-3 px-4 py-3 text-sm">
        <span class="text-muted-foreground">
          {{ t('modemDetail.settings.wifiCallingDuration') }}
        </span>
        <span class="font-semibold text-foreground">
          {{ durationLabel }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CheckCircle2, CircleX, LoaderCircle } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import VoLTEIcon from '@/components/modem/VoLTEIcon.vue'
import { Card, CardContent } from '@/components/ui/card'
import type { ConnectionProgress } from '@/types/connectionProgress'
import ConnectionProgressNodes from './ConnectionProgressNodes.vue'

const props = defineProps<{
  enabled: boolean
  connected: boolean
  modemRegistered: boolean
  state: string
  progress?: ConnectionProgress
  durationSeconds: number
  isLoading: boolean
}>()

const { t } = useI18n()

const normalizedState = computed(() => {
  if (props.isLoading) return 'loading'
  if (!props.enabled && props.modemRegistered) return 'modem_managed'
  if (!props.enabled) return 'disabled'
  if (props.progress?.status === 'failed') return 'disconnected'
  if (props.progress?.status === 'running' || props.progress?.status === 'waiting') {
    return 'connecting'
  }
  if (props.connected || props.state === 'connected') return 'connected'
  if (props.state === 'connecting') return 'connecting'
  return 'disconnected'
})

const statusLabel = computed(() => {
  switch (normalizedState.value) {
    case 'loading':
      return t('modemDetail.settings.volteStatusLoading')
    case 'connected':
      return t('modemDetail.settings.volteConnected')
    case 'connecting':
      return t('modemDetail.settings.volteConnecting')
    case 'modem_managed':
      return t('modemDetail.settings.volteConnected')
    case 'disabled':
      return t('modemDetail.settings.volteDisabled')
    default:
      return t('modemDetail.settings.volteDisconnected')
  }
})

const statusDescription = computed(() =>
  normalizedState.value === 'modem_managed'
    ? t('modemDetail.settings.volteModemRegisteredDescription')
    : '',
)

const isConnected = computed(() => normalizedState.value === 'connected')
const isActive = computed(
  () => normalizedState.value === 'connected' || normalizedState.value === 'modem_managed',
)
const isPending = computed(
  () => normalizedState.value === 'loading' || normalizedState.value === 'connecting',
)
const durationLabel = computed(() => {
  if (normalizedState.value === 'modem_managed') return '—'
  return formatDuration(isConnected.value ? props.durationSeconds : 0)
})

const formatDuration = (seconds: number) => {
  const normalized = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(normalized / 3600)
  const minutes = Math.floor((normalized % 3600) / 60)
  const remainingSeconds = normalized % 60
  if (hours > 0) return `${hours}h ${minutes}m ${remainingSeconds}s`
  if (minutes > 0) return `${minutes}m ${remainingSeconds}s`
  return `${remainingSeconds}s`
}
</script>

<template>
  <Card
    class="overflow-hidden py-0 shadow-sm"
    :class="
      isActive
        ? 'border-emerald-500/20 bg-emerald-50/40 dark:bg-emerald-950/10'
        : 'border-primary/10 bg-card'
    "
  >
    <CardContent class="p-0">
      <div class="flex items-start gap-3 p-4">
        <div
          class="relative flex size-11 shrink-0 items-center justify-center rounded-full"
          :class="isActive ? 'bg-emerald-500/10 text-emerald-600' : 'bg-primary/10 text-primary'"
        >
          <VoLTEIcon class="size-6" />
          <span
            class="absolute -right-0.5 -bottom-0.5 flex size-4 items-center justify-center rounded-full border-2 border-card"
            :class="isActive ? 'bg-emerald-500 text-white' : 'bg-primary text-primary-foreground'"
          >
            <CheckCircle2
              v-if="isActive"
              class="size-3"
            />
            <LoaderCircle
              v-else-if="isPending"
              class="size-3 animate-spin"
            />
            <CircleX
              v-else
              class="size-3"
            />
          </span>
        </div>

        <div class="min-w-0 flex-1">
          <p
            class="text-base font-semibold"
            :class="isActive ? 'text-emerald-600' : 'text-primary'"
          >
            {{ statusLabel }}
          </p>
          <ConnectionProgressNodes
            v-if="props.enabled && !props.isLoading"
            access="volte"
            :connected="isConnected"
            :progress="props.progress"
          />
          <p
            v-if="statusDescription"
            class="truncate text-sm text-muted-foreground"
          >
            {{ statusDescription }}
          </p>
        </div>
      </div>

      <div
        class="flex items-center justify-between gap-3 border-t border-border/70 px-4 py-3 text-sm"
      >
        <span class="text-muted-foreground">
          {{ t('modemDetail.settings.volteDuration') }}
        </span>
        <span class="font-semibold text-foreground">
          {{ durationLabel }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>

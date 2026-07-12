<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const props = defineProps<{
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  canEnable: boolean
  modemRegistered: boolean
  isLoading: boolean
  isUpdating: boolean
  canUpdate: boolean
}>()

const emit = defineEmits<{
  (event: 'update', enabled: boolean): void
}>()

const { t } = useI18n()

const description = computed(() => {
  if (props.enabled) return t('modemDetail.settings.volteManagedDescription')
  if (props.modemRegistered) return t('modemDetail.settings.volteModemRegisteredDescription')
  if (!props.canEnable) return t('modemDetail.settings.volteUnavailable')
  return t('modemDetail.settings.volteDescription')
})

const duration = computed(() => {
  const minutes = Math.floor(props.durationSeconds / 60)
  const seconds = props.durationSeconds % 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
})
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">{{ t('modemDetail.settings.volteTitle') }}</CardTitle>
    </CardHeader>
    <CardContent class="space-y-3 px-4">
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="volte-enabled">{{ t('modemDetail.settings.volteLabel') }}</Label>
          <p class="text-xs leading-5 text-muted-foreground">{{ description }}</p>
        </div>
        <div class="inline-flex shrink-0 items-center gap-2">
          <Spinner v-if="props.isUpdating" class="size-4 text-muted-foreground" />
          <Switch
            id="volte-enabled"
            :model-value="props.enabled"
            :disabled="props.isLoading || !props.canUpdate"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
      <div v-if="props.enabled" class="flex justify-between border-t pt-3 text-xs">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.volteStatus') }}</span>
        <span>{{ props.connected ? `${props.state} · ${duration}` : props.state }}</span>
      </div>
    </CardContent>
  </Card>
</template>

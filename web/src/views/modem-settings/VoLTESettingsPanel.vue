<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const props = defineProps<{
  enabled: boolean
  modemRegistered: boolean
  isLoading: boolean
  isUpdating: boolean
}>()

const emit = defineEmits<{
  (event: 'update', enabled: boolean): void
}>()

const { t } = useI18n()

const description = computed(() => {
  if (props.enabled) return t('modemDetail.settings.volteManagedDescription')
  if (props.modemRegistered) return t('modemDetail.settings.volteModemRegisteredDescription')
  return t('modemDetail.settings.volteDescription')
})
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">{{ t('modemDetail.settings.volteTitle') }}</CardTitle>
    </CardHeader>
    <CardContent class="px-4">
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
            :disabled="props.isLoading || props.isUpdating"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>

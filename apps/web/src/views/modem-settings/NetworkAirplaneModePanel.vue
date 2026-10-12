<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const props = defineProps<{
  supported: boolean
  enabled: boolean
  isLoading: boolean
  isUpdating: boolean
  canUpdate: boolean
}>()

const emit = defineEmits<{
  (event: 'update', enabled: boolean): void
}>()

const { t } = useI18n()
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkAirplaneModeTitle') }}
      </CardTitle>
    </CardHeader>

    <CardContent class="space-y-3 px-4">
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="network-airplane-mode">
            {{ t('modemDetail.settings.networkAirplaneModeLabel') }}
          </Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{
              props.supported
                ? t('modemDetail.settings.networkAirplaneModeDescription')
                : t('modemDetail.settings.networkAirplaneModeUnsupported')
            }}
          </p>
        </div>
        <div class="inline-flex shrink-0 items-center gap-2">
          <Spinner v-if="props.isUpdating" class="size-4 text-muted-foreground" />
          <Switch
            id="network-airplane-mode"
            :model-value="props.enabled"
            :disabled="props.isLoading || !props.canUpdate"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>

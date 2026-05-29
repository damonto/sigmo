<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import type { BandResponse } from '@/types/network'

const props = defineProps<{
  supportedBands: BandResponse[]
  selectedBands: number[]
  isSettingsLoading: boolean
  isBandUpdating: boolean
  canUpdateBands: boolean
}>()

const emit = defineEmits<{
  (event: 'toggleBand', value: number, checked: boolean): void
  (event: 'updateBands'): void
}>()

const { t } = useI18n()

const hasBandOptions = computed(() => props.supportedBands.length > 0)

const isBandSelected = (band: number) => props.selectedBands.includes(band)
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkBandTitle') }}
      </CardTitle>
      <CardAction>
        <Button
          size="sm"
          type="button"
          :disabled="!props.canUpdateBands"
          @click="emit('updateBands')"
        >
          <span v-if="props.isBandUpdating" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.networkApply') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.networkApply') }}</span>
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent class="space-y-3 px-4">
      <div v-if="hasBandOptions" class="flex flex-wrap gap-2">
        <div
          v-for="band in props.supportedBands"
          :key="band.value"
          class="inline-flex min-h-9 items-center gap-2 px-1 py-1"
        >
          <Checkbox
            :id="`band-${band.value}`"
            :model-value="isBandSelected(band.value)"
            :disabled="props.isSettingsLoading || props.isBandUpdating"
            @update:model-value="emit('toggleBand', band.value, $event === true)"
          />
          <Label :for="`band-${band.value}`" class="cursor-pointer text-sm font-normal">
            {{ band.label }}
          </Label>
        </div>
      </div>
      <p v-else-if="!props.isSettingsLoading" class="text-xs text-muted-foreground">
        {{ t('modemDetail.settings.networkBandEmpty') }}
      </p>
    </CardContent>
  </Card>
</template>

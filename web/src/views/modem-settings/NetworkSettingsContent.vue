<script setup lang="ts">
import NetworkBandsPanel from './NetworkBandsPanel.vue'
import NetworkModePanel from './NetworkModePanel.vue'
import NetworkOverviewPanel from './NetworkOverviewPanel.vue'
import type { BandResponse, ModeResponse } from '@/types/network'

const selectedMode = defineModel<string>('selectedMode', { required: true })

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  isScanning: boolean
  modeOptions: ModeResponse[]
  supportedBands: BandResponse[]
  selectedBands: number[]
  isSettingsLoading: boolean
  isModeUpdating: boolean
  isBandUpdating: boolean
  canUpdateMode: boolean
  canUpdateBands: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
  (event: 'toggleBand', value: number, checked: boolean): void
  (event: 'updateMode'): void
  (event: 'updateBands'): void
}>()

const handleToggleBand = (value: number, checked: boolean) => {
  emit('toggleBand', value, checked)
}
</script>

<template>
  <div class="space-y-3">
    <NetworkOverviewPanel
      :operator-label="props.operatorLabel"
      :registration-state="props.registrationState"
      :access-technology="props.accessTechnology"
      :is-scanning="props.isScanning"
      @scan="emit('scan')"
    />

    <NetworkModePanel
      v-model:selected-mode="selectedMode"
      :mode-options="props.modeOptions"
      :is-settings-loading="props.isSettingsLoading"
      :is-mode-updating="props.isModeUpdating"
      :can-update-mode="props.canUpdateMode"
      @update-mode="emit('updateMode')"
    />

    <NetworkBandsPanel
      :supported-bands="props.supportedBands"
      :selected-bands="props.selectedBands"
      :is-settings-loading="props.isSettingsLoading"
      :is-band-updating="props.isBandUpdating"
      :can-update-bands="props.canUpdateBands"
      @toggle-band="handleToggleBand"
      @update-bands="emit('updateBands')"
    />
  </div>
</template>

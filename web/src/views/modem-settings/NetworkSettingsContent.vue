<script setup lang="ts">
import NetworkAirplaneModePanel from './NetworkAirplaneModePanel.vue'
import NetworkBandsPanel from './NetworkBandsPanel.vue'
import NetworkModePanel from './NetworkModePanel.vue'
import NetworkOverviewPanel from './NetworkOverviewPanel.vue'
import NetworkVoLTEPanel from './NetworkVoLTEPanel.vue'
import type { BandResponse, ModeResponse } from '@/types/network'

const selectedMode = defineModel<string>('selectedMode', { required: true })

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  isScanning: boolean
  canScanNetworks: boolean
  modeOptions: ModeResponse[]
  supportedBands: BandResponse[]
  selectedBands: number[]
  airplaneModeSupported: boolean
  airplaneModeEnabled: boolean
  volteManaged: boolean
  volteCanEnable: boolean
  isSettingsLoading: boolean
  isModeUpdating: boolean
  isBandUpdating: boolean
  isAirplaneModeUpdating: boolean
  isVolteUpdating: boolean
  canUpdateMode: boolean
  canUpdateBands: boolean
  canUpdateAirplaneMode: boolean
  canUpdateVolte: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
  (event: 'toggleBand', value: number, checked: boolean): void
  (event: 'updateMode'): void
  (event: 'updateBands'): void
  (event: 'updateAirplaneMode', enabled: boolean): void
  (event: 'updateVolte', managed: boolean): void
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
      :can-scan="props.canScanNetworks"
      @scan="emit('scan')"
    />

    <NetworkAirplaneModePanel
      :supported="props.airplaneModeSupported"
      :enabled="props.airplaneModeEnabled"
      :is-loading="props.isSettingsLoading"
      :is-updating="props.isAirplaneModeUpdating"
      :can-update="props.canUpdateAirplaneMode"
      @update="emit('updateAirplaneMode', $event)"
    />

    <NetworkVoLTEPanel
      :managed="props.volteManaged"
      :can-enable="props.volteCanEnable"
      :is-loading="props.isSettingsLoading || props.airplaneModeEnabled"
      :is-updating="props.isVolteUpdating"
      :can-update="props.canUpdateVolte"
      @update="emit('updateVolte', $event)"
    />

    <NetworkModePanel
      v-model:selected-mode="selectedMode"
      :mode-options="props.modeOptions"
      :is-settings-loading="props.isSettingsLoading || props.airplaneModeEnabled"
      :is-mode-updating="props.isModeUpdating"
      :can-update-mode="props.canUpdateMode"
      @update-mode="emit('updateMode')"
    />

    <NetworkBandsPanel
      :supported-bands="props.supportedBands"
      :selected-bands="props.selectedBands"
      :is-settings-loading="props.isSettingsLoading || props.airplaneModeEnabled"
      :is-band-updating="props.isBandUpdating"
      :can-update-bands="props.canUpdateBands"
      @toggle-band="handleToggleBand"
      @update-bands="emit('updateBands')"
    />
  </div>
</template>

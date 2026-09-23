<script setup lang="ts">
import NetworkAirplaneModePanel from './NetworkAirplaneModePanel.vue'
import NetworkBandsPanel from './NetworkBandsPanel.vue'
import NetworkModePanel from './NetworkModePanel.vue'
import NetworkOverviewPanel from './NetworkOverviewPanel.vue'
import type { BandResponse, BandValue, ModeResponse, RegistrationMode } from '@/types/network'

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  registrationMode: RegistrationMode
  registeredOperatorCode: string
  isScanning: boolean
  isRegistrationUpdating: boolean
  canScanNetworks: boolean
  canUpdateRegistration: boolean
  modeOptions: ModeResponse[]
  supportedBands: BandResponse[]
  selectedBands: BandValue[]
  airplaneModeSupported: boolean
  airplaneModeEnabled: boolean
  isSettingsLoading: boolean
  isModeUpdating: boolean
  isBandUpdating: boolean
  isAirplaneModeUpdating: boolean
  canUpdateMode: boolean
  canUpdateBands: boolean
  canUpdateAirplaneMode: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
  (event: 'updateRegistrationMode', mode: RegistrationMode): void
  (event: 'toggleBand', value: BandValue, checked: boolean): void
  (event: 'updateMode'): void
  (event: 'updateBands'): void
  (event: 'updateAirplaneMode', enabled: boolean): void
}>()

const selectedMode = defineModel<string>('selectedMode', { required: true })

const handleToggleBand = (value: BandValue, checked: boolean) => {
  emit('toggleBand', value, checked)
}
</script>

<template>
  <div class="space-y-3">
    <NetworkOverviewPanel
      :operator-label="props.operatorLabel"
      :registration-state="props.registrationState"
      :access-technology="props.accessTechnology"
      :registration-mode="props.registrationMode"
      :registered-operator-code="props.registeredOperatorCode"
      :is-scanning="props.isScanning"
      :is-registration-updating="props.isRegistrationUpdating"
      :can-scan="props.canScanNetworks"
      :can-update-registration="props.canUpdateRegistration"
      @scan="emit('scan')"
      @update-registration-mode="emit('updateRegistrationMode', $event)"
    />

    <NetworkAirplaneModePanel
      :supported="props.airplaneModeSupported"
      :enabled="props.airplaneModeEnabled"
      :is-loading="props.isSettingsLoading"
      :is-updating="props.isAirplaneModeUpdating"
      :can-update="props.canUpdateAirplaneMode"
      @update="emit('updateAirplaneMode', $event)"
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

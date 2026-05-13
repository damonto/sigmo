<script setup lang="ts">
import ModemNetworkBandsCard from '@/components/modem/settings/ModemNetworkBandsCard.vue'
import ModemNetworkCellInfoCard from '@/components/modem/settings/ModemNetworkCellInfoCard.vue'
import ModemNetworkModeCard from '@/components/modem/settings/ModemNetworkModeCard.vue'
import ModemNetworkOverviewCard from '@/components/modem/settings/ModemNetworkOverviewCard.vue'
import type { BandResponse, CellInfoResponse, ModeResponse } from '@/types/network'

const selectedMode = defineModel<string>('selectedMode', { required: true })

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  isScanning: boolean
  modeOptions: ModeResponse[]
  supportedBands: BandResponse[]
  selectedBands: number[]
  cellInfo: CellInfoResponse[]
  isSettingsLoading: boolean
  isModeUpdating: boolean
  isBandUpdating: boolean
  isCellInfoLoading: boolean
  canUpdateMode: boolean
  canUpdateBands: boolean
  hasCells: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
  (event: 'toggleBand', value: number, checked: boolean): void
  (event: 'updateMode'): void
  (event: 'updateBands'): void
  (event: 'refreshCells'): void
}>()

const handleToggleBand = (value: number, checked: boolean) => {
  emit('toggleBand', value, checked)
}
</script>

<template>
  <div class="space-y-3">
    <ModemNetworkOverviewCard
      :operator-label="props.operatorLabel"
      :registration-state="props.registrationState"
      :access-technology="props.accessTechnology"
      :is-scanning="props.isScanning"
      @scan="emit('scan')"
    />

    <ModemNetworkModeCard
      v-model:selected-mode="selectedMode"
      :mode-options="props.modeOptions"
      :is-settings-loading="props.isSettingsLoading"
      :is-mode-updating="props.isModeUpdating"
      :can-update-mode="props.canUpdateMode"
      @update-mode="emit('updateMode')"
    />

    <ModemNetworkBandsCard
      :supported-bands="props.supportedBands"
      :selected-bands="props.selectedBands"
      :is-settings-loading="props.isSettingsLoading"
      :is-band-updating="props.isBandUpdating"
      :can-update-bands="props.canUpdateBands"
      @toggle-band="handleToggleBand"
      @update-bands="emit('updateBands')"
    />

    <ModemNetworkCellInfoCard
      :cell-info="props.cellInfo"
      :is-cell-info-loading="props.isCellInfoLoading"
      :has-cells="props.hasCells"
      @refresh-cells="emit('refreshCells')"
    />
  </div>
</template>

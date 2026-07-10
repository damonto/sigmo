<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import ModemSettingsShell from '@/components/modem/settings/ModemSettingsShell.vue'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemNetwork } from '@/composables/useModemNetwork'
import { useModemOverview } from '@/composables/useModemOverview'
import NetworkRegistrationDialog from '@/views/modem-settings/NetworkRegistrationDialog.vue'
import NetworkSettingsContent from '@/views/modem-settings/NetworkSettingsContent.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)

const { showFeedback, showError } = useFeedbackBanner()
const { currentOperatorLabel, currentRegistrationState, currentAccessTechnology, fetchModem } =
  useModemOverview(modemId)

const {
  networkDialogOpen,
  availableNetworks,
  selectedNetwork,
  modeOptions,
  selectedMode,
  supportedBands,
  selectedBands,
  airplaneModeSupported,
  airplaneModeEnabled,
  volteManaged,
  volteCanEnable,
  isNetworkLoading,
  isNetworkRegistering,
  isNetworkSettingsLoading,
  isModeUpdating,
  isBandUpdating,
  isAirplaneModeUpdating,
  isVoLTEUpdating,
  hasAvailableNetworks,
  hasNetworkSelection,
  canScanNetworks,
  canUpdateMode,
  canUpdateBands,
  canUpdateAirplaneMode,
  canUpdateVoLTE,
  openNetworkDialog,
  handleNetworkRegister,
  handleModeUpdate,
  toggleBand,
  handleBandUpdate,
  handleAirplaneModeUpdate,
  handleVoLTEUpdate,
} = useModemNetwork({
  modemId,
  onRegistered: fetchModem,
  onChanged: fetchModem,
  onSuccess: showFeedback,
  onError: showError,
})
</script>

<template>
  <ModemSettingsShell>
    <ModemSettingsHeader
      :title="t('modemDetail.settings.networkTitle')"
      :subtitle="t('modemDetail.settings.networkCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <NetworkSettingsContent
      v-model:selected-mode="selectedMode"
      :operator-label="currentOperatorLabel"
      :registration-state="currentRegistrationState"
      :access-technology="currentAccessTechnology"
      :is-scanning="isNetworkLoading"
      :can-scan-networks="canScanNetworks"
      :mode-options="modeOptions"
      :supported-bands="supportedBands"
      :selected-bands="selectedBands"
      :airplane-mode-supported="airplaneModeSupported"
      :airplane-mode-enabled="airplaneModeEnabled"
      :volte-managed="volteManaged"
      :volte-can-enable="volteCanEnable"
      :is-settings-loading="isNetworkSettingsLoading"
      :is-mode-updating="isModeUpdating"
      :is-band-updating="isBandUpdating"
      :is-airplane-mode-updating="isAirplaneModeUpdating"
      :is-volte-updating="isVoLTEUpdating"
      :can-update-mode="canUpdateMode"
      :can-update-bands="canUpdateBands"
      :can-update-airplane-mode="canUpdateAirplaneMode"
      :can-update-volte="canUpdateVoLTE"
      @scan="openNetworkDialog"
      @toggle-band="toggleBand"
      @update-mode="handleModeUpdate"
      @update-bands="handleBandUpdate"
      @update-airplane-mode="handleAirplaneModeUpdate"
      @update-volte="handleVoLTEUpdate"
    />
  </ModemSettingsShell>

  <NetworkRegistrationDialog
    v-model:open="networkDialogOpen"
    v-model:selected-network="selectedNetwork"
    :networks="availableNetworks"
    :is-loading="isNetworkLoading"
    :is-registering="isNetworkRegistering"
    :has-available-networks="hasAvailableNetworks"
    :has-selection="hasNetworkSelection"
    @register="handleNetworkRegister"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import CarrierWebsheetDialog from '@/components/CarrierWebsheetDialog.vue'
import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import ModemSettingsShell from '@/components/modem/settings/ModemSettingsShell.vue'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemWiFiCallingSettings } from '@/composables/useModemWiFiCallingSettings'
import WiFiCallingEmergencyAddressPanel from '@/views/modem-settings/WiFiCallingEmergencyAddressPanel.vue'
import WiFiCallingSettingsPanel from '@/views/modem-settings/WiFiCallingSettingsPanel.vue'
import WiFiCallingStatusPanel from '@/views/modem-settings/WiFiCallingStatusPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const { showFeedback } = useFeedbackBanner()
const { hasFeature } = useCapabilities()
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))

const {
  settingsWiFiCallingEnabled,
  settingsWiFiCallingPreferred,
  settingsWiFiCallingConnected,
  settingsWiFiCallingState,
  settingsWiFiCallingDurationSeconds,
  settingsWiFiCallingEmergencyAddressUpdateAvailable,
  settingsWiFiCallingWebsheet,
  settingsWiFiCallingEmergencyAddressWebsheet,
  isWiFiCallingSettingsLoading,
  isWiFiCallingSettingsUpdating,
  isWiFiCallingWebsheetStarting,
  isWiFiCallingEmergencyAddressWebsheetStarting,
  handleWiFiCallingUpdate,
  startWiFiCallingWebsheet,
  startWiFiCallingEmergencyAddressWebsheet,
  completeWiFiCallingWebsheet,
  completeWiFiCallingEmergencyAddressWebsheet,
} = useModemWiFiCallingSettings({
  modemId,
  enabled: canUseWiFiCalling,
  onSuccess: showFeedback,
})

const closeWiFiCallingWebsheet = () => {
  settingsWiFiCallingWebsheet.value = null
}

const closeWiFiCallingEmergencyAddressWebsheet = () => {
  void completeWiFiCallingEmergencyAddressWebsheet()
}
</script>

<template>
  <ModemSettingsShell>
    <ModemSettingsHeader
      :title="t('modemDetail.settings.wifiCallingTitle')"
      :subtitle="t('modemDetail.settings.wifiCallingCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <template v-if="canUseWiFiCalling">
      <WiFiCallingStatusPanel
        :enabled="settingsWiFiCallingEnabled"
        :connected="settingsWiFiCallingConnected"
        :state="settingsWiFiCallingState"
        :duration-seconds="settingsWiFiCallingDurationSeconds"
        :is-loading="isWiFiCallingSettingsLoading"
        :is-updating="isWiFiCallingSettingsUpdating"
        @reconnect="handleWiFiCallingUpdate"
      />

      <WiFiCallingSettingsPanel
        v-model:enabled="settingsWiFiCallingEnabled"
        v-model:preferred="settingsWiFiCallingPreferred"
        :is-loading="isWiFiCallingSettingsLoading"
        :is-updating="isWiFiCallingSettingsUpdating"
        :is-websheet-starting="isWiFiCallingWebsheetStarting"
        :state="settingsWiFiCallingState"
        :websheet="settingsWiFiCallingWebsheet"
        @update="handleWiFiCallingUpdate"
        @start-websheet="startWiFiCallingWebsheet"
      />

      <WiFiCallingEmergencyAddressPanel
        v-if="settingsWiFiCallingEmergencyAddressUpdateAvailable"
        :is-starting="isWiFiCallingEmergencyAddressWebsheetStarting"
        @start="startWiFiCallingEmergencyAddressWebsheet"
      />
    </template>

    <div v-else class="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">
      {{ t('modemDetail.settings.wifiCallingUnavailable') }}
    </div>
  </ModemSettingsShell>

  <CarrierWebsheetDialog
    :open="settingsWiFiCallingWebsheet !== null"
    :websheet="settingsWiFiCallingWebsheet"
    close-on-status-change
    @cancel="closeWiFiCallingWebsheet"
    @done="completeWiFiCallingWebsheet"
  />

  <CarrierWebsheetDialog
    :open="settingsWiFiCallingEmergencyAddressWebsheet !== null"
    :websheet="settingsWiFiCallingEmergencyAddressWebsheet"
    close-on-status-change
    @cancel="closeWiFiCallingEmergencyAddressWebsheet"
    @done="completeWiFiCallingEmergencyAddressWebsheet"
  />
</template>

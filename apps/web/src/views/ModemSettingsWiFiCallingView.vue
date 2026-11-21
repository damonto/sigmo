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
import { useModems } from '@/composables/useModems'
import type { WiFiCallingUnderlay, WiFiDirectUnderlay } from '@/types/modem'
import WiFiCallingEmergencyAddressPanel from '@/views/modem-settings/WiFiCallingEmergencyAddressPanel.vue'
import WiFiCallingProxyPanel from '@/views/modem-settings/WiFiCallingProxyPanel.vue'
import WiFiCallingSettingsPanel from '@/views/modem-settings/WiFiCallingSettingsPanel.vue'
import WiFiCallingStatusPanel from '@/views/modem-settings/WiFiCallingStatusPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const { showFeedback, showError } = useFeedbackBanner()
const { hasFeature } = useCapabilities()
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))
const { modems, fetchModems } = useModems()
void fetchModems()
const simId = computed(
  () => modems.value.find((modem) => modem.id === modemId.value)?.sim?.identifier,
)

const {
  hasWiFiCallingSettings,
  savedWiFiCallingProxyId,
  settingsWiFiCallingEnabled,
  settingsWiFiCallingUnderlay,
  settingsWiFiCallingConnected,
  settingsWiFiCallingState,
  settingsWiFiCallingLastError,
  settingsWiFiCallingDurationSeconds,
  settingsWiFiCallingEmergencyAddressUpdateAvailable,
  settingsWiFiCallingWebsheet,
  settingsWiFiCallingEmergencyAddressWebsheet,
  isWiFiCallingSettingsLoading,
  isWiFiCallingSettingsUpdating,
  isWiFiCallingReconnecting,
  isWiFiCallingWebsheetStarting,
  isWiFiCallingEmergencyAddressWebsheetStarting,
  updateWiFiCallingSettings,
  reconnectWiFiCalling,
  startWiFiCallingWebsheet,
  startWiFiCallingEmergencyAddressWebsheet,
  completeWiFiCallingWebsheet,
  completeWiFiCallingEmergencyAddressWebsheet,
} = useModemWiFiCallingSettings({
  modemId,
  simId,
  enabled: canUseWiFiCalling,
  onSuccess: showFeedback,
  onError: showError,
})

const updateWiFiCallingProxy = (proxyId: string | null) => {
  const current = settingsWiFiCallingUnderlay.value
  const directUnderlay: WiFiDirectUnderlay =
    current.mode === 'socks5'
      ? (current.directUnderlay ?? { mode: 'system' })
      : { mode: current.mode, modemId: current.modemId }
  const underlay: WiFiCallingUnderlay = proxyId
    ? { mode: 'socks5', proxyId, directUnderlay }
    : { ...directUnderlay, proxyId: current.proxyId }

  return updateWiFiCallingSettings({ enabled: settingsWiFiCallingEnabled.value, underlay })
}

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
        :last-error="settingsWiFiCallingLastError"
        :is-loading="isWiFiCallingSettingsLoading"
        :is-updating="isWiFiCallingSettingsUpdating"
        :is-reconnecting="isWiFiCallingReconnecting"
        @reconnect="reconnectWiFiCalling"
      />

      <WiFiCallingSettingsPanel
        :enabled="settingsWiFiCallingEnabled"
        :underlay="settingsWiFiCallingUnderlay"
        :modems="modems"
        :modem-id="modemId"
        :is-loading="!hasWiFiCallingSettings || isWiFiCallingSettingsLoading"
        :is-updating="isWiFiCallingSettingsUpdating"
        :is-websheet-starting="isWiFiCallingWebsheetStarting"
        :state="settingsWiFiCallingState"
        :websheet="settingsWiFiCallingWebsheet"
        @update="updateWiFiCallingSettings"
        @start-websheet="startWiFiCallingWebsheet"
      />

      <WiFiCallingProxyPanel
        :key="`${modemId}:${simId ?? ''}`"
        :modem-id="modemId"
        :saved-proxy-id="savedWiFiCallingProxyId"
        :using-proxy="settingsWiFiCallingUnderlay.mode === 'socks5'"
        :disabled="!hasWiFiCallingSettings || isWiFiCallingSettingsUpdating"
        @update-proxy="updateWiFiCallingProxy"
      />

      <WiFiCallingEmergencyAddressPanel
        v-if="settingsWiFiCallingEmergencyAddressUpdateAvailable"
        :is-starting="isWiFiCallingEmergencyAddressWebsheetStarting"
        @start="startWiFiCallingEmergencyAddressWebsheet"
      />
    </template>

    <div
      v-else
      class="rounded-xl border border-dashed p-6 text-sm text-muted-foreground"
    >
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

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import CarrierWebsheetDialog from '@/components/CarrierWebsheetDialog.vue'
import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemWiFiCallingSettings } from '@/composables/useModemWiFiCallingSettings'
import WiFiCallingSettingsPanel from '@/views/modem-settings/WiFiCallingSettingsPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const { showFeedback } = useFeedbackBanner()
const { hasFeature, fetchCapabilities } = useCapabilities()
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))

const {
  settingsWiFiCallingEnabled,
  settingsWiFiCallingPreferred,
  settingsWiFiCallingState,
  settingsWiFiCallingWebsheet,
  isWiFiCallingSettingsLoading,
  isWiFiCallingSettingsUpdating,
  isWiFiCallingWebsheetStarting,
  handleWiFiCallingUpdate,
  startWiFiCallingWebsheet,
  completeWiFiCallingWebsheet,
} = useModemWiFiCallingSettings({
  modemId,
  enabled: canUseWiFiCalling,
  onSuccess: showFeedback,
})

const closeWiFiCallingWebsheet = () => {
  settingsWiFiCallingWebsheet.value = null
}

onMounted(() => {
  void fetchCapabilities()
})
</script>

<template>
  <div class="space-y-4">
    <ModemSettingsHeader
      :title="t('modemDetail.settings.wifiCallingTitle')"
      :subtitle="t('modemDetail.settings.wifiCallingCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <WiFiCallingSettingsPanel
      v-if="canUseWiFiCalling"
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

    <div v-else class="rounded-2xl border border-dashed p-6 text-sm text-muted-foreground">
      {{ t('modemDetail.settings.wifiCallingUnavailable') }}
    </div>
  </div>

  <CarrierWebsheetDialog
    :open="settingsWiFiCallingWebsheet !== null"
    :websheet="settingsWiFiCallingWebsheet"
    @cancel="closeWiFiCallingWebsheet"
    @done="completeWiFiCallingWebsheet"
  />
</template>

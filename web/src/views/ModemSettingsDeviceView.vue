<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import ModemSettingsShell from '@/components/modem/settings/ModemSettingsShell.vue'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemDeviceSettings } from '@/composables/useModemDeviceSettings'
import DeviceSettingsPanel from '@/views/modem-settings/DeviceSettingsPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const { showFeedback } = useFeedbackBanner()

const {
  settingsAlias,
  settingsMss,
  settingsCompatible,
  isSettingsLoading,
  isSettingsUpdating,
  isMssValid,
  handleSettingsUpdate,
} = useModemDeviceSettings({
  modemId,
  onSuccess: showFeedback,
})
</script>

<template>
  <ModemSettingsShell>
    <ModemSettingsHeader
      :title="t('modemDetail.settings.deviceTitle')"
      :subtitle="t('modemDetail.settings.deviceCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <DeviceSettingsPanel
      v-model:alias="settingsAlias"
      v-model:mss="settingsMss"
      v-model:compatible="settingsCompatible"
      :is-loading="isSettingsLoading"
      :is-updating="isSettingsUpdating"
      :is-valid="isMssValid"
      @update="handleSettingsUpdate"
    />
  </ModemSettingsShell>
</template>

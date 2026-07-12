<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import ModemSettingsShell from '@/components/modem/settings/ModemSettingsShell.vue'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemVoLTE } from '@/composables/useModemVoLTE'
import VoLTESettingsPanel from '@/views/modem-settings/VoLTESettingsPanel.vue'

const route = useRoute()
const { t } = useI18n()
const modemId = computed(() => route.params.id as string)
const { hasFeature } = useCapabilities()
const canUseVoLTE = computed(() => hasFeature(FEATURE.volte))
const { showFeedback, showError } = useFeedbackBanner()

const {
  volteEnabled,
  volteConnected,
  volteState,
  volteDurationSeconds,
  volteCanEnable,
  volteModemRegistered,
  isVoLTELoading,
  isVoLTEUpdating,
  canUpdateVoLTE,
  updateVoLTE,
} = useModemVoLTE({
  modemId,
  enabled: canUseVoLTE,
  onSuccess: showFeedback,
  onError: showError,
})
</script>

<template>
  <ModemSettingsShell>
    <ModemSettingsHeader
      :title="t('modemDetail.settings.volteTitle')"
      :subtitle="t('modemDetail.settings.volteCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <VoLTESettingsPanel
      v-if="canUseVoLTE"
      :enabled="volteEnabled"
      :connected="volteConnected"
      :state="volteState"
      :duration-seconds="volteDurationSeconds"
      :can-enable="volteCanEnable"
      :modem-registered="volteModemRegistered"
      :is-loading="isVoLTELoading"
      :is-updating="isVoLTEUpdating"
      :can-update="canUpdateVoLTE"
      @update="updateVoLTE"
    />
  </ModemSettingsShell>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import ModemSettingsShell from '@/components/modem/settings/ModemSettingsShell.vue'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemInternet } from '@/composables/useModemInternet'
import { useModemSpeedTest } from '@/composables/useModemSpeedTest'
import InternetSettingsContent from '@/views/modem-settings/InternetSettingsContent.vue'
import InternetSpeedTestPanel from '@/views/modem-settings/InternetSpeedTestPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const { showFeedback, showError } = useFeedbackBanner()

const {
  internetConnection,
  internetPublicInfo,
  internetAPN,
  internetIPType,
  internetAPNUsername,
  internetAPNPassword,
  internetAPNAuth,
  internetDefaultRoute,
  internetProxyEnabled,
  internetAlwaysOn,
  isInternetLoading,
  isInternetConnecting,
  isInternetDisconnecting,
  isInternetPreferencesUpdating,
  isInternetConnected,
  canConnectInternet,
  handleInternetConnect,
  handleInternetDisconnect,
  handleInternetPreferencesUpdate,
} = useModemInternet({
  modemId,
  onSuccess: showFeedback,
  onError: showError,
})
const { result, running, failure, readFailed, canRun, run } = useModemSpeedTest(
  modemId,
  isInternetConnected,
)
</script>

<template>
  <ModemSettingsShell>
    <ModemSettingsHeader
      :title="t('modemDetail.settings.internetTitle')"
      :subtitle="t('modemDetail.settings.internetCategoryDescription')"
      :back-to="{ name: 'modem-settings', params: { id: modemId } }"
    />

    <InternetSettingsContent
      v-model:apn="internetAPN"
      v-model:ip-type="internetIPType"
      v-model:apn-username="internetAPNUsername"
      v-model:apn-password="internetAPNPassword"
      v-model:apn-auth="internetAPNAuth"
      v-model:default-route="internetDefaultRoute"
      v-model:proxy-enabled="internetProxyEnabled"
      v-model:always-on="internetAlwaysOn"
      :connection="internetConnection"
      :public-info="internetPublicInfo"
      :is-loading="isInternetLoading"
      :is-connecting="isInternetConnecting"
      :is-disconnecting="isInternetDisconnecting"
      :is-preferences-updating="isInternetPreferencesUpdating"
      :is-connected="isInternetConnected"
      :can-connect="canConnectInternet"
      @connect="handleInternetConnect"
      @disconnect="handleInternetDisconnect"
      @update-preferences="handleInternetPreferencesUpdate"
    >
      <template #speed-test>
        <InternetSpeedTestPanel
          :key="modemId"
          :result="result"
          :running="running"
          :failure="failure"
          :read-failed="readFailed"
          :can-run="canRun && !isInternetDisconnecting && !isInternetPreferencesUpdating"
          @test="run"
        />
      </template>
    </InternetSettingsContent>
  </ModemSettingsShell>
</template>

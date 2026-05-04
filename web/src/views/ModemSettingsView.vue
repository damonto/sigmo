<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemDeviceSettingsSection from '@/components/modem/settings/ModemDeviceSettingsSection.vue'
import ModemInternetSection from '@/components/modem/settings/ModemInternetSection.vue'
import ModemMsisdnSection from '@/components/modem/settings/ModemMsisdnSection.vue'
import ModemNetworkDialog from '@/components/modem/settings/ModemNetworkDialog.vue'
import ModemNetworkSection from '@/components/modem/settings/ModemNetworkSection.vue'
import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemInternet } from '@/composables/useModemInternet'
import { useModemDeviceSettings } from '@/composables/useModemDeviceSettings'
import { useModemMsisdn } from '@/composables/useModemMsisdn'
import { useModemNetwork } from '@/composables/useModemNetwork'
import { useModemOverview } from '@/composables/useModemOverview'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)

const { showFeedback } = useFeedbackBanner()

const {
  modem,
  isModemLoading,
  currentOperatorLabel,
  currentRegistrationState,
  currentAccessTechnology,
  fetchModem,
} = useModemOverview(modemId)

const { msisdnInput, isMsisdnUpdating, isMsisdnValid, handleMsisdnUpdate } = useModemMsisdn({
  modemId,
  modem,
  refreshModem: fetchModem,
  onSuccess: showFeedback,
})

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

const {
  networkDialogOpen,
  availableNetworks,
  selectedNetwork,
  isNetworkLoading,
  isNetworkRegistering,
  hasAvailableNetworks,
  hasNetworkSelection,
  openNetworkDialog,
  handleNetworkRegister,
} = useModemNetwork({
  modemId,
  onRegistered: fetchModem,
  onSuccess: showFeedback,
})

const {
  internetConnection,
  internetPublicInfo,
  internetAPN,
  internetDefaultRoute,
  internetProxyEnabled,
  internetAlwaysOn,
  isInternetLoading,
  isInternetConnecting,
  isInternetDisconnecting,
  isInternetConnected,
  canConnectInternet,
  handleInternetConnect,
  handleInternetDisconnect,
} = useModemInternet({
  modemId,
  onSuccess: showFeedback,
})
</script>

<template>
  <div class="space-y-3">
    <ModemSettingsHeader />

    <Tabs default-value="network" class="space-y-3">
      <TabsList class="grid w-full grid-cols-3">
        <TabsTrigger value="network">
          {{ t('modemDetail.settings.networkTitle') }}
        </TabsTrigger>
        <TabsTrigger value="internet">
          {{ t('modemDetail.settings.internetTitle') }}
        </TabsTrigger>
        <TabsTrigger value="device">
          {{ t('modemDetail.settings.deviceTitle') }}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="network" class="space-y-3">
        <ModemMsisdnSection
          v-model="msisdnInput"
          :is-loading="isModemLoading"
          :is-updating="isMsisdnUpdating"
          :is-valid="isMsisdnValid"
          @update="handleMsisdnUpdate"
        />

        <ModemNetworkSection
          :operator-label="currentOperatorLabel"
          :registration-state="currentRegistrationState"
          :access-technology="currentAccessTechnology"
          :is-scanning="isNetworkLoading"
          @scan="openNetworkDialog"
        />
      </TabsContent>

      <TabsContent value="internet" class="space-y-3">
        <ModemInternetSection
          v-model:apn="internetAPN"
          v-model:default-route="internetDefaultRoute"
          v-model:proxy-enabled="internetProxyEnabled"
          v-model:always-on="internetAlwaysOn"
          :connection="internetConnection"
          :public-info="internetPublicInfo"
          :is-loading="isInternetLoading"
          :is-connecting="isInternetConnecting"
          :is-disconnecting="isInternetDisconnecting"
          :is-connected="isInternetConnected"
          :can-connect="canConnectInternet"
          @connect="handleInternetConnect"
          @disconnect="handleInternetDisconnect"
        />
      </TabsContent>

      <TabsContent value="device" class="space-y-3">
        <ModemDeviceSettingsSection
          v-model:alias="settingsAlias"
          v-model:mss="settingsMss"
          v-model:compatible="settingsCompatible"
          :is-loading="isSettingsLoading"
          :is-updating="isSettingsUpdating"
          :is-valid="isMssValid"
          @update="handleSettingsUpdate"
        />
      </TabsContent>
    </Tabs>
  </div>

  <ModemNetworkDialog
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

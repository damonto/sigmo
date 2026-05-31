<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Globe2, Phone, RadioTower, Smartphone } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemLineMsisdnDialog from '@/components/modem/settings/ModemLineMsisdnDialog.vue'
import ModemLineSummaryCard from '@/components/modem/settings/ModemLineSummaryCard.vue'
import ModemSettingsCategoryCard from '@/components/modem/settings/ModemSettingsCategoryCard.vue'
import ModemSettingsHeader from '@/components/modem/settings/ModemSettingsHeader.vue'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useFeedbackBanner } from '@/composables/useFeedbackBanner'
import { useModemMsisdn } from '@/composables/useModemMsisdn'
import { useModemOverview } from '@/composables/useModemOverview'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => route.params.id as string)
const msisdnDialogOpen = ref(false)

const { showFeedback } = useFeedbackBanner()
const { hasFeature, fetchCapabilities } = useCapabilities()
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))

const {
  modem,
  isModemLoading,
  currentAccessTechnology,
  fetchModem,
} = useModemOverview(modemId)

const { msisdnInput, isMsisdnUpdating, isMsisdnValid, handleMsisdnUpdate } = useModemMsisdn({
  modemId,
  modem,
  refreshModem: fetchModem,
  onSuccess: showFeedback,
})

const lineOperatorLabel = computed(() => {
  const registeredName = modem.value?.registeredOperator?.name?.trim() ?? ''
  if (registeredName) return registeredName
  const simName = modem.value?.sim?.operatorName?.trim() ?? ''
  return simName || t('modemDetail.settings.networkUnknown')
})

const categoryCards = computed(() => {
  const cards = [
    {
      key: 'network',
      title: t('modemDetail.settings.networkTitle'),
      description: t('modemDetail.settings.networkCategoryDescription'),
      icon: RadioTower,
      to: { name: 'modem-settings-network', params: { id: modemId.value } },
    },
    {
      key: 'internet',
      title: t('modemDetail.settings.internetTitle'),
      description: t('modemDetail.settings.internetCategoryDescription'),
      icon: Globe2,
      to: { name: 'modem-settings-internet', params: { id: modemId.value } },
    },
    {
      key: 'wifi-calling',
      title: t('modemDetail.settings.wifiCallingTitle'),
      description: t('modemDetail.settings.wifiCallingCategoryDescription'),
      icon: Phone,
      to: { name: 'modem-settings-wifi-calling', params: { id: modemId.value } },
    },
    {
      key: 'device',
      title: t('modemDetail.settings.deviceTitle'),
      description: t('modemDetail.settings.deviceCategoryDescription'),
      icon: Smartphone,
      to: { name: 'modem-settings-device', params: { id: modemId.value } },
    },
  ]

  if (canUseWiFiCalling.value) return cards
  return cards.filter((card) => card.key !== 'wifi-calling')
})

const openMsisdnDialog = () => {
  msisdnInput.value = modem.value?.number ?? ''
  msisdnDialogOpen.value = true
}

const saveMsisdn = async () => {
  const updated = await handleMsisdnUpdate()
  if (updated) {
    msisdnDialogOpen.value = false
  }
}

onMounted(() => {
  void fetchCapabilities()
})
</script>

<template>
  <div class="space-y-4">
    <ModemSettingsHeader />

    <ModemLineSummaryCard
      :modem="modem"
      :operator-label="lineOperatorLabel"
      :access-technology="currentAccessTechnology"
      @edit="openMsisdnDialog"
    />

    <ModemLineMsisdnDialog
      v-model:open="msisdnDialogOpen"
      v-model:msisdn="msisdnInput"
      :is-updating="isMsisdnUpdating"
      :is-valid="isMsisdnValid"
      @save="saveMsisdn"
    />

    <div v-if="!modem && !isModemLoading" class="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">
      {{ t('modemDetail.unknown') }}
    </div>

    <div class="space-y-3">
      <ModemSettingsCategoryCard
        v-for="card in categoryCards"
        :key="card.key"
        :title="card.title"
        :description="card.description"
        :icon="card.icon"
        :to="card.to"
      />
    </div>
  </div>
</template>

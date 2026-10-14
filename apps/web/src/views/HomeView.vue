<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import HomeHeader from '@/components/home/HomeHeader.vue'
import HomeModemList from '@/components/home/HomeModemList.vue'
import { useAppInfo } from '@/composables/useAppInfo'
import { useModems } from '@/composables/useModems'
import type { HomeModemItem } from '@/types/home'

const { t } = useI18n()

const { version, fetchAppInfo } = useAppInfo()
const { modems, isLoading, fetchModems } = useModems()

const modemCount = computed(() => modems.value.length)
const subtitle = computed(() => t('home.subtitle', { count: modemCount.value }))

const modemItems = computed<HomeModemItem[]>(() =>
  modems.value.map((modem) => ({
    id: modem.id,
    name: modem.name,
    icon: modem.sim.icon,
    plmn: modem.sim.operatorIdentifier,
    regionCode: modem.sim.regionCode,
    operatorName: modem.sim.operatorName,
    registeredOperatorName: modem.registeredOperator.name,
    registeredOperatorCode: modem.registeredOperator.code,
    registrationState: modem.registrationState,
    accessTechnology: modem.accessTechnology,
    simKind: modem.simKind,
    number: modem.number ?? '',
    signalQuality: modem.signalQuality,
    airplaneMode: modem.airplaneMode,
    wifiCallingConnected: modem.wifiCallingConnected ?? false,
  })),
)

onMounted(() => {
  void fetchAppInfo()
  void fetchModems()
})
</script>

<template>
  <main class="flex flex-col gap-5">
    <HomeHeader
      :subtitle="subtitle"
      :version="version"
    />

    <HomeModemList
      :items="modemItems"
      :is-loading="isLoading"
    />
  </main>
</template>

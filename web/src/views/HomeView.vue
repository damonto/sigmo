<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import HomeHeader from '@/components/home/HomeHeader.vue'
import HomeModemList from '@/components/home/HomeModemList.vue'
import { useModems } from '@/composables/useModems'
import type { HomeModemItem } from '@/types/home'

const { t } = useI18n()

const { modems, isLoading, fetchModems } = useModems()

const modemCount = computed(() => modems.value.length)
const subtitle = computed(() => t('home.subtitle', { count: modemCount.value }))

const modemItems = computed<HomeModemItem[]>(() =>
  modems.value.map((modem) => ({
    id: modem.id,
    name: modem.name,
    regionCode: modem.sim.regionCode,
    operatorName: modem.sim.operatorName,
    registeredOperatorName: modem.registeredOperator.name,
    registeredOperatorCode: modem.registeredOperator.code,
    registrationState: modem.registrationState,
    accessTechnology: modem.accessTechnology,
    supportsEsim: modem.supportsEsim,
    number: modem.number ?? '',
    signalQuality: modem.signalQuality,
    wifiCallingConnected: modem.wifiCallingConnected ?? false,
  })),
)

const handleRefresh = () => {
  void fetchModems()
}

onMounted(() => {
  void fetchModems()
})
</script>

<template>
  <div class="min-h-dvh bg-background">
    <div class="mx-auto flex w-full max-w-7xl flex-col gap-5 px-6 py-10 lg:px-8">
      <HomeHeader :subtitle="subtitle" :is-loading="isLoading" @refresh="handleRefresh" />

      <HomeModemList :items="modemItems" :is-loading="isLoading" />
    </div>
  </div>
</template>

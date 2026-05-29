<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import ModemCallBanner from '@/components/modem/ModemCallBanner.vue'
import { provideModemCallSession } from '@/composables/useModemCallSession'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'

const route = useRoute()
const activeModemId = ref('unknown')
const modemId = computed(() => activeModemId.value)
const { phoneCountry } = useModemPhoneCountry(modemId)

watch(
  () => route.params.id,
  (value) => {
    if (typeof value === 'string' && value) {
      activeModemId.value = value
    }
  },
  { immediate: true },
)

const callSession = provideModemCallSession(modemId, phoneCountry)
</script>

<template>
  <slot />
  <ModemCallBanner :session="callSession" />
</template>

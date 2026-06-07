<script setup lang="ts">
import { onMounted } from 'vue'

import ModemSettingsDesktopNav from '@/components/modem/settings/ModemSettingsDesktopNav.vue'
import { useCapabilities } from '@/composables/useCapabilities'

const { fetchCapabilities } = useCapabilities()

onMounted(() => {
  void fetchCapabilities()
})
</script>

<template>
  <div class="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)] lg:items-start">
    <ModemSettingsDesktopNav />

    <section class="min-w-0 space-y-4">
      <slot />
    </section>
  </div>
</template>

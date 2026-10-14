<script setup lang="ts">
import ModemCallBanner from '@/components/modem/ModemCallBanner.vue'
import { useWorkspaceCalls } from '@/composables/useWorkspaceCalls'
const { callSession } = useWorkspaceCalls()
</script>

<template>
  <slot />
  <ModemCallBanner :session="callSession" />
  <audio
    ref="remoteAudioRef"
    class="sr-only"
    autoplay
    playsinline
  />
</template>

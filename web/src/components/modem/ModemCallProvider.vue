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
const remoteAudioRef = ref<HTMLAudioElement | null>(null)
let boundRemoteAudio: HTMLAudioElement | null = null
let outputBinding = Promise.resolve(true)

const bindRemoteAudio = (audio: HTMLAudioElement | null) => {
  if (boundRemoteAudio === audio) return outputBinding
  boundRemoteAudio = audio
  outputBinding = callSession.callAudio.bindOutputElement(audio)
  return outputBinding
}

const syncRemoteAudio = async (stream: MediaStream | null) => {
  const audio = remoteAudioRef.value
  if (!audio) return
  await bindRemoteAudio(audio)
  if (audio.srcObject !== stream) {
    audio.srcObject = stream
  }
  if (!stream) {
    audio.pause()
    return
  }
  try {
    await audio.play()
  } catch (err) {
    console.warn('[ModemCallProvider] play remote call audio:', err)
  }
}

watch(
  remoteAudioRef,
  (audio) => {
    void bindRemoteAudio(audio)
  },
  { flush: 'post' },
)

watch(
  callSession.callAudio.remoteStream,
  (stream) => {
    void syncRemoteAudio(stream)
  },
  { flush: 'post' },
)
</script>

<template>
  <slot />
  <ModemCallBanner :session="callSession" />
  <audio ref="remoteAudioRef" class="sr-only" autoplay playsinline />
</template>

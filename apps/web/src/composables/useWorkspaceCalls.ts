import { computed, nextTick, onMounted, provide, ref, useTemplateRef, watch } from 'vue'
import { useRoute } from 'vue-router'

import { provideModemCallSession } from '@/composables/useModemCallSession'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'

import { useCallMonitor } from '@/composables/useCallMonitor'
import { useContacts } from '@/composables/useContacts'
import { useCapabilities } from '@/composables/useCapabilities'
import { communicationDialKey } from '@/composables/useCommunicationDial'

/** Pins media to its accepted device while observing the rest of the backend. */
export const useWorkspaceCalls = () => {
  const route = useRoute()
  const activeModemId = ref('unknown')
  const reserving = ref(false)
  const modemId = computed(() => activeModemId.value)
  const { phoneCountry } = useModemPhoneCountry(modemId)

  const callSession = provideModemCallSession(modemId, phoneCountry)
  const hasLiveCall = computed(
    () =>
      reserving.value ||
      !!callSession.activeCall.value ||
      !!callSession.incomingCall.value ||
      callSession.isDialing.value,
  )
  watch(
    [() => route.params.id, hasLiveCall],
    ([value, busy]) => {
      if (!busy && typeof value === 'string' && value) activeModemId.value = value
    },
    { immediate: true },
  )
  provide(communicationDialKey, async (number, line) => {
    if (hasLiveCall.value) return false
    reserving.value = true
    try {
      activeModemId.value = line.modemId
      await nextTick()
      const call = await callSession.dial(number, 'auto', line.profileId)
      if (!call && callSession.errorMessage.value) throw new Error(callSession.errorMessage.value)
      return !!call
    } finally {
      reserving.value = false
    }
  })

  const contacts = useContacts()
  const capabilities = useCapabilities()
  onMounted(async () => {
    await capabilities.fetchCapabilities()
    if (capabilities.hasFeature('contacts')) {
      try {
        await contacts.load()
      } catch {
        /* Use numbers while offline. */
      }
    }
  })
  watch(hasLiveCall, async (active) => {
    if (!active) return
    await capabilities.fetchCapabilities()
    if (capabilities.hasFeature('contacts')) {
      try {
        await contacts.load()
      } catch {
        /* Keep the number visible. */
      }
    }
  })
  useCallMonitor(hasLiveCall, async (id, call) => {
    if (hasLiveCall.value) return
    reserving.value = true
    try {
      activeModemId.value = id
      await nextTick()
      callSession.observeCall(call)
    } finally {
      reserving.value = false
    }
  })

  const remoteAudioRef = useTemplateRef<HTMLAudioElement>('remoteAudioRef')
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

  return { callSession }
}

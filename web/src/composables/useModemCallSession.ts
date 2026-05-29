import { computed, inject, onBeforeUnmount, provide, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useCallAudioSession } from '@/composables/useCallAudioSession'
import { usePhoneCalls } from '@/composables/usePhoneCalls'
import { createBrowserAmrCodec, hasBrowserAmrCodec } from '@/lib/browserAmrCodec'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { CallRecord } from '@/types/call'

const liveDurationStates = new Set<CallRecord['state']>([
  'dialing',
  'ringing',
  'answering',
  'early_media',
  'active',
  'confirmed',
])

const mediaSessionStates = new Set<CallRecord['state']>(['early_media', 'active', 'confirmed'])
const terminalStates = new Set<CallRecord['state']>(['ended', 'failed'])

const modemCallSessionKey = Symbol('modem-call-session')

const createModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
) => {
  const { t } = useI18n()
  const phoneCalls = usePhoneCalls(modemId, defaultCountry)
  const callAudio = useCallAudioSession(modemId, { codecFactory: createBrowserAmrCodec })

  const durationTick = ref(Date.now())
  const audioCallID = ref('')
  let durationTimer: number | null = null

  const routeLabel = (value: string) => {
    switch (value) {
      case 'wifi_calling':
        return t('modemDetail.phone.routes.wifiCalling')
      case 'modem':
        return t('modemDetail.phone.routes.modem')
      default:
        return t('modemDetail.phone.routes.auto')
    }
  }

  const stateLabel = (value: string) => {
    switch (value) {
      case 'dialing':
        return t('modemDetail.phone.states.dialing')
      case 'ringing':
        return t('modemDetail.phone.states.ringing')
      case 'answering':
        return t('modemDetail.phone.states.answering')
      case 'early_media':
        return t('modemDetail.phone.states.earlyMedia')
      case 'active':
        return t('modemDetail.phone.states.active')
      case 'confirmed':
        return t('modemDetail.phone.states.confirmed')
      case 'ending':
        return t('modemDetail.phone.states.ending')
      case 'failed':
        return t('modemDetail.phone.states.failed')
      default:
        return t('modemDetail.phone.states.ended')
    }
  }

  const primaryLine = (call: CallRecord) =>
    formatPhoneDisplay(call.number, defaultCountry?.value) || t('modemDetail.phone.unknownNumber')

  const callStartedAt = (call: CallRecord) => Date.parse(call.answeredAt)

  const callEndedAt = (call: CallRecord) => {
    if (call.endedAt) return Date.parse(call.endedAt)
    if (liveDurationStates.has(call.state)) {
      return durationTick.value
    }
    return Date.parse(call.updatedAt)
  }

  const callDurationLabel = (call: CallRecord) => {
    const start = callStartedAt(call)
    if (!Number.isFinite(start)) return ''
    const end = callEndedAt(call)
    if (!Number.isFinite(start) || !Number.isFinite(end) || end < start)
      return t('modemDetail.phone.durationEmpty')
    const seconds = Math.max(0, Math.floor((end - start) / 1000))
    const minutes = Math.floor(seconds / 60)
    const remaining = seconds % 60
    if (minutes >= 60) {
      const hours = Math.floor(minutes / 60)
      const hourMinutes = minutes % 60
      return `${hours}:${String(hourMinutes).padStart(2, '0')}:${String(remaining).padStart(2, '0')}`
    }
    return `${minutes}:${String(remaining).padStart(2, '0')}`
  }

  const activeCallDurationLabel = computed(() =>
    phoneCalls.activeCall.value ? callDurationLabel(phoneCalls.activeCall.value) : '',
  )

  const audioMessage = computed(() => {
    if (callAudio.errorMessage.value) return callAudio.errorMessage.value
    const call = phoneCalls.activeCall.value
    if (
      call &&
      mediaSessionStates.has(call.state) &&
      call.route === 'wifi_calling' &&
      !hasBrowserAmrCodec()
    ) {
      return t('modemDetail.phone.audioCodecUnavailable')
    }
    return ''
  })

  const startDurationTimer = () => {
    durationTick.value = Date.now()
    if (durationTimer !== null) return
    durationTimer = window.setInterval(() => {
      durationTick.value = Date.now()
    }, 1000)
  }

  const stopDurationTimer = () => {
    if (durationTimer === null) return
    window.clearInterval(durationTimer)
    durationTimer = null
  }

  const answerIncoming = async (call: CallRecord) => {
    if (call.route === 'wifi_calling' && hasBrowserAmrCodec()) {
      const ready = await callAudio.prepare()
      if (!ready) return
    }
    await phoneCalls.answer(call)
  }

  watch(
    phoneCalls.activeCall,
    (call) => {
      if (call?.answeredAt && liveDurationStates.has(call.state)) {
        startDurationTimer()
      } else {
        stopDurationTimer()
      }

      if (
        call &&
        mediaSessionStates.has(call.state) &&
        call.route === 'wifi_calling' &&
        hasBrowserAmrCodec()
      ) {
        if (audioCallID.value === call.callID) return
        audioCallID.value = call.callID
        void callAudio.start(call.callID)
        return
      }
      if (audioCallID.value) {
        audioCallID.value = ''
        callAudio.stop()
      }
    },
    { immediate: true },
  )

  onBeforeUnmount(stopDurationTimer)

  return {
    ...phoneCalls,
    callAudio,
    routeLabel,
    stateLabel,
    primaryLine,
    callDurationLabel,
    activeCallDurationLabel,
    audioMessage,
    answerIncoming,
    terminalStates,
  }
}

export type ModemCallSession = ReturnType<typeof createModemCallSession>

export const provideModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
) => {
  const session = createModemCallSession(modemId, defaultCountry)
  provide(modemCallSessionKey, session)
  return session
}

export const useModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
) =>
  inject<ModemCallSession | null>(modemCallSessionKey, null) ??
  createModemCallSession(modemId, defaultCountry)

import {
  computed,
  inject,
  onBeforeUnmount,
  provide,
  ref,
  watch,
  type ComputedRef,
  type Ref,
} from 'vue'
import { useI18n } from 'vue-i18n'

import { useCallAudioSession } from '@/composables/useCallAudioSession'
import { usePhoneCalls } from '@/composables/usePhoneCalls'
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
const imsMediaRoutes = new Set<CallRecord['route']>(['wifi_calling', 'volte'])

const localHoldStates = new Set<CallRecord['hold']>(['local', 'local_remote'])
const remoteHoldStates = new Set<CallRecord['hold']>(['remote', 'local_remote'])

const modemCallSessionKey = Symbol('modem-call-session')

const createModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) => {
  const { t } = useI18n()
  const sessionSearchQuery = ref(searchQuery?.value ?? '')
  if (searchQuery) {
    watch(
      searchQuery,
      (value) => {
        sessionSearchQuery.value = value.trim()
      },
      { immediate: true },
    )
  }
  const setSearchQuery = (value: string) => {
    sessionSearchQuery.value = value.trim()
  }
  const phoneCalls = usePhoneCalls(modemId, defaultCountry, sessionSearchQuery)
  const callAudio = useCallAudioSession(modemId)

  const durationTick = ref(Date.now())
  const audioCallID = ref('')
  let durationTimer: number | null = null

  const routeLabel = (value: string) => {
    switch (value) {
      case 'wifi_calling':
        return t('modemDetail.phone.routes.wifiCalling')
      case 'volte':
        return t('modemDetail.phone.routes.volte')
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

  const holdLabel = (value: string) => {
    switch (value) {
      case 'local':
        return t('modemDetail.phone.holdStates.local')
      case 'remote':
        return t('modemDetail.phone.holdStates.remote')
      case 'local_remote':
        return t('modemDetail.phone.holdStates.localRemote')
      default:
        return ''
    }
  }

  const isLocallyHeld = (call: CallRecord | null) => !!call && localHoldStates.has(call.hold)
  const isRemotelyHeld = (call: CallRecord | null) => !!call && remoteHoldStates.has(call.hold)

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
    if (imsMediaRoutes.has(call.route)) {
      const ready = await callAudio.prepare()
      if (!ready) return
    }
    await phoneCalls.answer(call)
  }

  const toggleHold = async (call: CallRecord) => {
    await phoneCalls.toggleHold(call)
  }

  watch(
    phoneCalls.activeCall,
    (call) => {
      if (call?.answeredAt && liveDurationStates.has(call.state)) {
        startDurationTimer()
      } else {
        stopDurationTimer()
      }

      if (call && mediaSessionStates.has(call.state) && imsMediaRoutes.has(call.route)) {
        callAudio.setInputEnabled(!localHoldStates.has(call.hold) && !remoteHoldStates.has(call.hold))
        if (audioCallID.value === call.callID) return
        audioCallID.value = call.callID
        void Promise.resolve(callAudio.start(call.callID)).then(() => {
          const current = phoneCalls.activeCall.value
          if (current?.callID !== call.callID) return
          callAudio.setInputEnabled(
            !localHoldStates.has(current.hold) && !remoteHoldStates.has(current.hold),
          )
        })
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
    holdLabel,
    isLocallyHeld,
    isRemotelyHeld,
    primaryLine,
    callDurationLabel,
    activeCallDurationLabel,
    audioMessage,
    answerIncoming,
    toggleHold,
    setSearchQuery,
    terminalStates,
  }
}

export type ModemCallSession = ReturnType<typeof createModemCallSession>

export const provideModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) => {
  const session = createModemCallSession(modemId, defaultCountry, searchQuery)
  provide(modemCallSessionKey, session)
  return session
}

export const useModemCallSession = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) =>
  inject<ModemCallSession | null>(modemCallSessionKey, null) ??
  createModemCallSession(modemId, defaultCountry, searchQuery)

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

import { useCallLabels } from '@/composables/useCallLabels'
import { useCallAudioSession } from '@/composables/useCallAudioSession'
import { useCallRecording } from '@/composables/useCallRecording'
import { useContacts } from '@/composables/useContacts'
import { useCalls } from '@/composables/useCalls'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import { callInUseElsewhere, ownsCall } from '@/lib/callClient'
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
  const contacts = useContacts()
  const calls = useCalls(modemId, defaultCountry, sessionSearchQuery)
  const callAudio = useCallAudioSession(modemId)
  const recordingCallId = computed(() => {
    const call = calls.activeCall.value
    return call &&
      ownsCall(call) &&
      imsMediaRoutes.has(call.route) &&
      ['active', 'confirmed'].includes(call.state)
      ? call.callID
      : ''
  })
  const callRecording = useCallRecording({
    callId: recordingCallId,
    audioReady: callAudio.isReady,
    localStream: callAudio.localStream,
    remoteStream: callAudio.remoteStream,
  })

  const durationTick = ref(Date.now())
  const audioCallID = ref('')
  const preparedIncomingCallID = ref('')
  let durationTimer: number | null = null

  const { routeLabel, stateLabel, callDurationLabel } = useCallLabels(() => durationTick.value)

  const holdLabel = (value: string) => {
    switch (value) {
      case 'local':
        return t('modemDetail.calls.holdStates.local')
      case 'remote':
        return t('modemDetail.calls.holdStates.remote')
      case 'local_remote':
        return t('modemDetail.calls.holdStates.localRemote')
      default:
        return ''
    }
  }

  const isLocallyHeld = (call: CallRecord | null) => !!call && localHoldStates.has(call.hold)
  const isRemotelyHeld = (call: CallRecord | null) => !!call && remoteHoldStates.has(call.hold)
  const usesBrowserAudio = (call: CallRecord | null) => !!call && imsMediaRoutes.has(call.route)

  const primaryLine = (call: CallRecord) =>
    contacts.match(call.number)?.name ||
    formatPhoneDisplay(call.number, defaultCountry?.value) ||
    t('modemDetail.calls.unknownNumber')

  const activeCallDurationLabel = computed(() =>
    calls.activeCall.value ? callDurationLabel(calls.activeCall.value) : '',
  )

  const audioMessage = computed(() => {
    if (callAudio.errorMessage.value) return callAudio.errorMessage.value
    switch (callAudio.deviceNotice.value) {
      case 'list_failed':
        return t('modemDetail.calls.audioDevices.listFailed')
      case 'input_switch_failed':
        return t('modemDetail.calls.audioDevices.inputSwitchFailed')
      case 'output_switch_failed':
        return t('modemDetail.calls.audioDevices.outputSwitchFailed')
      case 'device_fallback':
        return t('modemDetail.calls.audioDevices.deviceFallback')
      default:
        return ''
    }
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

  const answerIncoming = async (call: CallRecord) => await calls.answer(call)

  const prepareAudioDevices = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return false
    if (!usesBrowserAudio(call)) return false
    const ready = await callAudio.prepare()
    if (!ready) return false
    const callStillExists =
      calls.incomingCall.value?.callID === call.callID ||
      calls.activeCall.value?.callID === call.callID
    if (!callStillExists) {
      callAudio.stop()
      return false
    }
    if (call.direction === 'incoming') {
      preparedIncomingCallID.value = call.callID
    }
    return true
  }

  const toggleHold = async (call: CallRecord) => {
    await calls.toggleHold(call)
  }

  watch(
    calls.activeCall,
    (call) => {
      let audioStopped = false
      if (call?.answeredAt && liveDurationStates.has(call.state)) {
        startDurationTimer()
      } else {
        stopDurationTimer()
      }

      if (
        call &&
        ownsCall(call) &&
        mediaSessionStates.has(call.state) &&
        imsMediaRoutes.has(call.route)
      ) {
        callAudio.setInputEnabled(
          !localHoldStates.has(call.hold) && !remoteHoldStates.has(call.hold),
        )
        if (audioCallID.value === call.callID) return
        audioCallID.value = call.callID
        void Promise.resolve(callAudio.start(call.callID)).then(() => {
          const current = calls.activeCall.value
          if (current?.callID !== call.callID || !ownsCall(current)) return
          callAudio.setInputEnabled(
            !localHoldStates.has(current.hold) && !remoteHoldStates.has(current.hold),
          )
        })
        return
      }
      if (audioCallID.value) {
        audioCallID.value = ''
        callAudio.stop()
        audioStopped = true
      }
      if (preparedIncomingCallID.value && (!call || call.callID !== preparedIncomingCallID.value)) {
        preparedIncomingCallID.value = ''
        if (!audioStopped) callAudio.stop()
      }
    },
    { immediate: true },
  )

  watch(calls.incomingCall, (call, previous) => {
    if (!previous || previous.callID !== preparedIncomingCallID.value) return
    if (call?.callID === previous.callID) return
    queueMicrotask(() => {
      if (preparedIncomingCallID.value !== previous.callID) return
      if (calls.activeCall.value?.callID === previous.callID) return
      preparedIncomingCallID.value = ''
      callAudio.stop()
    })
  })

  onBeforeUnmount(stopDurationTimer)

  return {
    ...calls,
    callAudio,
    callRecording,
    routeLabel,
    stateLabel,
    holdLabel,
    isLocallyHeld,
    isRemotelyHeld,
    usesBrowserAudio,
    observeCall: calls.observeCall,
    primaryLine,
    callDurationLabel,
    activeCallDurationLabel,
    audioMessage,
    answerIncoming,
    prepareAudioDevices,
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

import { computed, onBeforeUnmount, ref, watch, type ComputedRef, type Ref } from 'vue'

import { buildCallEventsUrl, useCallApi } from '@/apis/call'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import { callClientId, callInUseElsewhere } from '@/lib/callClient'
import { hasWebPushSubscription } from '@/lib/webPush'
import type { CallEventMessage, CallRecord, CallRoute } from '@/types/call'

const terminalStates = new Set(['ended', 'failed'])
const currentStates = new Set([
  'dialing',
  'ringing',
  'answering',
  'early_media',
  'active',
  'confirmed',
  'ending',
])

const normalizeCall = (call: CallRecord): CallRecord => ({
  ...call,
  hold: call.hold || 'none',
})

const mergeCall = (items: CallRecord[], call: CallRecord) => {
  call = normalizeCall(call)
  const index = items.findIndex((item) => item.callID === call.callID)
  if (index === -1) {
    return [call, ...items]
  }
  const previous = items[index]
  if (
    previous &&
    ((terminalStates.has(previous.state) && !terminalStates.has(call.state)) ||
      Date.parse(previous.updatedAt) > Date.parse(call.updatedAt))
  )
    return items
  const next = items.slice()
  next[index] = call
  return next.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
}

export const useCalls = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) => {
  const callApi = useCallApi()
  const calls = ref<CallRecord[]>([])
  const liveCalls = ref<CallRecord[]>([])
  const isLoading = ref(false)
  const isDialing = ref(false)
  const isDeletingCallID = ref('')
  const errorMessage = ref('')

  let ws: WebSocket | null = null
  let ringContext: AudioContext | null = null
  let ringTimer: number | null = null
  let eventReconnectTimer: number | null = null
  let eventReconnectAttempt = 0
  let loadRequestID = 0
  let connectionRevision = 0
  let dtmfQueue = Promise.resolve()
  const incomingNotifications = new Map<string, Notification>()
  const endedCalls = new Map<string, CallRecord>()

  const recentCalls = computed(() => calls.value)
  const hasRecentCalls = computed(() => recentCalls.value.length > 0)
  const incomingCall = computed(
    () =>
      liveCalls.value.find(
        (call) =>
          call.state === 'ringing' && call.direction === 'incoming' && !callInUseElsewhere(call),
      ) ?? null,
  )
  const activeCall = computed(
    () =>
      liveCalls.value.find(
        (call) =>
          currentStates.has(call.state) &&
          !(call.state === 'ringing' && call.direction === 'incoming'),
      ) ?? null,
  )

  const currentSearchQuery = () => searchQuery?.value.trim() ?? ''

  const phoneDigits = (value: string) => {
    let result = ''
    for (const char of value) {
      if (char >= '0' && char <= '9') {
        result += char
      }
    }
    return result
  }

  const callMatchesSearch = (call: CallRecord) => {
    const query = currentSearchQuery()
    if (!query) return true
    const number = call.number.toLocaleLowerCase()
    const normalizedQuery = query.toLocaleLowerCase()
    if (number.includes(normalizedQuery)) return true
    const queryDigits = phoneDigits(query)
    return queryDigits.length > 0 && phoneDigits(call.number).includes(queryDigits)
  }

  const closeEvents = () => {
    if (eventReconnectTimer !== null) {
      window.clearTimeout(eventReconnectTimer)
      eventReconnectTimer = null
    }
    eventReconnectAttempt = 0
    if (!ws) return
    const current = ws
    ws = null
    current.onclose = null
    current.close()
  }

  const scheduleEventsReconnect = () => {
    const id = modemId.value
    if (!id || id === 'unknown' || eventReconnectTimer !== null) return
    const delay = Math.min(1000 * 2 ** eventReconnectAttempt, 15000)
    eventReconnectAttempt++
    eventReconnectTimer = window.setTimeout(() => {
      eventReconnectTimer = null
      openEvents(false)
    }, delay)
  }

  const stopRing = () => {
    if (ringTimer !== null) {
      window.clearInterval(ringTimer)
      ringTimer = null
    }
    if (ringContext) {
      void ringContext.close()
      ringContext = null
    }
  }

  const closeIncomingNotification = (callID: string) => {
    const notification = incomingNotifications.get(callID)
    if (!notification) return
    incomingNotifications.delete(callID)
    notification.close()
  }

  const closeIncomingNotifications = () => {
    for (const notification of incomingNotifications.values()) {
      notification.close()
    }
    incomingNotifications.clear()
  }

  const ringOnce = () => {
    try {
      ringContext ??= new AudioContext()
      const oscillator = ringContext.createOscillator()
      const gain = ringContext.createGain()
      oscillator.type = 'sine'
      oscillator.frequency.value = 880
      gain.gain.value = 0.08
      oscillator.connect(gain)
      gain.connect(ringContext.destination)
      oscillator.start()
      oscillator.stop(ringContext.currentTime + 0.18)
    } catch (err) {
      console.warn('[useCalls] play ringtone:', err)
      stopRing()
    }
  }

  const startRing = () => {
    if (ringTimer !== null) return
    ringOnce()
    ringTimer = window.setInterval(ringOnce, 1400)
  }

  const maybeNotifyIncoming = (call: CallRecord) => {
    if (hasWebPushSubscription.value) return
    if (!('Notification' in window) || Notification.permission !== 'granted') return
    if (incomingNotifications.has(call.callID)) return
    try {
      const notification = new Notification('Incoming call', {
        body: formatPhoneDisplay(call.number, defaultCountry?.value),
        tag: call.callID,
      })
      incomingNotifications.set(call.callID, notification)
      notification.onclick = () => {
        window.focus()
      }
      notification.onclose = () => {
        incomingNotifications.delete(call.callID)
      }
    } catch (err) {
      console.warn('[useCalls] show incoming notification:', err)
    }
  }

  const setCallState = (call: CallRecord) => {
    call = normalizeCall(call)
    const previous =
      endedCalls.get(call.callID) ?? liveCalls.value.find((item) => item.callID === call.callID)
    if (
      previous &&
      ((terminalStates.has(previous.state) && !terminalStates.has(call.state)) ||
        Date.parse(previous.updatedAt) > Date.parse(call.updatedAt))
    )
      return
    if (terminalStates.has(call.state)) {
      endedCalls.set(call.callID, call)
      if (endedCalls.size > 1000) {
        const oldest = endedCalls.keys().next().value
        if (oldest) endedCalls.delete(oldest)
      }
    }
    const previousIncoming = incomingCall.value
    const previousActive = activeCall.value
    calls.value = callMatchesSearch(call)
      ? mergeCall(calls.value, call)
      : calls.value.filter((item) => item.callID !== call.callID)
    liveCalls.value = mergeCall(liveCalls.value, call)
    if (call.state === 'ringing' && call.direction === 'incoming' && !callInUseElsewhere(call)) {
      startRing()
      maybeNotifyIncoming(call)
      return
    }
    if (
      previousIncoming?.callID === call.callID &&
      (call.state !== 'ringing' || callInUseElsewhere(call))
    ) {
      stopRing()
      closeIncomingNotification(call.callID)
    }
    if (previousActive?.callID === call.callID && terminalStates.has(call.state)) {
      stopRing()
      closeIncomingNotification(call.callID)
    }
  }

  const seedLiveCalls = (items: CallRecord[]) => {
    liveCalls.value = items.map((call) => {
      const old =
        endedCalls.get(call.callID) ?? liveCalls.value.find((item) => item.callID === call.callID)
      if (
        old &&
        ((terminalStates.has(old.state) && !terminalStates.has(call.state)) ||
          Date.parse(old.updatedAt) > Date.parse(call.updatedAt))
      )
        return old
      if (terminalStates.has(call.state)) endedCalls.set(call.callID, call)
      return call
    })
    const ringingCall = incomingCall.value
    if (ringingCall) {
      startRing()
      maybeNotifyIncoming(ringingCall)
      return
    }
    stopRing()
    closeIncomingNotifications()
  }

  const loadCalls = async () => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    const query = currentSearchQuery()
    const currentRequestID = ++loadRequestID
    isLoading.value = true
    errorMessage.value = ''
    try {
      const { data } = await callApi.listCalls(id, query)
      const nextCalls = (data.value ?? []).map(normalizeCall)
      if (currentRequestID !== loadRequestID) return
      calls.value = nextCalls
      if (!query) {
        seedLiveCalls(nextCalls)
        return
      }
      for (const call of nextCalls) {
        if (currentStates.has(call.state)) {
          setCallState(call)
        }
      }
    } catch (err) {
      if (currentRequestID !== loadRequestID) return
      errorMessage.value = err instanceof Error ? err.message : 'Loading calls failed'
    } finally {
      if (currentRequestID === loadRequestID) {
        isLoading.value = false
      }
    }
  }

  const openEvents = (resetReconnectAttempt = true) => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    const currentReconnectAttempt = eventReconnectAttempt
    closeEvents()
    if (!resetReconnectAttempt) {
      eventReconnectAttempt = currentReconnectAttempt
    }
    const conn = new WebSocket(buildCallEventsUrl(id))
    ws = conn
    conn.onopen = () => {
      if (eventReconnectAttempt > 0) void loadCalls()
      eventReconnectAttempt = 0
    }
    conn.onmessage = (event) => {
      if (ws !== conn) return
      let message: CallEventMessage
      try {
        message = JSON.parse(event.data) as CallEventMessage
      } catch (err) {
        console.error('[useCalls] parse event:', err)
        return
      }
      if (message.type !== 'call') return
      setCallState(message.call)
    }
    conn.onclose = () => {
      if (ws !== conn) return
      ws = null
      scheduleEventsReconnect()
    }
  }

  const dial = async (number: string, route: CallRoute = 'auto', expectedProfileId?: string) => {
    const id = modemId.value
    if (!id || id === 'unknown' || isDialing.value) return null
    const revision = connectionRevision
    isDialing.value = true
    errorMessage.value = ''
    try {
      const { data } = await callApi.dialCall(id, {
        to: number,
        route,
        ...(expectedProfileId ? { expectedProfileId } : {}),
      })
      if (revision !== connectionRevision) return null
      if (data.value) {
        setCallState(data.value)
      }
      return data.value ?? null
    } catch (err) {
      if (revision !== connectionRevision) return null
      errorMessage.value = err instanceof Error ? err.message : 'Dial failed'
      // A lost HTTP response may hide an accepted dial. Reconcile, never replay.
      try {
        const { data } = await callApi.listCalls(id)
        if (revision !== connectionRevision) return null
        const accepted = data.value?.find(
          (call) =>
            call.ownerClientId === callClientId() &&
            call.number === number &&
            !terminalStates.has(call.state),
        )
        if (accepted) {
          setCallState(accepted)
          errorMessage.value = ''
          return accepted
        }
      } catch {
        /* Keep the original uncertain result visible. */
      }
      return null
    } finally {
      if (revision === connectionRevision) isDialing.value = false
    }
  }

  const answer = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return
    const id = modemId.value
    if (!id || id === 'unknown') return
    const revision = connectionRevision
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.answerCall(id, call.callID)
      if (revision !== connectionRevision) return
      if (data.value) setCallState(data.value)
    } catch (err) {
      if (revision !== connectionRevision) return
      errorMessage.value = err instanceof Error ? err.message : 'Answer failed'
    }
  }

  const reject = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return
    const id = modemId.value
    if (!id || id === 'unknown') return
    const revision = connectionRevision
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.rejectCall(id, call.callID)
      if (revision !== connectionRevision) return
      if (data.value) setCallState(data.value)
    } catch (err) {
      if (revision !== connectionRevision) return
      errorMessage.value = err instanceof Error ? err.message : 'Reject failed'
    }
  }

  const hangup = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return
    const id = modemId.value
    if (!id || id === 'unknown') return
    const revision = connectionRevision
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.hangupCall(id, call.callID)
      if (revision !== connectionRevision) return
      if (data.value) setCallState(data.value)
    } catch (err) {
      if (revision !== connectionRevision) return
      errorMessage.value = err instanceof Error ? err.message : 'Hang up failed'
    }
  }

  const hold = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return
    const id = modemId.value
    if (!id || id === 'unknown') return
    const revision = connectionRevision
    errorMessage.value = ''
    try {
      const { data } = await callApi.holdCall(id, call.callID)
      if (revision !== connectionRevision) return
      if (data.value) setCallState(data.value)
    } catch (err) {
      if (revision !== connectionRevision) return
      errorMessage.value = err instanceof Error ? err.message : 'Hold failed'
    }
  }

  const resume = async (call: CallRecord) => {
    if (callInUseElsewhere(call)) return
    const id = modemId.value
    if (!id || id === 'unknown') return
    const revision = connectionRevision
    errorMessage.value = ''
    try {
      const { data } = await callApi.resumeCall(id, call.callID)
      if (revision !== connectionRevision) return
      if (data.value) setCallState(data.value)
    } catch (err) {
      if (revision !== connectionRevision) return
      errorMessage.value = err instanceof Error ? err.message : 'Resume failed'
    }
  }

  const toggleHold = async (call: CallRecord) => {
    if (call.hold === 'local' || call.hold === 'local_remote') {
      await resume(call)
      return
    }
    await hold(call)
  }

  const sendDTMF = (call: CallRecord, digits: string) => {
    if (callInUseElsewhere(call)) return Promise.resolve(false)
    const id = modemId.value
    if (!id || id === 'unknown') return Promise.resolve(false)
    const callID = call.callID
    const revision = connectionRevision
    const task = dtmfQueue
      .catch(() => undefined)
      .then(async () => {
        if (revision !== connectionRevision || endedCalls.has(callID)) return false
        const current = liveCalls.value.find((item) => item.callID === callID)
        if (current && (callInUseElsewhere(current) || (current.hold && current.hold !== 'none')))
          return false
        errorMessage.value = ''
        try {
          await callApi.sendDTMF(id, callID, { digits })
          return revision === connectionRevision
        } catch (err) {
          if (revision !== connectionRevision) return false
          errorMessage.value = err instanceof Error ? err.message : 'DTMF failed'
          return false
        }
      })
    dtmfQueue = task.then(
      () => undefined,
      () => undefined,
    )
    return task
  }

  const deleteRecord = async (call: CallRecord) => {
    const id = modemId.value
    if (!id || id === 'unknown' || isDeletingCallID.value) return false
    const revision = connectionRevision
    isDeletingCallID.value = call.callID
    errorMessage.value = ''
    try {
      const wasIncoming = incomingCall.value?.callID === call.callID
      await callApi.deleteCall(id, call.callID)
      if (revision !== connectionRevision) return false
      calls.value = calls.value.filter((item) => item.callID !== call.callID)
      liveCalls.value = liveCalls.value.filter((item) => item.callID !== call.callID)
      if (wasIncoming) {
        stopRing()
        closeIncomingNotification(call.callID)
      }
      return true
    } catch (err) {
      if (revision !== connectionRevision) return false
      errorMessage.value = err instanceof Error ? err.message : 'Delete failed'
      return false
    } finally {
      if (revision === connectionRevision) isDeletingCallID.value = ''
    }
  }

  watch(
    modemId,
    () => {
      connectionRevision++
      loadRequestID++
      isLoading.value = false
      isDialing.value = false
      isDeletingCallID.value = ''
      errorMessage.value = ''
      dtmfQueue = Promise.resolve()
      calls.value = []
      liveCalls.value = []
      endedCalls.clear()
      stopRing()
      closeIncomingNotifications()
      closeEvents()
      void loadCalls()
      openEvents()
    },
    { immediate: true },
  )

  watch(
    () => currentSearchQuery(),
    () => {
      calls.value = []
      void loadCalls()
    },
  )

  onBeforeUnmount(() => {
    connectionRevision++
    loadRequestID++
    closeEvents()
    stopRing()
    closeIncomingNotifications()
  })

  return {
    observeCall: setCallState,
    recentCalls,
    hasRecentCalls,
    activeCall,
    incomingCall,
    isLoading,
    isDialing,
    isDeletingCallID,
    errorMessage,
    dial,
    answer,
    reject,
    hangup,
    hold,
    resume,
    toggleHold,
    sendDTMF,
    deleteRecord,
    loadCalls,
  }
}

import { computed, onBeforeUnmount, ref, watch, type ComputedRef } from 'vue'

import { buildCallEventsUrl, useCallApi } from '@/apis/call'
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

const mergeCall = (items: CallRecord[], call: CallRecord) => {
  const index = items.findIndex((item) => item.callID === call.callID)
  if (index === -1) {
    return [call, ...items]
  }
  const next = items.slice()
  next[index] = call
  return next.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
}

export const usePhoneCalls = (modemId: ComputedRef<string>) => {
  const callApi = useCallApi()
  const calls = ref<CallRecord[]>([])
  const activeCall = ref<CallRecord | null>(null)
  const incomingCall = ref<CallRecord | null>(null)
  const isLoading = ref(false)
  const isDialing = ref(false)
  const errorMessage = ref('')

  let ws: WebSocket | null = null
  let ringContext: AudioContext | null = null
  let ringTimer: number | null = null
  let eventReconnectTimer: number | null = null
  let eventReconnectAttempt = 0
  const incomingNotifications = new Map<string, Notification>()

  const recentCalls = computed(() => calls.value)
  const hasRecentCalls = computed(() => recentCalls.value.length > 0)

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
      console.warn('[usePhoneCalls] play ringtone:', err)
      stopRing()
    }
  }

  const startRing = () => {
    if (ringTimer !== null) return
    ringOnce()
    ringTimer = window.setInterval(ringOnce, 1400)
  }

  const maybeNotifyIncoming = (call: CallRecord) => {
    if (!('Notification' in window) || Notification.permission !== 'granted') return
    if (incomingNotifications.has(call.callID)) return
    try {
      const notification = new Notification('Incoming call', {
        body: call.number,
        tag: call.callID,
      })
      incomingNotifications.set(call.callID, notification)
      notification.onclick = () => {
        window.focus()
        incomingCall.value = call
      }
      notification.onclose = () => {
        incomingNotifications.delete(call.callID)
      }
    } catch (err) {
      console.warn('[usePhoneCalls] show incoming notification:', err)
    }
  }

  const requestNotificationPermission = async () => {
    if (!('Notification' in window) || Notification.permission !== 'default') return
    try {
      await Notification.requestPermission()
    } catch (err) {
      console.error('[usePhoneCalls] notification permission:', err)
    }
  }

  const setCallState = (call: CallRecord) => {
    calls.value = mergeCall(calls.value, call)
    if (call.state === 'ringing' && call.direction === 'incoming') {
      incomingCall.value = call
      startRing()
      maybeNotifyIncoming(call)
      return
    }
    if (activeCall.value?.callID === call.callID || currentStates.has(call.state)) {
      activeCall.value = call
    }
    if (incomingCall.value?.callID === call.callID && call.state !== 'ringing') {
      incomingCall.value = null
      stopRing()
      closeIncomingNotification(call.callID)
    }
    if (activeCall.value?.callID === call.callID && terminalStates.has(call.state)) {
      activeCall.value = null
      stopRing()
      closeIncomingNotification(call.callID)
    }
  }

  const loadCalls = async () => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    isLoading.value = true
    errorMessage.value = ''
    try {
      const { data } = await callApi.listCalls(id)
      calls.value = data.value ?? []
      const currentCall = calls.value.find((call) => currentStates.has(call.state)) ?? null
      if (currentCall?.state === 'ringing' && currentCall.direction === 'incoming') {
        setCallState(currentCall)
      } else {
        activeCall.value = currentCall
        incomingCall.value = null
        stopRing()
        closeIncomingNotifications()
      }
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Loading calls failed'
    } finally {
      isLoading.value = false
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
      eventReconnectAttempt = 0
    }
    conn.onmessage = (event) => {
      if (ws !== conn) return
      let message: CallEventMessage
      try {
        message = JSON.parse(event.data) as CallEventMessage
      } catch (err) {
        console.error('[usePhoneCalls] parse event:', err)
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

  const dial = async (number: string, route: CallRoute = 'auto') => {
    const id = modemId.value
    if (!id || id === 'unknown') return null
    isDialing.value = true
    errorMessage.value = ''
    try {
      const { data } = await callApi.dialCall(id, { to: number, route })
      if (data.value) {
        setCallState(data.value)
      }
      void requestNotificationPermission()
      return data.value ?? null
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Dial failed'
      return null
    } finally {
      isDialing.value = false
    }
  }

  const answer = async (call: CallRecord) => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.answerCall(id, call.callID)
      if (data.value) setCallState(data.value)
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Answer failed'
    }
  }

  const reject = async (call: CallRecord) => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.rejectCall(id, call.callID)
      if (data.value) setCallState(data.value)
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Reject failed'
    }
  }

  const hangup = async (call: CallRecord) => {
    const id = modemId.value
    if (!id || id === 'unknown') return
    stopRing()
    errorMessage.value = ''
    try {
      const { data } = await callApi.hangupCall(id, call.callID)
      if (data.value) setCallState(data.value)
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Hang up failed'
    }
  }

  watch(
    modemId,
    () => {
      calls.value = []
      activeCall.value = null
      incomingCall.value = null
      stopRing()
      closeIncomingNotifications()
      void loadCalls()
      openEvents()
    },
    { immediate: true },
  )

  onBeforeUnmount(() => {
    closeEvents()
    stopRing()
    closeIncomingNotifications()
  })

  return {
    recentCalls,
    hasRecentCalls,
    activeCall,
    incomingCall,
    isLoading,
    isDialing,
    errorMessage,
    dial,
    answer,
    reject,
    hangup,
    loadCalls,
  }
}

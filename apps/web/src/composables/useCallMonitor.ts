import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'
import { buildCallEventsUrl } from '@/apis/call'
import { useModemApi } from '@/apis/modem'
import { useCapabilities } from '@/composables/useCapabilities'
import { sessionRevision } from '@/lib/authSession'
import { callInUseElsewhere } from '@/lib/callClient'
import type { CallRecord } from '@/types/call'

const isIncomingCall = (value: object): value is CallRecord =>
  'callID' in value &&
  typeof value.callID === 'string' &&
  'state' in value &&
  value.state === 'ringing' &&
  'direction' in value &&
  value.direction === 'incoming' &&
  'number' in value &&
  typeof value.number === 'string' &&
  'route' in value &&
  (value.route === 'wifi_calling' || value.route === 'volte' || value.route === 'modem') &&
  'hold' in value &&
  (value.hold === 'none' ||
    value.hold === 'local' ||
    value.hold === 'remote' ||
    value.hold === 'local_remote') &&
  'reason' in value &&
  typeof value.reason === 'string' &&
  'startedAt' in value &&
  typeof value.startedAt === 'string' &&
  'answeredAt' in value &&
  typeof value.answeredAt === 'string' &&
  'endedAt' in value &&
  typeof value.endedAt === 'string' &&
  'updatedAt' in value &&
  typeof value.updatedAt === 'string' &&
  (!('ownerClientId' in value) || typeof value.ownerClientId === 'string') &&
  (!('profileId' in value) || typeof value.profileId === 'string') &&
  (!('modemId' in value) || typeof value.modemId === 'string') &&
  (!('backendId' in value) || typeof value.backendId === 'string') &&
  (!('callUUID' in value) || typeof value.callUUID === 'string') &&
  (!('historyProfileId' in value) || typeof value.historyProfileId === 'string')

/** Observes all devices without creating or moving a browser audio session. */
export const useCallMonitor = (
  busy: Readonly<Ref<boolean>>,
  receive: (modemID: string, call: CallRecord) => Promise<void>,
) => {
  const sockets = new Map<string, WebSocket>()
  const ringing = new Map<string, { modemID: string; call: CallRecord }>()
  const ended = new Set<string>()
  const capabilities = useCapabilities()
  let stopped = false
  let refreshing = false
  let timer: ReturnType<typeof setInterval> | undefined
  const deliver = async () => {
    if (stopped || busy.value) return
    const next = ringing.values().next().value
    if (next) await receive(next.modemID, next.call)
  }
  const forgetModem = (id: string) => {
    for (const [callID, value] of ringing) {
      if (value.modemID === id) ringing.delete(callID)
    }
  }
  const refresh = async () => {
    if (stopped || refreshing) return
    refreshing = true
    const revision = sessionRevision()
    try {
      await capabilities.fetchCapabilities()
      if (!capabilities.hasFeature('callHistory')) return
      const { data } = await useModemApi().getModems()
      if (stopped || revision !== sessionRevision()) return
      const ids = new Set((data.value ?? []).map((modem) => modem.id))
      for (const [id, socket] of sockets) {
        if (!ids.has(id)) {
          socket.close()
          sockets.delete(id)
          forgetModem(id)
        }
      }
      for (const id of ids) {
        if (sockets.has(id)) continue
        const socket = new WebSocket(buildCallEventsUrl(id))
        sockets.set(id, socket)
        socket.onmessage = (event: MessageEvent<string>) => {
          if (stopped || sockets.get(id) !== socket) return
          let value: unknown
          try {
            value = JSON.parse(event.data)
          } catch {
            return
          }
          if (
            !value ||
            typeof value !== 'object' ||
            !('type' in value) ||
            value.type !== 'call' ||
            !('call' in value)
          )
            return
          const call = value.call
          if (
            !call ||
            typeof call !== 'object' ||
            !('callID' in call) ||
            typeof call.callID !== 'string' ||
            !('state' in call)
          )
            return
          if (call.state === 'ended' || call.state === 'failed') {
            ended.add(call.callID)
            if (ended.size > 1000) {
              const first = ended.values().next().value
              if (first) ended.delete(first)
            }
          }
          ringing.delete(call.callID)
          if (!ended.has(call.callID) && isIncomingCall(call) && !callInUseElsewhere(call)) {
            ringing.set(call.callID, { modemID: id, call })
          }
          window.dispatchEvent(new Event('sigmo:communication-changed'))
          void deliver()
        }
        socket.onclose = () => {
          if (sockets.get(id) === socket) {
            sockets.delete(id)
            forgetModem(id)
          }
        }
      }
    } catch {
      /* The next inventory refresh reconnects unavailable devices. */
    } finally {
      refreshing = false
    }
  }
  watch(
    sessionRevision,
    () => {
      for (const socket of sockets.values()) socket.close()
      sockets.clear()
      ringing.clear()
      ended.clear()
    },
    { flush: 'sync' },
  )
  watch(busy, () => {
    void deliver()
  })
  onMounted(() => {
    void refresh()
    timer = setInterval(() => {
      void refresh()
    }, 10_000)
  })
  onBeforeUnmount(() => {
    stopped = true
    clearInterval(timer)
    for (const socket of sockets.values()) socket.close()
    sockets.clear()
    ringing.clear()
  })
}

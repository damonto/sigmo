import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { decideEsimTransferRequest } from '@/apis/esimPeer'
import { useCapabilities } from '@/composables/useCapabilities'
import { resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { sessionRevision } from '@/lib/authSession'
import {
  isTransferApproval,
  parseTransferRequests,
  type EsimTransferRequest,
  type EsimTransferSelection,
} from '@/types/esimPeer'

/** Observes approvals for the current login and owns its subscription lifetime. */
export const useEsimTransferRequests = () => {
  const capabilities = useCapabilities()
  const requests = ref<EsimTransferRequest[]>([])
  const busy = ref(false)
  const error = ref('')
  const dismissed = new Set<string>()
  let socket: WebSocket | null = null
  let timer: ReturnType<typeof setInterval> | undefined
  let stopped = false
  let activeDecision: symbol | undefined

  const current = computed(() => {
    const visible = requests.value.filter((request) => !dismissed.has(request.id))
    return (
      visible.find(isTransferApproval) ??
      visible.find((request) => request.phase === 'running' || request.phase === 'ended')
    )
  })

  const close = () => {
    const previous = socket
    socket = null
    previous?.close()
    requests.value = []
    activeDecision = undefined
    busy.value = false
    error.value = ''
  }

  const connect = () => {
    if (stopped || socket || !capabilities.hasFeature('esimTransfer')) return
    const connection = new WebSocket(resolveAPIWebSocketURL('esim-transfer-requests/events'))
    socket = connection
    connection.onmessage = (event: MessageEvent<string>) => {
      if (socket !== connection || stopped) return
      try {
        const value: unknown = JSON.parse(event.data)
        requests.value = parseTransferRequests(value)
        const liveIDs = new Set(requests.value.map((request) => request.id))
        for (const id of dismissed) if (!liveIDs.has(id)) dismissed.delete(id)
      } catch {
        close()
      }
    }
    const disconnected = () => {
      if (socket === connection) close()
    }
    connection.onclose = disconnected
    connection.onerror = disconnected
  }

  const decide = async (
    request: EsimTransferRequest,
    accept: boolean,
    selection?: EsimTransferSelection,
  ) => {
    if (stopped || busy.value || !isTransferApproval(request) || (accept && !selection)) return
    // A dialog's stale click must not apply to a newer request selected by the
    // subscription while Vue is updating the page.
    const latest = requests.value.find((value) => value.id === request.id)
    if (latest?.revision !== request.revision || latest.phase !== request.phase) return
    const decision = Symbol('transfer decision')
    activeDecision = decision
    busy.value = true
    error.value = ''
    try {
      await decideEsimTransferRequest(request, accept, selection)
    } catch (failure) {
      const displayed = current.value
      if (
        activeDecision === decision &&
        displayed?.id === request.id &&
        displayed.revision === request.revision
      ) {
        error.value = failure instanceof Error ? failure.message : ''
      }
    } finally {
      if (activeDecision === decision) {
        activeDecision = undefined
        busy.value = false
      }
    }
  }

  const dismiss = () => {
    const request = current.value
    if (!request) return
    if (isTransferApproval(request)) {
      void decide(request, false)
      return
    }
    dismissed.add(request.id)
    requests.value = requests.value.filter((value) => value.id !== request.id)
  }

  watch(sessionRevision, () => {
    close()
    dismissed.clear()
  })
  watch(capabilities.features, () => {
    if (capabilities.hasFeature('esimTransfer')) connect()
    else close()
  })
  onMounted(() => {
    void capabilities.fetchCapabilities().then(connect)
    timer = setInterval(connect, 10_000)
  })
  onBeforeUnmount(() => {
    stopped = true
    clearInterval(timer)
    close()
  })
  return { current, busy, error, decide, dismiss }
}

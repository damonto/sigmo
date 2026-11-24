import { ref, shallowRef, watch, type ComputedRef } from 'vue'
import { useDocumentVisibility } from '@vueuse/core'

import { useInternetApi } from '@/apis/internet'
import type { InternetTrafficResponse, InternetTrafficSample } from '@/types/internet'

export const TRAFFIC_WINDOW_MS = 30_000
const CONNECTED_INTERVAL_MS = 1000
const DISCOVERY_INTERVAL_MS = 5000
const REQUEST_TIMEOUT_MS = 3000
const STATUS_TIMEOUT_MS = 5000

export interface TrafficRates {
  download: number
  upload: number
}

export interface TrafficPoint {
  elapsedMilliseconds: number
  bitsPerSecond: number
}

/** Samples the active session while the detail view is visible. Rates are in bits per second. */
export const useModemTraffic = (
  modemId: ComputedRef<string>,
  simId: ComputedRef<string>,
  enabled: ComputedRef<boolean>,
) => {
  const api = useInternetApi()
  const visibility = useDocumentVisibility()
  const connected = ref(false)
  const usedBytes = ref<number | null>(null)
  const rates = shallowRef<TrafficRates | null>(null)
  const history = shallowRef<TrafficPoint[]>([])
  let previous: InternetTrafficSample | null = null

  const clearSamples = () => {
    usedBytes.value = null
    rates.value = null
    if (history.value.length) history.value = []
    previous = null
  }

  const apply = ({ connected: isConnected, sample }: InternetTrafficResponse) => {
    connected.value = isConnected
    if (!connected.value || !sample) {
      clearSamples()
      return
    }

    const prior = previous
    const elapsed = prior ? sample.elapsedMilliseconds - prior.elapsedMilliseconds : 0
    const sameSession = prior && sample.sessionId === prior.sessionId
    if (sameSession && elapsed <= 0) return

    usedBytes.value = sample.rxBytes + sample.txBytes
    previous = sample
    if (
      !sameSession ||
      elapsed > TRAFFIC_WINDOW_MS ||
      sample.rxBytes < prior.rxBytes ||
      sample.txBytes < prior.txBytes
    ) {
      rates.value = null
      history.value = []
      return
    }

    const seconds = elapsed / 1000
    const download = ((sample.rxBytes - prior.rxBytes) * 8) / seconds
    const upload = ((sample.txBytes - prior.txBytes) * 8) / seconds
    rates.value = { download, upload }
    const bitsPerSecond = download + upload
    const cutoff = sample.elapsedMilliseconds - TRAFFIC_WINDOW_MS
    const points = history.value
    if (bitsPerSecond > 0 || points[points.length - 1]?.bitsPerSecond) {
      history.value = [
        ...points.filter((point) => point.elapsedMilliseconds >= cutoff),
        { elapsedMilliseconds: sample.elapsedMilliseconds, bitsPerSecond },
      ]
    } else if (
      points.length &&
      !points.some((point) => point.elapsedMilliseconds >= cutoff && point.bitsPerSecond > 0)
    ) {
      history.value = []
    }
  }

  watch(
    [modemId, simId, enabled, visibility],
    ([id, , canSample, state], _, onCleanup) => {
      clearSamples()
      connected.value = false
      if (!id || !canSample || state === 'hidden') return

      let stopped = false
      let request: AbortController | undefined
      let timer: number | undefined
      let lastResponseAt = performance.now()
      const poll = async () => {
        const controller = new AbortController()
        request = controller
        timer = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
        try {
          const response = await api.getTraffic(id, controller.signal)
          if (stopped) return
          controller.signal.throwIfAborted()
          lastResponseAt = performance.now()
          apply(response)
        } catch {
          if (stopped) return
          // A missed sample can still be recovered from the next cumulative total.
          rates.value = null
          if (performance.now() - lastResponseAt >= STATUS_TIMEOUT_MS) {
            connected.value = false
            clearSamples()
          }
        } finally {
          request = undefined
          window.clearTimeout(timer)
          if (!stopped) {
            const interval = connected.value ? CONNECTED_INTERVAL_MS : DISCOVERY_INTERVAL_MS
            timer = window.setTimeout(() => void poll(), interval)
          }
        }
      }
      onCleanup(() => {
        stopped = true
        request?.abort()
        window.clearTimeout(timer)
      })
      void poll()
    },
    { immediate: true },
  )

  return { connected, usedBytes, rates, history }
}

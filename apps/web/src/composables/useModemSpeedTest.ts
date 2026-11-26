import { computed, ref, shallowRef, watch, type ComputedRef } from 'vue'
import { useDocumentVisibility } from '@vueuse/core'

import { useInternetApi } from '@/apis/internet'
import { sessionRevision } from '@/lib/authSession'
import type { InternetSpeedTestResponse, InternetSpeedTestSize } from '@/types/internet'

const REFRESH_INTERVAL_MS = 5000

/** Reads the backend's active SIM result and measures only in response to a user action. */
export const useModemSpeedTest = (
  modemId: ComputedRef<string>,
  connected: ComputedRef<boolean>,
) => {
  const api = useInternetApi()
  const visibility = useDocumentVisibility()
  const status = shallowRef<InternetSpeedTestResponse | null>(null)
  const failure = shallowRef<unknown>(null)
  const readFailed = ref(false)
  const testRequest = shallowRef<AbortController>()
  let readRequest: AbortController | undefined

  const result = computed(() => status.value?.result ?? null)
  const running = computed(() => testRequest.value !== undefined)
  const canRun = computed(() =>
    Boolean(connected.value && status.value?.iccid && !running.value && !readFailed.value),
  )

  const refresh = async () => {
    const id = modemId.value
    if (!id || visibility.value !== 'visible' || readRequest || testRequest.value) return
    const request = new AbortController()
    readRequest = request
    try {
      const latest = await api.getLatestSpeedTest(id, request.signal)
      if (modemId.value === id && !request.signal.aborted) {
        if (status.value?.iccid !== latest.iccid) failure.value = null
        status.value = latest
        readFailed.value = false
      }
    } catch {
      if (modemId.value === id && !request.signal.aborted) readFailed.value = true
    } finally {
      if (readRequest === request) readRequest = undefined
    }
  }

  const cancelRead = () => {
    readRequest?.abort()
    readRequest = undefined
  }

  const run = async (sizeMB: InternetSpeedTestSize) => {
    const id = modemId.value
    const iccid = status.value?.iccid
    if (!id || !iccid || !canRun.value) return
    cancelRead()
    const request = new AbortController()
    testRequest.value = request
    failure.value = null
    try {
      const updated = await api.runSpeedTest(id, iccid, sizeMB, request.signal)
      if (updated.iccid !== iccid || !updated.result) throw new Error('Invalid speed test result')
      if (modemId.value === id && !request.signal.aborted) {
        status.value = updated
      }
    } catch (error) {
      if (modemId.value === id && !request.signal.aborted) failure.value = error
    } finally {
      if (testRequest.value === request) {
        testRequest.value = undefined
        if (failure.value) void refresh()
      }
    }
  }

  watch(
    [modemId, sessionRevision],
    (_current, _previous, onCleanup) => {
      status.value = null
      failure.value = null
      readFailed.value = false
      onCleanup(() => {
        cancelRead()
        testRequest.value?.abort()
        testRequest.value = undefined
      })
    },
    { immediate: true, flush: 'sync' },
  )
  watch(
    [modemId, sessionRevision, visibility],
    ([id, , state], _, onCleanup) => {
      if (!id || state !== 'visible') return
      void refresh()
      const timer = window.setInterval(() => void refresh(), REFRESH_INTERVAL_MS)
      onCleanup(() => {
        window.clearInterval(timer)
        cancelRead()
      })
    },
    { immediate: true },
  )
  watch(connected, () => void refresh())

  return { result, running, failure, readFailed, canRun, run }
}

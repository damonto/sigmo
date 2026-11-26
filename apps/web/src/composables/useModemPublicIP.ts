import { shallowRef, watch, type ComputedRef } from 'vue'

import { useInternetApi } from '@/apis/internet'
import { sessionRevision } from '@/lib/authSession'
import type { InternetPublicResponse } from '@/types/internet'

/** Requests egress information once per visible connection and drops retired responses. */
export const useModemPublicIP = (
  modemId: ComputedRef<string>,
  connectionKey: ComputedRef<string>,
) => {
  const internetApi = useInternetApi()
  const publicInfo = shallowRef<InternetPublicResponse | null>(null)
  let request: AbortController | undefined

  watch(
    [modemId, connectionKey],
    async ([id, key], _, onCleanup) => {
      publicInfo.value = null
      if (!id || !key) return
      const controller = new AbortController()
      request = controller
      onCleanup(() => controller.abort())
      try {
        const info = await internetApi.getPublic(id, controller.signal)
        if (!controller.signal.aborted && modemId.value === id && connectionKey.value === key) {
          publicInfo.value = info
        }
      } catch {
        // A failed lookup does not affect the connection or the live chart.
      } finally {
        if (request === controller) request = undefined
      }
    },
    { immediate: true },
  )
  watch(
    sessionRevision,
    () => {
      request?.abort()
      publicInfo.value = null
    },
    { flush: 'sync' },
  )

  return publicInfo
}

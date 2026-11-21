import { shallowRef, watch, type Ref } from 'vue'

import { automationApi } from '@/apis/automation'
import { sessionRevision } from '@/lib/authSession'
import type { ScheduledAutomation } from '@/types/automation'

const REFRESH_INTERVAL_MS = 30_000

/** Identifies a line within the current modem, including its eSIM secure element. */
export const automationLineKey = (profileId: string, seId = '') => `${seId}:${profileId}`

/** Loads the lightweight schedule list for the visible modem. */
export const useScheduledAutomations = (modemId: Readonly<Ref<string | undefined>>) => {
  const schedules = shallowRef<ReadonlyMap<string, ScheduledAutomation>>(new Map())
  let refresh: (() => Promise<void>) | undefined

  watch(
    [modemId, sessionRevision],
    ([id], _previous, onCleanup) => {
      schedules.value = new Map()
      refresh = undefined
      if (!id) return

      let pending: AbortController | undefined
      refresh = async () => {
        pending?.abort()
        const request = new AbortController()
        pending = request
        try {
          const { items } = await automationApi.scheduled(id, request.signal)
          if (request.signal.aborted) return
          const grouped = new Map<string, ScheduledAutomation>()
          for (const item of items) {
            const key = automationLineKey(item.profileId, item.seId)
            if (!grouped.has(key)) grouped.set(key, item)
          }
          schedules.value = grouped
        } catch {
          // Schedule summaries should not block or clear the visible line list.
        } finally {
          if (pending === request) pending = undefined
        }
      }

      const refreshWhenVisible = () => {
        if (!pending && document.visibilityState === 'visible') void refresh?.()
      }
      void refresh()
      const timer = window.setInterval(refreshWhenVisible, REFRESH_INTERVAL_MS)
      document.addEventListener('visibilitychange', refreshWhenVisible)
      onCleanup(() => {
        refresh = undefined
        pending?.abort()
        window.clearInterval(timer)
        document.removeEventListener('visibilitychange', refreshWhenVisible)
      })
    },
    { immediate: true, flush: 'sync' },
  )

  return { schedules, refresh: () => refresh?.() }
}

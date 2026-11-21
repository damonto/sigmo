import { flushPromises } from '@vue/test-utils'
import { effectScope, ref } from 'vue'
import { expect, it, vi } from 'vitest'

import { useScheduledAutomations } from '@/composables/useScheduledAutomations'
import type { ScheduledAutomation } from '@/types/automation'

const scheduled = vi.hoisted(() => vi.fn())

vi.mock('@/apis/automation', () => ({ automationApi: { scheduled } }))

it('clears old summaries and discards a late response when switching modems', async () => {
  let resolveFirst: (value: { items: ScheduledAutomation[] }) => void = () => {}
  const first = new Promise<{ items: ScheduledAutomation[] }>((resolve) => {
    resolveFirst = resolve
  })
  const second: ScheduledAutomation = {
    id: 'to-call',
    profileId: 'travel',
    seId: 'se1',
    actionType: 'call',
    nextAt: '2026-11-30T12:00:00Z',
  }
  scheduled.mockReturnValueOnce(first).mockResolvedValueOnce({ items: [second] })

  const modemId = ref('modem-a')
  const scope = effectScope()
  try {
    const state = scope.run(() => useScheduledAutomations(modemId))
    const firstSignal = scheduled.mock.calls[0]?.[1]
    modemId.value = 'modem-b'
    expect(firstSignal?.aborted).toBe(true)
    expect(state?.schedules.value.size).toBe(0)

    await flushPromises()
    resolveFirst({ items: [{ ...second, id: 'from-old-modem' }] })
    await flushPromises()

    expect(state?.schedules.value.get('se1:travel')).toEqual(second)
    expect(scheduled.mock.calls.map(([id]) => id)).toEqual(['modem-a', 'modem-b'])
  } finally {
    scope.stop()
  }
})

import { computed, effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { useModemTraffic } from '@/composables/useModemTraffic'
import { parseInternetTrafficResponse, type InternetTrafficResponse } from '@/types/internet'

const { getTraffic } = vi.hoisted(() => ({
  getTraffic: vi.fn<(id: string, signal: AbortSignal) => Promise<InternetTrafficResponse>>(),
}))
vi.mock('@/apis/internet', () => ({ useInternetApi: () => ({ getTraffic }) }))

const response = (elapsedMilliseconds: number, rxBytes: number): InternetTrafficResponse => ({
  connected: true,
  sessionId: 'session-1',
  sample: { elapsedMilliseconds, rxBytes, txBytes: 0 },
})

describe('useModemTraffic', () => {
  it('keeps cumulative usage through a timeout and expires stale connection status', async () => {
    getTraffic
      .mockReset()
      .mockResolvedValueOnce(response(0, 0))
      .mockResolvedValueOnce(response(1000, 1000))
      .mockImplementationOnce(
        (_, signal) =>
          new Promise((_, reject) => {
            signal.addEventListener('abort', () => reject(signal.reason), { once: true })
          }),
      )
      .mockResolvedValueOnce(response(6000, 6000))
      .mockRejectedValue(new Error('unavailable'))

    vi.useFakeTimers()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const scope = effectScope()
    try {
      const traffic = scope.run(() =>
        useModemTraffic(
          computed(() => 'modem-1'),
          computed(() => 'sim-1'),
          computed(() => true),
        ),
      )
      if (!traffic) throw new Error('traffic polling did not start')

      await vi.advanceTimersByTimeAsync(1000)
      expect(traffic.rates.value?.download).toBe(8000)
      expect(traffic.history.value).toHaveLength(1)

      await vi.advanceTimersByTimeAsync(4000)
      expect(getTraffic.mock.calls[2]?.[1].aborted).toBe(true)
      expect(traffic.connected.value).toBe(true)
      expect(traffic.usedBytes.value).toBe(1000)
      expect(traffic.rates.value).toBeNull()
      expect(traffic.history.value).toHaveLength(1)

      await vi.advanceTimersByTimeAsync(1000)
      expect(traffic.usedBytes.value).toBe(6000)
      expect(traffic.rates.value?.download).toBe(8000)
      expect(traffic.history.value).toHaveLength(2)

      await vi.advanceTimersByTimeAsync(5000)
      expect(traffic.connected.value).toBe(false)
      expect(traffic.usedBytes.value).toBeNull()
    } finally {
      scope.stop()
      visibility.mockRestore()
      vi.useRealTimers()
    }
  })

  it('rejects invalid counters at the API boundary', () => {
    expect(() => parseInternetTrafficResponse(response(1000, -1))).toThrow()
    expect(() => parseInternetTrafficResponse(response(1000, Number.NaN))).toThrow()
    expect(() => parseInternetTrafficResponse({ connected: true, sample: null })).toThrow()
  })

  it('updates the connection identity even when the new counters are unavailable', async () => {
    vi.useFakeTimers()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const scope = effectScope()
    try {
      getTraffic
        .mockReset()
        .mockResolvedValueOnce(response(0, 1000))
        .mockResolvedValue({ connected: true, sessionId: 'session-2', sample: null })
      const traffic = scope.run(() =>
        useModemTraffic(
          computed(() => 'modem-1'),
          computed(() => 'sim-1'),
          computed(() => true),
        ),
      )
      await vi.advanceTimersByTimeAsync(0)
      expect(traffic?.sessionId.value).toBe('session-1')
      await vi.advanceTimersByTimeAsync(1000)
      expect(traffic?.connected.value).toBe(true)
      expect(traffic?.sessionId.value).toBe('session-2')
      expect(traffic?.usedBytes.value).toBeNull()
    } finally {
      scope.stop()
      visibility.mockRestore()
      vi.useRealTimers()
    }
  })
})

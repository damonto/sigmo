import { flushPromises } from '@vue/test-utils'
import { computed, effectScope, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { useModemPublicIP } from '@/composables/useModemPublicIP'
import { useModemSpeedTest } from '@/composables/useModemSpeedTest'
import type {
  InternetPublicResponse,
  InternetSpeedTestResponse,
  InternetSpeedTestSize,
} from '@/types/internet'

const { getPublic, getLatestSpeedTest, runSpeedTest } = vi.hoisted(() => ({
  getPublic: vi.fn<(id: string, signal: AbortSignal) => Promise<InternetPublicResponse>>(),
  getLatestSpeedTest:
    vi.fn<(id: string, signal: AbortSignal) => Promise<InternetSpeedTestResponse>>(),
  runSpeedTest:
    vi.fn<
      (
        id: string,
        iccid: string,
        sizeMB: InternetSpeedTestSize,
        signal: AbortSignal,
      ) => Promise<InternetSpeedTestResponse>
    >(),
}))
vi.mock('@/apis/internet', () => ({
  useInternetApi: () => ({ getPublic, getLatestSpeedTest, runSpeedTest }),
}))

const deferred = <T>() => {
  let resolve: ((value: T) => void) | undefined
  let reject: ((reason: Error) => void) | undefined
  const promise = new Promise<T>((accept, fail) => {
    resolve = accept
    reject = fail
  })
  return { promise, resolve, reject }
}

describe('Internet diagnostics', () => {
  it('requests IP info once per visible connection and discards the retired response', async () => {
    const pending = deferred<InternetPublicResponse>()
    getPublic.mockReset().mockReturnValueOnce(pending.promise)
    getPublic.mockResolvedValueOnce({ ip: '2001:db8::42' })
    const session = ref('sim-1:0')
    const scope = effectScope()
    try {
      const publicInfo = scope.run(() =>
        useModemPublicIP(
          computed(() => 'modem-1'),
          computed(() => session.value),
        ),
      )
      await flushPromises()
      expect(getPublic).toHaveBeenCalledTimes(1)
      session.value = 'sim-1:1'
      await flushPromises()
      expect(getPublic).toHaveBeenCalledTimes(2)
      expect(getPublic.mock.calls[0]?.[1].aborted).toBe(true)
      expect(publicInfo?.value?.ip).toBe('2001:db8::42')
      pending.resolve?.({ ip: '203.0.113.1' })
      await flushPromises()
      expect(publicInfo?.value?.ip).toBe('2001:db8::42')
      session.value = ''
      await flushPromises()
      expect(publicInfo?.value).toBeNull()
      expect(getPublic).toHaveBeenCalledTimes(2)
    } finally {
      scope.stop()
    }
  })

  it('reads offline results, runs only on demand, and keeps the saved result on failure', async () => {
    const stored: InternetSpeedTestResponse = {
      iccid: 'sim-1',
      result: { downloadBps: 12_000_000, uploadBps: 2_000_000, testedAt: '2026-01-01T00:00:00Z' },
    }
    const pending = deferred<InternetSpeedTestResponse>()
    getLatestSpeedTest.mockReset().mockResolvedValue(stored)
    runSpeedTest.mockReset().mockReturnValueOnce(pending.promise)
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const connected = ref(false)
    const scope = effectScope()
    try {
      const state = scope.run(() =>
        useModemSpeedTest(
          computed(() => 'modem-1'),
          computed(() => connected.value),
        ),
      )
      if (!state) throw new Error('speed test did not initialize')
      await flushPromises()
      expect(state.result.value).toEqual(stored.result)
      expect(state.canRun.value).toBe(false)
      expect(runSpeedTest).not.toHaveBeenCalled()

      connected.value = true
      await flushPromises()
      const test = state.run(50)
      await state.run(100)
      expect(state.running.value).toBe(true)
      expect(runSpeedTest).toHaveBeenCalledExactlyOnceWith(
        'modem-1',
        'sim-1',
        50,
        expect.any(AbortSignal),
      )
      pending.reject?.(new Error('upload failed'))
      await test
      await flushPromises()
      expect(state.failure.value).toEqual(new Error('upload failed'))
      expect(state.result.value).toEqual(stored.result)

      getLatestSpeedTest.mockResolvedValue({ iccid: 'sim-2', result: null })
      connected.value = false
      await flushPromises()
      expect(state.result.value).toBeNull()
      expect(state.failure.value).toBeNull()
    } finally {
      scope.stop()
      visibility.mockRestore()
    }
  })

  it('stops the result timer in the background and reads again on return', async () => {
    vi.useFakeTimers()
    getLatestSpeedTest.mockReset().mockResolvedValue({ iccid: 'sim-1', result: null })
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const scope = effectScope()
    try {
      scope.run(() =>
        useModemSpeedTest(
          computed(() => 'modem-1'),
          computed(() => false),
        ),
      )
      await vi.advanceTimersByTimeAsync(0)
      expect(getLatestSpeedTest).toHaveBeenCalledTimes(1)
      expect(vi.getTimerCount()).toBe(1)

      visibility.mockReturnValue('hidden')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(10_000)
      expect(vi.getTimerCount()).toBe(0)
      expect(getLatestSpeedTest).toHaveBeenCalledTimes(1)

      visibility.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)
      expect(getLatestSpeedTest).toHaveBeenCalledTimes(2)
    } finally {
      scope.stop()
      visibility.mockRestore()
      vi.useRealTimers()
    }
  })
})

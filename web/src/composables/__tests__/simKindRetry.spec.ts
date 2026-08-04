import { afterEach, describe, expect, it, vi } from 'vitest'

import { createSIMKindRetry } from '@/composables/simKindRetry'

describe('createSIMKindRetry', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('backs off at 1, 2, and 4 seconds before steady polling', async () => {
    vi.useFakeTimers()
    const simKindRetry = createSIMKindRetry()
    const retry = vi.fn()
    retry.mockImplementation(() => simKindRetry.schedule(true, retry))

    simKindRetry.schedule(true, retry)

    await vi.advanceTimersByTimeAsync(999)
    expect(retry).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(retry).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2000)
    expect(retry).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(4000)
    expect(retry).toHaveBeenCalledTimes(3)

    await vi.advanceTimersByTimeAsync(9999)
    expect(retry).toHaveBeenCalledTimes(3)

    await vi.advanceTimersByTimeAsync(1)
    expect(retry).toHaveBeenCalledTimes(4)

    await vi.advanceTimersByTimeAsync(10_000)
    expect(retry).toHaveBeenCalledTimes(5)
  })

  it('cancels and restarts the sequence', async () => {
    vi.useFakeTimers()
    const simKindRetry = createSIMKindRetry()
    const retry = vi.fn()

    simKindRetry.schedule(true, retry)
    simKindRetry.reset()
    await vi.advanceTimersByTimeAsync(1000)
    expect(retry).not.toHaveBeenCalled()

    simKindRetry.schedule(true, retry)
    await vi.advanceTimersByTimeAsync(1000)
    expect(retry).toHaveBeenCalledOnce()
  })
})

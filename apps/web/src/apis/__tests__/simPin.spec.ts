import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useSimPinApi } from '@/apis/simPin'

describe('SIM PIN API contract', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
  it('keeps unknown observations nullable and rejects guessed protection values', async () => {
    const value = {
      supported: true,
      enabled: null,
      lock: 'future_lock',
      pinRetries: null,
      pukRetries: 0,
      simToken: 'target',
    }
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(new Response(JSON.stringify(value), { status: 200 })),
        ),
    )
    expect(await useSimPinApi().status('modem')).toEqual({ ...value, lock: 'unknown' })
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ ...value, enabled: 'false' }), { status: 200 }),
        ),
    )
    await expect(useSimPinApi().status('modem')).rejects.toThrow()
  })
  it('sends one PUK request with leading zeros and does not log credentials', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetch)
    const log = vi.spyOn(console, 'log').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    await useSimPinApi().submit('modem', {
      kind: 'unblock',
      puk: '00123456',
      newPin: '0001',
      simToken: 'target',
    })
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/modems/modem/sim-unlocks'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ puk: '00123456', newPin: '0001', simToken: 'target' }),
      }),
    )
    expect(log).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useLicenseStore } from '@/stores/license'

describe('license store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('recognizes Community only from an explicit 404', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 404 })))

    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(true)
    expect(store.mode).toBe('community')
    expect(store.businessEnabled).toBe(true)
  })

  it.each([
    {
      name: 'server error',
      response: () =>
        new Response(JSON.stringify({ error_code: 'internal_server_error', message: 'internal' }), {
          status: 500,
        }),
    },
    {
      name: 'invalid success response',
      response: () => Response.json({ authorized: 'yes' }),
    },
  ])('fails closed on $name', async ({ response }) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response()))

    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(false)
    expect(store.mode).toBe('unavailable')
    expect(store.businessEnabled).toBe(false)
  })

  it('fails closed on a network error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))

    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(false)
    expect(store.mode).toBe('unavailable')
    expect(store.businessEnabled).toBe(false)
  })

  it('shares an in-flight status request between concurrent callers', async () => {
    let resolveResponse: ((response: Response) => void) | undefined
    const fetchMock = vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          resolveResponse = resolve
        }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const store = useLicenseStore()
    const first = store.check()
    const second = store.check()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    resolveResponse?.(
      Response.json({ authorized: true, deviceId: '0123456789abcdef0123456789abcdef' }),
    )
    await expect(Promise.all([first, second])).resolves.toEqual([true, true])
    expect(store.mode).toBe('authorized')
  })
})

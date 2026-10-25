import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useLicenseStore } from '@/stores/license'
import { problemResponse } from '@/lib/__tests__/httpProblem'

describe('license store', () => {
  it('records an unlicensed installation', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          authorized: false,
          deviceId: '0123456789abcdef0123456789abcdef',
        }),
      ),
    )
    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(false)
    expect(store.mode).toBe('unlicensed')
    expect(store.deviceId).not.toBe('')
  })
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('treats a missing license endpoint as unavailable', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(problemResponse(404, 'resource_not_found', 'not found')),
    )

    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(false)
    expect(store.mode).toBe('unavailable')
  })

  it.each([
    {
      name: 'server error',
      response: () => problemResponse(500, 'internal_server_error', 'internal'),
    },
    {
      name: 'proxy 404',
      response: () =>
        new Response('<html>Not found</html>', {
          status: 404,
          headers: { 'Content-Type': 'text/html' },
        }),
    },
    {
      name: 'unrelated business 404',
      response: () => problemResponse(404, 'license_pairing_not_found', 'not found'),
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
    expect(store.authorized).toBe(false)
  })

  it('fails closed on a network error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))

    const store = useLicenseStore()
    await expect(store.check()).resolves.toBe(false)
    expect(store.mode).toBe('unavailable')
    expect(store.authorized).toBe(false)
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

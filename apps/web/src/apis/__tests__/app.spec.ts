import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAppApi } from '@/apis/app'

describe('useAppApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads app info from the app resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ version: 'v1.2.3' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { data } = await useAppApi().getAppInfo()

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/app'),
      expect.any(Object),
    )
    expect(data.value).toEqual({ version: 'v1.2.3' })
  })

  it('reports whether the full application server is ready', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(useAppApi().ready()).resolves.toBe(true)

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/app'),
      expect.objectContaining({ cache: 'no-store' }),
    )
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useInternetApi } from '@/apis/internet'

describe('useInternetApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('updates the current connection preferences resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: 'connected' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useInternetApi().updatePreferences('modem-1', {
      defaultRoute: false,
      proxyEnabled: true,
      alwaysOn: true,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/internet-connections/current/preferences'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({
          defaultRoute: false,
          proxyEnabled: true,
          alwaysOn: true,
        }),
      }),
    )
  })
})

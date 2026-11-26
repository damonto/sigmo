import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useInternetApi } from '@/apis/internet'

describe('useInternetApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('updates the current connection preferences resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: 'connected', sessionId: 'session-1' }), {
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

  it('posts the selected size in MB with the target SIM', async () => {
    const result = {
      iccid: 'sim-1',
      result: { downloadBps: 1_000_000, uploadBps: 500_000, testedAt: '2026-11-26T00:00:00Z' },
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(result))
    vi.stubGlobal('fetch', fetchMock)

    const response = await useInternetApi().runSpeedTest(
      'modem-1',
      'sim-1',
      50,
      new AbortController().signal,
    )

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/internet-connections/speed-tests'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ iccid: 'sim-1', sizeMB: 50 }),
      }),
    )
    expect(response).toEqual(result)
  })
})

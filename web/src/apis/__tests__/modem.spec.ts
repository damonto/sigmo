import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemApi } from '@/apis/modem'

describe('useModemApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('creates a Wi-Fi Calling session resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }))
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().createWiFiCallingSession('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling-sessions'),
      expect.objectContaining({ method: 'POST' }),
    )
  })
})

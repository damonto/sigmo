import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useEsimApi } from '@/apis/esim'

describe('useEsimApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads eSIM transfer source resources', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ sources: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useEsimApi().getTransferSources('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/esim-transfers/sources'),
      expect.any(Object),
    )
  })

  it('loads eSIM transfer source profile resources', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useEsimApi().getTransferProfiles('modem-1', {
      sourceType: 'modem',
      sourceId: 'source-1',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/esim-transfers/source-profiles'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ sourceType: 'modem', sourceId: 'source-1' }),
      }),
    )
  })
})

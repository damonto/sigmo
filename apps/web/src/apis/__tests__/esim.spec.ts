import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useEsimApi } from '@/apis/esim'
import * as notifications from '@/lib/notify'

describe('useEsimApi', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
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

  it.each(['sources', 'profiles'])(
    'leaves %s errors to the active transfer dialog',
    async (resource) => {
      const notify = vi.spyOn(notifications, 'notifyError').mockImplementation(() => {})
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          new Response(
            JSON.stringify({
              type: 'https://sigmo.952007.xyz/problems/transfer_esim_failed',
              title: 'Transfer failed',
              status: 409,
              detail: 'Source is busy',
            }),
            { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
          ),
        ),
      )
      const api = useEsimApi()
      const request =
        resource === 'sources'
          ? api.getTransferSources('target')
          : api.getTransferProfiles('target', { sourceType: 'modem', sourceId: 'source' })

      await expect(request).rejects.toMatchObject({
        status: 409,
        error_code: 'transfer_esim_failed',
        message: 'Source is busy',
      })
      expect(notify).not.toHaveBeenCalled()
    },
  )
})

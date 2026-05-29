import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useCallApi } from '@/apis/call'
import { getStoredToken, setStoredToken } from '@/lib/authStorage'

describe('useCallApi', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('returns structured call errors without using the global error presenter', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error_code: 'wifi_calling_not_connected',
            message: 'wifi calling is not connected',
            request_id: 'req-1',
          }),
          {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
          },
        ),
      ),
    )

    await expect(useCallApi().dialCall('modem-1', { to: '+12242255559', route: 'auto' })).rejects.toMatchObject({
      message: 'wifi calling is not connected',
      error_code: 'wifi_calling_not_connected',
      request_id: 'req-1',
      status: 503,
    })
    expect(consoleError).not.toHaveBeenCalled()
  })

  it('clears the stored token on call auth failures', async () => {
    setStoredToken('token-1')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error_code: 'unauthorized', message: 'unauthorized', request_id: 'req-2' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )

    await expect(useCallApi().listCalls('modem-1')).rejects.toMatchObject({ status: 401 })

    expect(getStoredToken()).toBeNull()
  })

  it('updates call state with PATCH requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          callID: 'call-1',
          route: 'wifi_calling',
          direction: 'incoming',
          number: '+12242255559',
          state: 'active',
          reason: '',
          startedAt: '2026-05-27T00:00:00Z',
          answeredAt: '2026-05-27T00:00:05Z',
          endedAt: '',
          updatedAt: '2026-05-27T00:00:05Z',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useCallApi().answerCall('modem-1', 'call/1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/calls/call%2F1'),
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ state: 'active' }),
      }),
    )
  })

  it('hangs up calls with PATCH and deletes records with DELETE', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    const api = useCallApi()

    await api.hangupCall('modem-1', 'call/1')
    await api.deleteCall('modem-1', 'call/1')

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      expect.stringContaining('/api/v1/modems/modem-1/calls/call%2F1'),
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ state: 'ended' }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining('/api/v1/modems/modem-1/calls/call%2F1'),
      expect.objectContaining({ method: 'DELETE' }),
    )
  })
})

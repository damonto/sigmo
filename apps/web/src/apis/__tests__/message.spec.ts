import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useMessageApi } from '@/apis/message'

describe('useMessageApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('adds modem filters and encoded search queries to conversation list requests', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ items: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    await useMessageApi().getConversations({ modemId: 'modem-1', q: 'balance + data' })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/conversations?modemId=modem-1&q=balance+%2B+data'),
      expect.any(Object),
    )
  })

  it('requests bounded conversation pages with an encoded older cursor', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [], nextCursor: '' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useMessageApi().getConversationMessages('original/thread', 'older+snapshot=')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(
        '/api/v1/conversations/original%2Fthread/messages?limit=50&cursor=older%2Bsnapshot%3D',
      ),
      expect.any(Object),
    )
  })
})

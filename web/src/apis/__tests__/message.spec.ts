import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useMessageApi } from '@/apis/message'

describe('useMessageApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('adds encoded search queries to message list requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useMessageApi().getMessages('modem-1', 'balance + data')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/messages?q=balance+%2B+data'),
      expect.any(Object),
    )
  })
})

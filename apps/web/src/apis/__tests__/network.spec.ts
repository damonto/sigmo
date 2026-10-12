import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useNetworkApi } from '@/apis/network'

describe('useNetworkApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('uses resource routes for network scan tasks', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: 'scan-1', status: 'running' }), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: 'scan-1', status: 'completed', networks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    const api = useNetworkApi()
    await api.startNetworkScan('modem-1')
    await api.getNetworkScan('modem-1', 'scan/1')

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      expect.stringContaining('/api/v1/modems/modem-1/network-scans'),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining('/api/v1/modems/modem-1/network-scans/scan%2F1'),
      expect.any(Object),
    )
  })
})

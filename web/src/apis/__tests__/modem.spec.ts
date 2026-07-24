import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemApi } from '@/apis/modem'

describe('useModemApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads Wi-Fi Calling settings from nested resources', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ enabled: true, connected: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().getWiFiCallingSettings('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/settings'),
      expect.any(Object),
    )
  })

  it('updates Wi-Fi Calling settings with nested resources', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().updateWiFiCallingSettings('modem-1', {
      enabled: true,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/settings'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ enabled: true }),
      }),
    )
  })

  it('creates a Wi-Fi Calling session resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }))
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().createWiFiCallingSession('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/sessions'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('deletes the current Wi-Fi Calling session resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().deleteWiFiCallingSession('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/sessions/current'),
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('starts a Wi-Fi Calling websheet resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: 'sheet-1', url: 'https://example.test' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().startWiFiCallingWebsheet('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/websheets'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('starts a Wi-Fi Calling emergency address websheet resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: 'sheet-1', url: 'https://example.test' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useModemApi().startWiFiCallingEmergencyAddressWebsheet('modem-1')

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/modems/modem-1/wifi-calling/emergency-address-websheets'),
      expect.objectContaining({ method: 'POST' }),
    )
  })
})

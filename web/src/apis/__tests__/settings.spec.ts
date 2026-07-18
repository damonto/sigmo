import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useSettingsApi } from '@/apis/settings'

describe('useSettingsApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('updates the auth settings resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ schema: {}, values: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useSettingsApi().updateAuth({
      otpRequired: true,
      authProviders: ['telegram'],
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/auth'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ otpRequired: true, authProviders: ['telegram'] }),
      }),
    )
  })

  it('tests the selected authentication providers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await useSettingsApi().testAuth({ authProviders: ['telegram'] })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/auth-tests'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ authProviders: ['telegram'] }),
      }),
    )
  })

  it('updates the proxy settings resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ schema: {}, values: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useSettingsApi().updateProxy({
      listenAddress: '127.0.0.1',
      httpPort: 8080,
      socks5Port: 1080,
      password: 'secret',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/proxy'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({
          listenAddress: '127.0.0.1',
          httpPort: 8080,
          socks5Port: 1080,
          password: 'secret',
        }),
      }),
    )
  })

  it('updates one notification channel resource', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ schema: {}, values: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await useSettingsApi().updateNotificationChannel('telegram', {
      enabled: false,
      botToken: 'draft-token',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/notifications/telegram'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ enabled: false, botToken: 'draft-token' }),
      }),
    )
  })
})

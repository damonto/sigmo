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
      mode: 'otp',

      oidcIssuer: '',
      oidcClientId: '',
      oidcClientSecret: '',
      oidcAllowedSubjects: [],
      accessTeamDomain: '',
      accessAudience: '',
      accessAllowedSubjects: [],
      otpChannels: ['telegram'],
      sessionMaxDays: 30,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/auth'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({
          mode: 'otp',

          oidcIssuer: '',
          oidcClientId: '',
          oidcClientSecret: '',
          oidcAllowedSubjects: [],
          accessTeamDomain: '',
          accessAudience: '',
          accessAllowedSubjects: [],
          otpChannels: ['telegram'],
          sessionMaxDays: 30,
        }),
      }),
    )
  })

  it('tests the selected authentication providers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await useSettingsApi().testAuth({ channels: ['telegram'] })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/auth-tests'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ channels: ['telegram'] }),
      }),
    )
  })

  it('creates and reads an OIDC test session', async () => {
    const auth = {
      mode: 'oidc' as const,
      sessionMaxDays: 30,
      otpChannels: [],
      oidcIssuer: 'https://issuer.example',
      oidcClientId: 'client',
      oidcClientSecret: 'secret',
      oidcAllowedSubjects: ['owner'],
      accessTeamDomain: '',
      accessAudience: '',
      accessAllowedSubjects: [],
    }
    const expiresAt = new Date(Date.now() + 60_000).toISOString()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        Response.json(
          {
            id: 'test-id',
            authorizationUrl:
              'https://sigmo.example/api/v1/settings/auth-tests/oidc/test-id/browser',
            expiresAt,
          },
          { status: 201 },
        ),
      )
      .mockResolvedValueOnce(Response.json({ status: 'succeeded', subject: 'owner', expiresAt }))
    vi.stubGlobal('fetch', fetchMock)

    const api = useSettingsApi()
    const created = await api.createOIDCTest({ client: 'web', basePath: '/sigmo', state: '', auth })
    const result = await api.getOIDCTest('test-id')

    expect(created.data.value?.id).toBe('test-id')
    expect(result.data.value?.subject).toBe('owner')
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      expect.stringContaining('/api/v1/settings/auth-tests/oidc'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ client: 'web', basePath: '/sigmo', state: '', auth }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining('/api/v1/settings/auth-tests/oidc/test-id'),
      expect.objectContaining({ method: 'GET' }),
    )
  })

  it('tests the draft Cloudflare Access policy', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(Response.json({ status: 'succeeded', subject: 'owner' }))
    vi.stubGlobal('fetch', fetchMock)
    const auth = {
      mode: 'cloudflare_access' as const,
      sessionMaxDays: 30,
      otpChannels: [],
      oidcIssuer: '',
      oidcClientId: '',
      oidcClientSecret: '',
      oidcAllowedSubjects: [],
      accessTeamDomain: 'https://team.cloudflareaccess.com',
      accessAudience: 'aud',
      accessAllowedSubjects: ['owner'],
    }

    const result = await useSettingsApi().testCloudflareAccess(auth)

    expect(result.data.value?.status).toBe('succeeded')
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/auth-tests/cloudflare-access'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify(auth) }),
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
      botToken: 'draft-token',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/settings/channels/telegram'),
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ botToken: 'draft-token' }),
      }),
    )
  })
})

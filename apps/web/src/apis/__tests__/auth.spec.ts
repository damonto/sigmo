import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthApi } from '@/apis/auth'

const fetchMock = vi.fn<typeof fetch>()

describe('browser authentication transport', () => {
  it('accepts already-expired local logout but surfaces other failures', async () => {
    fetchMock.mockResolvedValueOnce(
      Response.json(
        {
          type: 'https://sigmo.952007.xyz/problems/invalid_session',
          title: 'Invalid Session',
          status: 401,
        },
        { status: 401, headers: { 'content-type': 'application/problem+json' } },
      ),
    )
    await expect(useAuthApi().logout()).resolves.toBeUndefined()
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 503 }))
    await expect(useAuthApi().logout()).rejects.toMatchObject({ status: 503 })
  })
  beforeEach(() => {
    vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockReset()
  })

  afterEach(() => vi.unstubAllEnvs())

  it('uses a cookie session and never sends the retired stored token', async () => {
    localStorage.setItem('sigmo:token', 'retired-secret')
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await useAuthApi().verify('challenge', '123456')
    const [url, init] = fetchMock.mock.calls[0] ?? []
    expect(String(url)).toMatch(/\/auth\/sessions$/)
    expect(init?.credentials).toBe('same-origin')
    expect(new Headers(init?.headers).has('Authorization')).toBe(false)
    expect(JSON.parse(String(init?.body))).toEqual({
      method: 'otp',
      client: 'web',
      challengeId: 'challenge',
      code: '123456',
    })
    expect(JSON.stringify(fetchMock.mock.calls)).not.toContain('retired-secret')
  })

  it.each([
    {},
    { version: 2, mode: 'none', methods: [] },
    { version: 1, mode: 'unknown', methods: [] },
    { version: 1, mode: 'otp', methods: ['otp', 'oidc'] },
    { version: 1, mode: 'oidc', methods: ['otp'] },
    { version: 1, mode: 'none', methods: ['otp'] },
    { version: 1, mode: 'sigmo', methods: [] },
    { version: 1, mode: 'sigmo', methods: ['password'] },
  ])('rejects unsupported discovery: %j', async (value) => {
    fetchMock.mockResolvedValue(Response.json(value))
    await expect(useAuthApi().methods()).rejects.toThrow()
  })

  it('validates supported discovery and a bounded OTP challenge', async () => {
    const methods = { version: 1, mode: 'otp', methods: ['otp'] }
    fetchMock.mockResolvedValueOnce(Response.json(methods))
    expect(await useAuthApi().methods()).toEqual(methods)
    fetchMock.mockResolvedValueOnce(
      Response.json({ id: 'c', expiresAt: new Date(Date.now() + 60_000).toISOString() }),
    )
    expect((await useAuthApi().challenge()).id).toBe('c')
    fetchMock.mockResolvedValueOnce(Response.json({ id: 'c', expiresAt: 'invalid' }))
    await expect(useAuthApi().challenge()).rejects.toThrow()
  })

  it.each([
    'https://evil.example/login',
    'https://user:secret@localhost/api/v1/auth/browser-sessions/x',
    '/api/v1/settings',
    '/api/v1/auth/browser-sessions/x?token=secret',
  ])('rejects an unsafe authorization URL: %s', async (target) => {
    fetchMock.mockResolvedValue(
      Response.json({ authorizationUrl: new URL(target, location.origin).toString() }),
    )
    await expect(useAuthApi().browser()).rejects.toThrow()
  })

  it('accepts only the same backend transaction URL and treats 401 as signed out', async () => {
    const target = new URL('/api/v1/auth/browser-sessions/transaction', location.origin).toString()
    fetchMock.mockResolvedValueOnce(Response.json({ authorizationUrl: target }))
    expect(await useAuthApi().browser()).toBe(target)
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 401 }))
    expect(await useAuthApi().session()).toBe(false)
  })

  it.each(['', '/sigmo', '/my%20sigmo'])(
    'derives the login path from the API URL: %s',
    async (basePath) => {
      vi.stubEnv('VITE_API_BASE_URL', `${basePath}/api/v1`)
      const target = new URL(
        `${basePath}/api/v1/auth/browser-sessions/transaction`,
        location.origin,
      ).toString()
      fetchMock.mockResolvedValueOnce(Response.json({ authorizationUrl: target }))
      expect(await useAuthApi().browser()).toBe(target)
      const [url, init] = fetchMock.mock.calls[0] ?? []
      expect(String(url)).toBe(
        new URL(`${basePath}/api/v1/auth/browser-sessions`, location.origin).toString(),
      )
      expect(JSON.parse(String(init?.body))).toEqual({ client: 'web', basePath })
    },
  )
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const notifyHarness = vi.hoisted(() => ({
  error: vi.fn(),
}))

const routerHarness = vi.hoisted(() => ({
  replace: vi.fn(),
  routeName: 'home',
}))

vi.mock('@/router', () => ({
  default: {
    currentRoute: {
      get value() {
        return { name: routerHarness.routeName }
      },
    },
    replace: routerHarness.replace,
  },
}))

vi.mock('@/lib/notify', () => ({
  notifyError: notifyHarness.error,
}))

import { fetchJson, fetchJsonQuietly } from '@/lib/fetch'
import { sessionRevision } from '@/lib/authSession'
import { problemResponse } from './httpProblem'

describe('useFetch global error handling', () => {
  beforeEach(() => {
    notifyHarness.error.mockReset()
    routerHarness.replace.mockReset()
    routerHarness.routeName = 'home'
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(problemResponse(500, 'remote_error', 'request rejected')),
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('shows API errors as a non-blocking global toast', async () => {
    await expect(fetchJson('modems/1/ussd')).rejects.toThrow('request rejected')

    expect(notifyHarness.error).toHaveBeenCalledWith(
      'Server Error',
      'The server could not complete the request.',
    )
  })

  it('does not show a global toast when the active SIM has no eUICC application', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      problemResponse(404, 'euicc_not_supported', 'no supported ISD-R AID found', 'req-2'),
    )

    await expect(fetchJson('modems/1/esims')).rejects.toThrow('no supported ISD-R AID found')

    expect(notifyHarness.error).not.toHaveBeenCalled()
  })

  it('keeps expected restart errors out of the global notifier', async () => {
    await expect(fetchJsonQuietly('update-installations/current')).rejects.toThrow(
      'request rejected',
    )

    expect(notifyHarness.error).not.toHaveBeenCalled()
  })

  it('localizes Worker errors without requiring a request ID', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      problemResponse(403, 'license_entitlement_inactive', 'authorization revoked or expired', ''),
    )

    await expect(fetchJson('settings/updates')).rejects.toThrow('authorization revoked or expired')
    expect(notifyHarness.error).toHaveBeenCalledWith(
      'Error',
      'The Sigmo Pro authorization is revoked or expired.',
    )
  })

  it('tells the server which language the UI is shown in', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['fr-FR', 'zh-CN'])
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))
    await fetchJson<void>('modems/1/messages/+1', { method: 'DELETE' })
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{"ok":true}', { status: 200 }))
    await fetchJsonQuietly('update-installations/current')

    const sentLocales = vi
      .mocked(fetch)
      .mock.calls.map(([, init]) => new Headers(init?.headers).get('X-Sigmo-Locale'))
    expect(sentLocales).toEqual(['zh', 'zh'])
  })

  it('resolves empty successful responses without parsing errors', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    const { data } = await fetchJson<void>('modems/1/messages/+1', { method: 'DELETE' })

    expect(data.value).toBeUndefined()
  })

  it.each([
    [401, 'invalid_session', true],
    [401, 'invalid_otp', false],
    [401, 'license_lease_expired', false],
    [503, 'invalid_session', false],
  ] as const)(
    'changes login only for a local token failure: %s %s',
    async (status, code, logout) => {
      const revision = sessionRevision()
      vi.mocked(fetch).mockResolvedValueOnce(problemResponse(status, code, 'rejected'))
      await expect(fetchJson('modems')).rejects.toMatchObject({ status, error_code: code })
      expect(sessionRevision()).toBe(revision + (logout ? 1 : 0))
      expect(routerHarness.replace).toHaveBeenCalledTimes(logout ? 1 : 0)
    },
  )

  it('preserves login after an unrecognized proxy 401', async () => {
    const revision = sessionRevision()
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response('<html>login</html>', { status: 401, headers: { 'Content-Type': 'text/html' } }),
    )
    await expect(fetchJson('modems')).rejects.toMatchObject({
      status: 401,
      error_code: 'unknown_error',
    })
    expect(sessionRevision()).toBe(revision)
    expect(routerHarness.replace).not.toHaveBeenCalled()
  })
})

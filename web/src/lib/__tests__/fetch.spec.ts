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

const apiError = {
  error_code: 'boom',
  message: 'request rejected',
  request_id: 'req-1',
}

describe('useFetch global error handling', () => {
  beforeEach(() => {
    notifyHarness.error.mockReset()
    routerHarness.replace.mockReset()
    routerHarness.routeName = 'home'
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(apiError), {
          status: 500,
          statusText: 'Internal Server Error',
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('shows API errors as a non-blocking global toast', async () => {
    await expect(fetchJson('modems/1/ussd')).rejects.toThrow('request rejected')

    expect(notifyHarness.error).toHaveBeenCalledWith('Server Error', 'request rejected')
  })

  it('does not show a global toast when the active SIM has no eUICC application', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error_code: 'euicc_not_supported',
          message: 'no supported ISD-R AID found',
          request_id: 'req-2',
        }),
        { status: 404, statusText: 'Not Found' },
      ),
    )

    await expect(fetchJson('modems/1/esims')).rejects.toThrow('no supported ISD-R AID found')

    expect(notifyHarness.error).not.toHaveBeenCalled()
  })

  it('keeps expected restart errors out of the global notifier', async () => {
    await expect(fetchJsonQuietly('update-installations/current')).rejects.toThrow(
      'request returned HTTP 500',
    )

    expect(notifyHarness.error).not.toHaveBeenCalled()
  })

  it('localizes Worker errors without requiring a request ID', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error_code: 'license_entitlement_inactive',
          message: 'authorization revoked or expired',
        }),
        { status: 403, statusText: 'Forbidden' },
      ),
    )

    await expect(fetchJson('settings/updates')).rejects.toThrow('authorization revoked or expired')
    expect(notifyHarness.error).toHaveBeenCalledWith(
      'Error',
      'The Sigmo Pro authorization is revoked or expired.',
    )
  })

  it('resolves empty successful responses without parsing errors', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    const { data } = await fetchJson<void>('modems/1/messages/+1', { method: 'DELETE' })

    expect(data.value).toBeUndefined()
  })
})

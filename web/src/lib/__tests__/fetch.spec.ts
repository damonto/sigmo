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

import { fetchJson } from '@/lib/fetch'

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

  it('resolves empty successful responses without parsing errors', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    const { data } = await fetchJson<void>('modems/1/messages/+1', { method: 'DELETE' })

    expect(data.value).toBeUndefined()
  })
})

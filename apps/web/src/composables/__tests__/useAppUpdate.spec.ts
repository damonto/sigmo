import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useUpdateApi } from '@/apis/update'
import { useAppUpdate } from '@/composables/useAppUpdate'
import type { UpdateSnapshot } from '@/types/update'

const notifier = vi.hoisted(() => ({ error: vi.fn(), replace: vi.fn(), reload: vi.fn() }))
vi.mock('@/lib/notify', () => ({ notifyError: notifier.error }))
vi.mock('@/router', () => ({
  default: {
    currentRoute: { value: { name: 'settings-updates' } },
    replace: notifier.replace,
  },
}))

const snapshot = (state: UpdateSnapshot['state']): UpdateSnapshot => ({
  settings: { automatic: false, channel: 'stable' },
  current: {
    version: 'v1.0.0',
    commit: 'old',
    channel: 'stable',
    target: 'linux-amd64',
    distribution: 'standalone',
  },
  state,
  updateAvailable: true,
  selfUpdateSupported: true,
})

const update = useAppUpdate()

describe('application update monitoring', () => {
  beforeEach(() => {
    update.dispose()
    vi.useFakeTimers()
    vi.clearAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.stubGlobal('window', { location: { reload: notifier.reload } })
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    update.dispose()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('waits through restarting and an outage, then reloads when the new server is idle', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(Response.json(snapshot('restarting')))
      .mockResolvedValueOnce(new Response('Bad Gateway', { status: 502 }))
      .mockResolvedValueOnce(Response.json(snapshot('idle')))
    update.observe(snapshot('downloading'))

    await vi.advanceTimersByTimeAsync(500)
    expect(update.snapshot.value?.state).toBe('restarting')
    expect(notifier.reload).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    expect(update.reconnecting.value).toBe(true)
    expect(notifier.reload).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)

    expect(notifier.reload).toHaveBeenCalledTimes(1)
    expect(update.updating.value).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
    expect(
      vi
        .mocked(fetch)
        .mock.calls.every(([url]) => String(url).endsWith('/update-installations/current')),
    ).toBe(true)
    expect(notifier.error).not.toHaveBeenCalled()
  })

  it('backs off transient proxy errors without changing the last server state', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(new Response(null, { status: 502 }))
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockResolvedValueOnce(new Response(null, { status: 504 }))
      .mockResolvedValueOnce(Response.json(snapshot('failed')))
    update.observe(snapshot('downloading'))

    await vi.advanceTimersByTimeAsync(500)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(update.snapshot.value?.state).toBe('downloading')
    await vi.advanceTimersByTimeAsync(1000)
    expect(fetch).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1999)
    expect(fetch).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetch).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(4000)

    expect(fetch).toHaveBeenCalledTimes(4)
    expect(update.snapshot.value?.state).toBe('failed')
    expect(update.updating.value).toBe(false)
    expect(update.failure.value).toBeUndefined()
    expect(vi.getTimerCount()).toBe(0)
    expect(notifier.error).not.toHaveBeenCalled()
  })

  it.each([
    { name: 'expired authentication', response: () => new Response(null, { status: 401 }) },
    { name: 'forbidden access', response: () => new Response(null, { status: 403 }) },
    { name: 'missing endpoint', response: () => new Response(null, { status: 404 }) },
    { name: 'application error', response: () => new Response(null, { status: 500 }) },
    { name: 'invalid JSON', response: () => new Response('<html>login</html>') },
    { name: 'invalid snapshot', response: () => Response.json({ ok: true }) },
  ])('stops on $name instead of treating it as a restart', async ({ response }) => {
    vi.mocked(fetch).mockResolvedValueOnce(response())
    update.observe(snapshot('downloading'))
    await vi.advanceTimersByTimeAsync(500)

    expect(update.updating.value).toBe(false)
    expect(update.failure.value?.kind).toBe('request')
    expect(notifier.reload).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('aborts a probe after ten seconds and retries the connection', async () => {
    let signal: AbortSignal | null | undefined
    vi.mocked(fetch)
      .mockImplementationOnce((_url, options) => {
        signal = options?.signal
        return new Promise<Response>((_resolve, reject) => {
          signal?.addEventListener('abort', () => reject(signal?.reason), { once: true })
        })
      })
      .mockResolvedValueOnce(Response.json(snapshot('failed')))
    update.observe(snapshot('downloading'))
    await vi.advanceTimersByTimeAsync(500 + 9999)
    expect(signal?.aborted).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    expect(signal?.aborted).toBe(true)
    expect(update.reconnecting.value).toBe(true)

    await vi.advanceTimersByTimeAsync(1000)
    expect(update.updating.value).toBe(false)
    expect(notifier.error).not.toHaveBeenCalled()
  })

  it('stops after three minutes of connection failures and can resume on retry', async () => {
    vi.mocked(fetch).mockRejectedValue(new TypeError('Failed to fetch'))
    update.observe(snapshot('downloading'))
    await vi.advanceTimersByTimeAsync(500 + 180_000)

    expect(update.updating.value).toBe(false)
    expect(update.failure.value).toEqual({ kind: 'timeout' })
    expect(vi.getTimerCount()).toBe(0)
    const calls = vi.mocked(fetch).mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(fetch).toHaveBeenCalledTimes(calls)

    vi.mocked(fetch).mockResolvedValueOnce(Response.json(snapshot('failed')))
    update.observe(snapshot('restarting'))
    expect(update.failure.value).toBeUndefined()
    await vi.advanceTimersByTimeAsync(500)
    expect(update.updating.value).toBe(false)
  })

  it('bounds restarting even while the old process still answers requests', async () => {
    vi.mocked(fetch).mockImplementation(async () => Response.json(snapshot('restarting')))
    update.observe(snapshot('restarting'))
    await vi.advanceTimersByTimeAsync(180_000)

    expect(update.failure.value).toEqual({ kind: 'timeout' })
    expect(update.updating.value).toBe(false)
    expect(notifier.reload).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('allows a reachable download to continue beyond the recovery deadline', async () => {
    vi.mocked(fetch)
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockImplementation(async () => Response.json(snapshot('downloading')))
    update.observe(snapshot('downloading'))
    await vi.advanceTimersByTimeAsync(500 + 180_000)

    expect(update.failure.value).toBeUndefined()
    expect(update.updating.value).toBe(true)
    expect(update.reconnecting.value).toBe(false)
  })

  it('does not hide errors from user actions while monitoring', async () => {
    update.observe(snapshot('downloading'))
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    await expect(
      useUpdateApi().saveSettings({ automatic: true, channel: 'stable' }),
    ).rejects.toMatchObject({ status: 503 })

    expect(notifier.error).toHaveBeenCalledTimes(1)
    expect(update.updating.value).toBe(true)
  })

  it('cleans up a rejected installation and preserves its normal error notification', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    await expect(update.install()).rejects.toMatchObject({ status: 503 })

    expect(update.updating.value).toBe(false)
    expect(notifier.error).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('prevents duplicate installation requests', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(Response.json(snapshot('downloading')))
    await Promise.all([update.install(), update.install()])

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(update.updating.value).toBe(true)
  })

  it('aborts application-owned requests on disposal and ignores late responses', async () => {
    let finish: ((response: Response) => void) | undefined
    let signal: AbortSignal | null | undefined
    vi.mocked(fetch).mockImplementationOnce((_url, options) => {
      signal = options?.signal
      return new Promise<Response>((resolve) => {
        finish = resolve
      })
    })
    update.observe(snapshot('restarting'))
    await vi.advanceTimersByTimeAsync(500)
    update.dispose()

    expect(signal?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
    finish?.(Response.json(snapshot('idle')))
    await vi.advanceTimersByTimeAsync(0)

    expect(update.snapshot.value).toBeUndefined()
    expect(update.failure.value).toBeUndefined()
    expect(notifier.reload).not.toHaveBeenCalled()
  })
})

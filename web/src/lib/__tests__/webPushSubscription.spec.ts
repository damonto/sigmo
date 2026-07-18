import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { hasWebPushSubscription, subscribeToWebPush } from '@/lib/webPush'

const getSubscription = vi.fn<() => Promise<PushSubscription | null>>()
const subscribe = vi.fn<(options: PushSubscriptionOptionsInit) => Promise<PushSubscription>>()

beforeAll(() => {
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: {
      addEventListener: vi.fn(),
      register: vi.fn().mockResolvedValue({ pushManager: { getSubscription, subscribe } }),
    },
  })
  Object.defineProperty(window, 'Notification', {
    configurable: true,
    value: { permission: 'granted', requestPermission: vi.fn() },
  })
  Object.defineProperty(window, 'PushManager', { configurable: true, value: class {} })
})

beforeEach(() => {
  getSubscription.mockReset()
  subscribe.mockReset()
  hasWebPushSubscription.value = false
})

const pushSubscription = (key: number[], endpoint = 'https://push.example/old') =>
  ({
    endpoint,
    options: { applicationServerKey: Uint8Array.from(key).buffer },
    unsubscribe: vi.fn().mockResolvedValue(true),
  }) as unknown as PushSubscription

describe('Web Push subscription renewal', () => {
  it('reuses an existing subscription with the current VAPID key', async () => {
    const existing = pushSubscription([1, 2, 3, 4])
    getSubscription.mockResolvedValue(existing)

    await expect(subscribeToWebPush('AQIDBA')).resolves.toBe(existing)
    expect(existing.unsubscribe).not.toHaveBeenCalled()
    expect(subscribe).not.toHaveBeenCalled()
    expect(hasWebPushSubscription.value).toBe(true)
  })

  it('replaces an existing subscription when the VAPID key changed', async () => {
    const existing = pushSubscription([9, 9, 9, 9])
    const replacement = pushSubscription([1, 2, 3, 4], 'https://push.example/new')
    getSubscription.mockResolvedValue(existing)
    subscribe.mockResolvedValue(replacement)

    await expect(subscribeToWebPush('AQIDBA')).resolves.toBe(replacement)
    expect(existing.unsubscribe).toHaveBeenCalledOnce()
    expect(subscribe).toHaveBeenCalledWith({
      userVisibleOnly: true,
      applicationServerKey: Uint8Array.from([1, 2, 3, 4]).buffer,
    })
  })

  it('forces a fresh endpoint for a subscription missing from the server', async () => {
    const existing = pushSubscription([1, 2, 3, 4])
    const replacement = pushSubscription([1, 2, 3, 4], 'https://push.example/new')
    getSubscription.mockResolvedValue(existing)
    subscribe.mockResolvedValue(replacement)

    await expect(subscribeToWebPush('AQIDBA', { forceRenew: true })).resolves.toBe(replacement)
    expect(existing.unsubscribe).toHaveBeenCalledOnce()
    expect(subscribe).toHaveBeenCalledOnce()
  })
})

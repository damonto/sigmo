import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useWebPush } from '@/composables/useWebPush'

const mocks = vi.hoisted(() => ({
  getOverview: vi.fn(),
  updateEnabled: vi.fn(),
  registerSubscription: vi.fn(),
  renameSubscription: vi.fn(),
  deleteSubscription: vi.fn(),
  currentPushSubscription: vi.fn(),
  subscribeToWebPush: vi.fn(),
  unsubscribeFromWebPush: vi.fn(),
  pushSubscriptionMatchesVAPIDKey: vi.fn(),
  hasWebPushSubscription: { value: false },
}))

vi.mock('@/apis/webPush', () => ({
  useWebPushApi: () => ({
    getOverview: mocks.getOverview,
    updateEnabled: mocks.updateEnabled,
    registerSubscription: mocks.registerSubscription,
    renameSubscription: mocks.renameSubscription,
    deleteSubscription: mocks.deleteSubscription,
  }),
}))

vi.mock('@/lib/webPush', () => ({
  currentPushSubscription: mocks.currentPushSubscription,
  defaultDeviceLabel: () => 'Chrome on Linux',
  devicePlatform: () => 'Linux',
  hasWebPushSubscription: mocks.hasWebPushSubscription,
  notificationPermission: () => 'default',
  pushSubscriptionMatchesVAPIDKey: mocks.pushSubscriptionMatchesVAPIDKey,
  subscribeToWebPush: mocks.subscribeToWebPush,
  unsubscribeFromWebPush: mocks.unsubscribeFromWebPush,
  webPushSupportReason: () => 'supported',
}))

const response = <T>(value: T) => ({ data: { value } })

describe('useWebPush', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.hasWebPushSubscription.value = false
    vi.stubGlobal('Notification', {
      permission: 'default',
      requestPermission: vi.fn().mockResolvedValue('granted'),
    })
  })

  it('renews a local subscription missing from the server before registering it', async () => {
    const oldSubscription = { endpoint: 'https://push.example/expired' } as PushSubscription
    const newSubscription = {
      endpoint: 'https://push.example/new',
      toJSON: () => ({
        endpoint: 'https://push.example/new',
        keys: { p256dh: 'p256dh', auth: 'auth' },
      }),
    } as unknown as PushSubscription
    const emptyOverview = { enabled: true, publicKey: 'AQIDBA', subscriptions: [] }
    const registeredOverview = {
      ...emptyOverview,
      subscriptions: [
        {
          id: 'new',
          endpoint: 'https://push.example/new',
          label: 'Chrome on Linux',
          userAgent: '',
          platform: 'Linux',
          createdAt: '2026-07-18T00:00:00Z',
          updatedAt: '2026-07-18T00:00:00Z',
        },
      ],
    }
    mocks.getOverview
      .mockResolvedValueOnce(response(emptyOverview))
      .mockResolvedValueOnce(response(registeredOverview))
    mocks.currentPushSubscription.mockResolvedValue(oldSubscription)
    mocks.pushSubscriptionMatchesVAPIDKey.mockReturnValue(true)
    mocks.subscribeToWebPush.mockResolvedValue(newSubscription)
    mocks.registerSubscription.mockResolvedValue(response(registeredOverview.subscriptions[0]))

    const webPush = useWebPush()
    await webPush.load()

    expect(webPush.currentSubscription.value).toBeNull()
    expect(mocks.hasWebPushSubscription.value).toBe(false)

    await expect(webPush.enableCurrentDevice()).resolves.toBe(true)
    expect(mocks.subscribeToWebPush).toHaveBeenCalledWith('AQIDBA', { forceRenew: true })
    expect(mocks.registerSubscription).toHaveBeenCalledWith({
      endpoint: 'https://push.example/new',
      keys: { p256dh: 'p256dh', auth: 'auth' },
      label: 'Chrome on Linux',
      platform: 'Linux',
    })
  })
})

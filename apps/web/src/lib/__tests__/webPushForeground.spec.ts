import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  toast: vi.fn(),
}))

vi.mock('vue-sonner', () => ({ toast: mocks.toast }))

describe('foreground Web Push delivery', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
  })

  it('renders an in-app toast and acknowledges the service worker', async () => {
    let messageListener: ((event: MessageEvent) => void) | undefined
    const addEventListener = vi.fn((type: string, listener: (event: MessageEvent) => void) => {
      if (type === 'message') messageListener = listener
    })
    const getSubscription = vi.fn().mockResolvedValue(null)
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(window, 'Notification', {
      configurable: true,
      value: { permission: 'granted', requestPermission: vi.fn() },
    })
    Object.defineProperty(window, 'PushManager', { configurable: true, value: class {} })
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: {
        addEventListener,
        register: vi.fn().mockResolvedValue({ pushManager: { getSubscription } }),
      },
    })

    const { bootstrapWebPush } = await import('@/lib/webPush')
    await bootstrapWebPush()

    const port = { postMessage: vi.fn() }
    messageListener?.({
      data: {
        type: 'web-push',
        payload: {
          type: 'call',
          id: 'call-1',
          modemId: 'modem-1',
          modem: 'Office',
          from: '10086',
          url: '/modems/modem-1/phone',
          tag: 'call:call-1',
        },
      },
      ports: [port],
    } as unknown as MessageEvent)

    expect(mocks.toast).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        id: 'web-push:call:call-1',
        action: expect.objectContaining({ label: expect.any(String) }),
      }),
    )
    expect(port.postMessage).toHaveBeenCalledWith({ type: 'web-push-rendered' })
  })
})

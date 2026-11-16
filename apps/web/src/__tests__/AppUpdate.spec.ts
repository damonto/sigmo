import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import App from '@/App.vue'
import { useAppUpdate } from '@/composables/useAppUpdate'
import type { UpdateSnapshot } from '@/types/update'

const api = vi.hoisted(() => ({ installation: vi.fn() }))
const toast = vi.hoisted(() => ({ error: vi.fn() }))
vi.mock('@/apis/update', () => ({ useUpdateApi: () => api }))
vi.mock('vue-sonner', () => ({ toast }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/ui/sonner', () => ({ Toaster: { template: '<div />' } }))

const snapshot: UpdateSnapshot = {
  settings: { automatic: false, channel: 'stable' },
  current: {
    version: 'v1.0.0',
    commit: 'old',
    channel: 'stable',
    target: 'linux-amd64',
    distribution: 'standalone',
  },
  state: 'restarting',
  updateAvailable: false,
  selfUpdateSupported: true,
}

const render = () =>
  mount(App, {
    global: {
      stubs: {
        ModemCallProvider: { template: '<slot />' },
        EsimTransferRequestProvider: { template: '<slot />' },
        RouterView: true,
        Toaster: true,
      },
    },
  })

enableAutoUnmount(afterEach)
beforeEach(() => {
  useAppUpdate().dispose()
  vi.useFakeTimers()
  vi.clearAllMocks()
})
afterEach(() => {
  useAppUpdate().dispose()
  vi.useRealTimers()
})

describe('application update ownership', () => {
  it('reports a recovery timeout even when the updates page is not open', async () => {
    api.installation.mockRejectedValue(new TypeError('Failed to fetch'))
    render()
    useAppUpdate().observe(snapshot)

    await vi.advanceTimersByTimeAsync(180_000)
    await flushPromises()

    expect(toast.error).toHaveBeenCalledExactlyOnceWith('settings.updates.recoveryTimedOut')
    await vi.advanceTimersByTimeAsync(10_000)
    expect(toast.error).toHaveBeenCalledTimes(1)
  })

  it('disposes monitoring when the application root is unmounted', async () => {
    const wrapper = render()
    useAppUpdate().observe(snapshot)
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(5000)

    expect(useAppUpdate().updating.value).toBe(false)
    expect(api.installation).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows a localized message for malformed responses', async () => {
    api.installation.mockRejectedValue(new SyntaxError('Unexpected token <'))
    render()
    useAppUpdate().observe(snapshot)

    await vi.advanceTimersByTimeAsync(500)

    expect(toast.error).toHaveBeenCalledExactlyOnceWith('settings.updates.monitorFailed')
    expect(useAppUpdate().updating.value).toBe(false)
  })
})

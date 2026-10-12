import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ActivateView from '@/views/ActivateView.vue'

const appApi = vi.hoisted(() => ({
  ready: vi.fn(),
}))
const licenseApi = vi.hoisted(() => ({
  createPairing: vi.fn(),
  pairing: vi.fn(),
}))
const licenseStore = vi.hoisted(() => ({
  deviceId: '0123456789abcdef0123456789abcdef',
  check: vi.fn(),
}))
const router = vi.hoisted(() => ({
  replace: vi.fn(),
}))

vi.mock('@/apis/app', () => ({
  useAppApi: () => appApi,
}))
vi.mock('@/apis/license', () => ({
  useLicenseApi: () => licenseApi,
}))
vi.mock('@/stores/license', () => ({
  useLicenseStore: () => licenseStore,
}))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRouter: () => router,
}))

describe('ActivateView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    licenseApi.createPairing.mockResolvedValue({
      id: 'pairing-id',
      activationUrl: 'https://t.me/SigmoProBot?start=pairing-id',
      status: 'pending',
      expiresAt: '2026-08-10T12:05:00Z',
    })
    licenseApi.pairing.mockResolvedValue({
      id: 'pairing-id',
      activationUrl: 'https://t.me/SigmoProBot?start=pairing-id',
      status: 'active',
      expiresAt: '2026-08-10T12:05:00Z',
    })
    appApi.ready.mockResolvedValueOnce(false).mockResolvedValueOnce(true)
    licenseStore.check.mockResolvedValue(true)
    router.replace.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('waits for the full server before entering the app after authorization', async () => {
    const wrapper = mount(ActivateView)
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.text()).toContain('activate.success')
    expect(router.replace).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(appApi.ready).toHaveBeenCalledTimes(2)
    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(router.replace).toHaveBeenCalledWith({ name: 'home' })
  })

  it('recovers when pairing creation races the authorization restart', async () => {
    licenseApi.createPairing.mockRejectedValueOnce(new Error('Not Found'))
    appApi.ready.mockReset()
    appApi.ready.mockResolvedValue(true)
    licenseStore.check.mockResolvedValue(true)

    mount(ActivateView)
    await flushPromises()

    expect(appApi.ready).toHaveBeenCalledTimes(1)
    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(router.replace).toHaveBeenCalledWith({ name: 'home' })
  })
})

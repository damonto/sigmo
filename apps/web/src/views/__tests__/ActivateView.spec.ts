import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ActivateView from '@/views/ActivateView.vue'

const licenseApi = vi.hoisted(() => ({
  createPairing: vi.fn(),
  pairing: vi.fn(),
}))
const licenseStore = vi.hoisted(() => ({
  mode: 'authorized',
  deviceId: '0123456789abcdef0123456789abcdef',
  check: vi.fn(),
}))
const router = vi.hoisted(() => ({
  replace: vi.fn(),
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
    licenseStore.mode = 'authorized'
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

    licenseStore.check.mockResolvedValue(true)
    router.replace.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('enters the app after authorization without a restart', async () => {
    const wrapper = mount(ActivateView)
    await flushPromises()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.text()).toContain('activate.success')

    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(router.replace).toHaveBeenCalledWith({ name: 'home' })
  })

  it('recovers when pairing creation loses the success response', async () => {
    licenseApi.createPairing.mockRejectedValueOnce(new Error('Not Found'))

    licenseStore.check.mockResolvedValue(true)

    mount(ActivateView)
    await flushPromises()

    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(router.replace).toHaveBeenCalledWith({ name: 'home' })
  })

  it('keeps optional service activation open until the device is authorized', async () => {
    licenseStore.mode = 'unlicensed'
    licenseApi.createPairing.mockRejectedValueOnce(new Error('pairing unavailable'))

    mount(ActivateView)
    await flushPromises()
    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(router.replace).not.toHaveBeenCalled()
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'

const licenseStore = vi.hoisted(() => ({
  activationRequired: false,
  unavailable: false,
  check: vi.fn(),
}))

const authStore = vi.hoisted(() => ({
  fetchOtpRequirement: vi.fn(),
}))

const authStorage = vi.hoisted(() => ({
  getStoredToken: vi.fn(),
}))

vi.mock('@/stores/license', () => ({
  useLicenseStore: () => licenseStore,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/lib/authStorage', () => authStorage)

import { enforceRouteAccess } from '@/router'

describe('route access', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    licenseStore.activationRequired = false
    licenseStore.unavailable = false
    licenseStore.check.mockResolvedValue(true)
    authStorage.getStoredToken.mockReturnValue(null)
    authStore.fetchOtpRequirement.mockResolvedValue(false)
  })

  it('redirects to activation before reading OTP state', async () => {
    licenseStore.check.mockImplementation(async () => {
      licenseStore.activationRequired = true
      return false
    })

    await expect(enforceRouteAccess({ name: 'home' })).resolves.toEqual({ name: 'activate' })
    expect(licenseStore.check).toHaveBeenCalledWith(true)
    expect(authStorage.getStoredToken).not.toHaveBeenCalled()
    expect(authStore.fetchOtpRequirement).not.toHaveBeenCalled()
  })

  it('checks OTP only after product authorization succeeds', async () => {
    const order: string[] = []
    licenseStore.check.mockImplementation(async () => {
      order.push('license')
      return true
    })
    authStorage.getStoredToken.mockImplementation(() => {
      order.push('token')
      return null
    })
    authStore.fetchOtpRequirement.mockImplementation(async () => {
      order.push('otp')
      return true
    })

    await expect(enforceRouteAccess({ name: 'home' })).resolves.toEqual({ name: 'auth' })
    expect(order).toEqual(['license', 'token', 'otp'])
  })

  it('keeps business routes closed when authorization status is unavailable', async () => {
    licenseStore.check.mockImplementation(async () => {
      licenseStore.unavailable = true
      return false
    })

    await expect(enforceRouteAccess({ name: 'home' })).resolves.toEqual({
      name: 'license-unavailable',
    })
    expect(authStorage.getStoredToken).not.toHaveBeenCalled()
    expect(authStore.fetchOtpRequirement).not.toHaveBeenCalled()
  })

  it('keeps the activation page unavailable after authorization', async () => {
    await expect(enforceRouteAccess({ name: 'activate' })).resolves.toEqual({ name: 'home' })
    expect(authStore.fetchOtpRequirement).not.toHaveBeenCalled()
  })
})

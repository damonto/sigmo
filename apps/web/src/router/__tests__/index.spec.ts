import { beforeEach, describe, expect, it, vi } from 'vitest'
const licenseStore = vi.hoisted(() => ({
  authorized: false,
  mode: 'unlicensed',
  unavailable: false,
  check: vi.fn(),
}))
const authStore = vi.hoisted(() => ({ check: vi.fn() }))
vi.mock('@/stores/license', () => ({ useLicenseStore: () => licenseStore }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/layouts/ModemLayout.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/ModemCallsView.vue', () => ({ default: { render: () => null } }))
import router, { enforceRouteAccess } from '@/router'

describe('route access', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    licenseStore.authorized = false
    licenseStore.unavailable = false
    licenseStore.check.mockResolvedValue(true)
    authStore.check.mockResolvedValue(true)
  })
  it('requires authentication before reading licensed resources', async () => {
    authStore.check.mockResolvedValue(false)
    await expect(enforceRouteAccess({ name: 'home' })).resolves.toEqual({ name: 'auth' })
    expect(licenseStore.check).not.toHaveBeenCalled()
  })
  it('checks the license after authentication', async () => {
    licenseStore.authorized = false
    await expect(enforceRouteAccess({ name: 'home' })).resolves.toBeUndefined()
    expect(licenseStore.check).toHaveBeenCalledWith(true)
  })
  it('keeps base routes accessible while authorization is unavailable', async () => {
    licenseStore.unavailable = true
    await expect(enforceRouteAccess({ name: 'home' })).resolves.toBeUndefined()
  })
  it('leaves the activation page after authorization', async () => {
    licenseStore.authorized = true
    await expect(enforceRouteAccess({ name: 'activate' })).resolves.toEqual({ name: 'home' })
  })
  it('uses Calls as the modem route and redirects old notification links', async () => {
    expect(router.resolve({ name: 'modem-calls', params: { id: 'modem-1' } }).path).toBe(
      '/modems/modem-1/calls',
    )
    await router.push('/modems/modem-1/phone?source=push#recent')
    expect(router.currentRoute.value.name).toBe('modem-calls')
    expect(router.currentRoute.value.fullPath).toBe('/modems/modem-1/calls?source=push#recent')
  })

  it('exposes backend contacts within the modem layout', () => {
    expect(router.resolve({ name: 'modem-contacts', params: { id: 'modem-1' } }).path).toBe(
      '/modems/modem-1/contacts',
    )
    const detail = router.resolve({
      name: 'modem-contact',
      params: { id: 'modem-1', contactId: 'alice' },
    })
    expect(detail.path).toBe('/modems/modem-1/contacts/alice')
    expect(detail.matched[0]?.path).toBe('/modems/:id')
  })
})

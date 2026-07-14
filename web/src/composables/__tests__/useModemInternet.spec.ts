import { computed, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemInternet } from '@/composables/useModemInternet'
import type { InternetConnectionResponse } from '@/types/internet'

const api = vi.hoisted(() => ({
  getCurrentConnection: vi.fn(),
  getPublic: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  updatePreferences: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/apis/internet', () => ({
  useInternetApi: () => api,
}))

const connected = (
  overrides: Partial<InternetConnectionResponse> = {},
): InternetConnectionResponse => ({
  status: 'connected',
  apn: 'internet',
  ipType: 'ipv4v6',
  apnUsername: '',
  apnPassword: '',
  apnAuth: '',
  defaultRoute: true,
  proxyEnabled: false,
  alwaysOn: false,
  proxy: { enabled: false },
  interfaceName: 'wwan0',
  bearer: '/bearer/1',
  ipv4Addresses: ['10.0.0.2/30'],
  ipv6Addresses: [],
  dns: ['1.1.1.1'],
  durationSeconds: 10,
  txBytes: 20,
  rxBytes: 30,
  routeMetric: 10,
  ...overrides,
})

describe('useModemInternet', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    api.getCurrentConnection.mockResolvedValue({ data: { value: connected() } })
    api.getPublic.mockResolvedValue({ data: { value: {} } })
    api.disconnect.mockResolvedValue({ data: { value: undefined } })
  })

  it('optimistically saves connected preferences and applies the response', async () => {
    let resolveUpdate:
      | ((value: { data: { value: InternetConnectionResponse } }) => void)
      | undefined
    api.updatePreferences.mockReturnValue(
      new Promise((resolve) => {
        resolveUpdate = resolve
      }),
    )
    const onSuccess = vi.fn()
    const internet = useModemInternet({
      modemId: computed(() => 'modem-1'),
      onSuccess,
    })
    await vi.waitFor(() => expect(internet.isInternetConnected.value).toBe(true))

    const update = internet.handleInternetPreferencesUpdate({
      defaultRoute: false,
      proxyEnabled: true,
      alwaysOn: true,
    })

    expect(internet.internetDefaultRoute.value).toBe(false)
    expect(internet.internetProxyEnabled.value).toBe(true)
    expect(internet.internetAlwaysOn.value).toBe(true)
    expect(internet.isInternetPreferencesUpdating.value).toBe(true)
    expect(api.updatePreferences).toHaveBeenCalledWith('modem-1', {
      defaultRoute: false,
      proxyEnabled: true,
      alwaysOn: true,
    })

    resolveUpdate?.({
      data: {
        value: connected({
          defaultRoute: false,
          proxyEnabled: true,
          alwaysOn: true,
          routeMetric: 1000,
        }),
      },
    })
    await update

    expect(internet.internetConnection.value?.routeMetric).toBe(1000)
    expect(internet.isInternetPreferencesUpdating.value).toBe(false)
    expect(onSuccess).toHaveBeenCalledWith('modemDetail.settings.internetPreferencesSuccess')
  })

  it('restores the previous preferences when saving fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const onError = vi.fn()
    api.updatePreferences.mockRejectedValue(new Error('offline'))
    const internet = useModemInternet({
      modemId: computed(() => 'modem-1'),
      onError,
    })
    await vi.waitFor(() => expect(internet.isInternetConnected.value).toBe(true))

    await internet.handleInternetPreferencesUpdate({
      defaultRoute: false,
      proxyEnabled: true,
      alwaysOn: true,
    })

    expect(internet.internetDefaultRoute.value).toBe(true)
    expect(internet.internetProxyEnabled.value).toBe(false)
    expect(internet.internetAlwaysOn.value).toBe(false)
    expect(onError).toHaveBeenCalledWith('modemDetail.settings.internetPreferencesUpdateFailed')
    consoleError.mockRestore()
  })

  it('discards a poll response started before saving preferences', async () => {
    vi.useFakeTimers()
    let resolvePoll: ((value: { data: { value: InternetConnectionResponse } }) => void) | undefined
    api.getCurrentConnection
      .mockResolvedValueOnce({ data: { value: connected() } })
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolvePoll = resolve
        }),
      )
    api.updatePreferences.mockResolvedValue({
      data: {
        value: connected({ defaultRoute: false, proxyEnabled: true, alwaysOn: true }),
      },
    })
    const internet = useModemInternet({ modemId: computed(() => 'modem-1') })
    await vi.advanceTimersByTimeAsync(0)
    await Promise.resolve()
    expect(internet.isInternetConnected.value).toBe(true)

    await vi.advanceTimersByTimeAsync(5000)
    expect(api.getCurrentConnection).toHaveBeenCalledTimes(2)
    await internet.handleInternetPreferencesUpdate({
      defaultRoute: false,
      proxyEnabled: true,
      alwaysOn: true,
    })
    resolvePoll?.({ data: { value: connected() } })
    await Promise.resolve()

    expect(internet.internetDefaultRoute.value).toBe(false)
    expect(internet.internetProxyEnabled.value).toBe(true)
    expect(internet.internetAlwaysOn.value).toBe(true)
  })

  it('does not roll back preferences onto a newly selected modem', async () => {
    let rejectUpdate: ((reason?: unknown) => void) | undefined
    api.getCurrentConnection
      .mockResolvedValueOnce({ data: { value: connected() } })
      .mockResolvedValueOnce({
        data: {
          value: connected({ defaultRoute: false, proxyEnabled: true, alwaysOn: true }),
        },
      })
    api.updatePreferences.mockReturnValueOnce(
      new Promise((_, reject) => {
        rejectUpdate = reject
      }),
    )
    const modemId = ref('modem-1')
    const onError = vi.fn()
    const internet = useModemInternet({
      modemId: computed(() => modemId.value),
      onError,
    })
    await vi.waitFor(() => expect(internet.isInternetConnected.value).toBe(true))

    const update = internet.handleInternetPreferencesUpdate({
      defaultRoute: false,
      proxyEnabled: false,
      alwaysOn: false,
    })
    modemId.value = 'modem-2'
    await vi.waitFor(() => expect(api.getCurrentConnection).toHaveBeenCalledTimes(2))
    rejectUpdate?.(new Error('offline'))
    await update

    expect(internet.internetDefaultRoute.value).toBe(false)
    expect(internet.internetProxyEnabled.value).toBe(true)
    expect(internet.internetAlwaysOn.value).toBe(true)
    expect(onError).not.toHaveBeenCalled()
  })
})

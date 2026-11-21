import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemWiFiCallingSettings } from '@/composables/useModemWiFiCallingSettings'

const api = vi.hoisted(() => ({
  getWiFiCallingSettings: vi.fn(),
  updateWiFiCallingSettings: vi.fn(),
  createWiFiCallingSession: vi.fn(),
  deleteWiFiCallingSession: vi.fn(),
  startWiFiCallingWebsheet: vi.fn(),
  startWiFiCallingEmergencyAddressWebsheet: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

describe('useModemWiFiCallingSettings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.getWiFiCallingSettings.mockResolvedValue({
      data: {
        value: {
          enabled: true,
          underlay: {
            mode: 'modem',
            modemId: 'uplink-1',
          },
          connected: false,
          state: 'websheet_required',
          durationSeconds: 0,
          emergencyAddressUpdateAvailable: true,
          websheet: {
            id: 'sheet-1',
            embedUrl: '/api/v1/websheets/sheet-1',
            url: 'https://example.com/setup',
            method: 'GET',
          },
        },
      },
    })
    api.updateWiFiCallingSettings.mockResolvedValue({ data: { value: undefined } })
    api.createWiFiCallingSession.mockResolvedValue({ data: { value: undefined } })
    api.deleteWiFiCallingSession.mockResolvedValue({ data: { value: undefined } })
  })

  it('loads a connection failure and clears it when switching modems', async () => {
    api.getWiFiCallingSettings.mockResolvedValueOnce({
      data: {
        value: {
          enabled: true,
          state: 'connecting',
          lastError: 'IPsec integrity algorithms are required',
        },
      },
    })
    const modemId = ref('modem-1')
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => modemId.value),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingLastError.value).toBe(
        'IPsec integrity algorithms are required',
      )
    })
    modemId.value = 'modem-2'
    await vi.waitFor(() => expect(api.getWiFiCallingSettings).toHaveBeenCalledTimes(2))
    expect(settings.settingsWiFiCallingLastError.value).toBe('')
  })

  it('loads pending carrier websheet state', async () => {
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })

    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingState.value).toBe('websheet_required')
    })
    expect(settings.settingsWiFiCallingState.value).toBe('websheet_required')
    expect(settings.settingsWiFiCallingConnected.value).toBe(false)
    expect(settings.settingsWiFiCallingUnderlay.value).toEqual({
      mode: 'modem',
      modemId: 'uplink-1',
    })
    expect(settings.settingsWiFiCallingEmergencyAddressUpdateAvailable.value).toBe(true)
    expect(settings.settingsWiFiCallingWebsheet.value).toBeNull()
  })

  it('starts a carrier websheet session', async () => {
    api.startWiFiCallingWebsheet.mockResolvedValue({
      data: {
        value: {
          id: 'sheet-2',
          embedUrl: '/api/v1/websheets/sheet-2',
          url: 'https://example.com/setup',
          method: 'GET',
        },
      },
    })
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.getWiFiCallingSettings).toHaveBeenCalled()
    })

    await settings.startWiFiCallingWebsheet()

    expect(api.startWiFiCallingWebsheet).toHaveBeenCalledWith('modem-1')
    expect(settings.settingsWiFiCallingWebsheet.value?.id).toBe('sheet-2')
  })

  it('starts an E911 websheet session without replacing carrier setup state', async () => {
    api.startWiFiCallingEmergencyAddressWebsheet.mockResolvedValue({
      data: {
        value: {
          id: 'sheet-e911',
          embedUrl: '/api/v1/websheets/sheet-e911',
          url: 'https://example.com/e911',
          method: 'POST',
        },
      },
    })
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.getWiFiCallingSettings).toHaveBeenCalled()
    })

    await settings.startWiFiCallingEmergencyAddressWebsheet()

    expect(api.startWiFiCallingEmergencyAddressWebsheet).toHaveBeenCalledWith('modem-1')
    expect(settings.settingsWiFiCallingEmergencyAddressWebsheet.value?.id).toBe('sheet-e911')
    expect(settings.settingsWiFiCallingWebsheet.value).toBeNull()
  })

  it('reconnects without saving Wi-Fi Calling settings', async () => {
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.getWiFiCallingSettings).toHaveBeenCalled()
    })

    await settings.reconnectWiFiCalling()

    expect(api.createWiFiCallingSession).toHaveBeenCalledWith('modem-1')
    expect(api.updateWiFiCallingSettings).not.toHaveBeenCalled()
  })

  it('saves switch changes immediately', async () => {
    const onSuccess = vi.fn()
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
      onSuccess,
    })
    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
    })

    await settings.updateWiFiCallingSettings({ enabled: false })

    expect(api.updateWiFiCallingSettings).toHaveBeenCalledWith('modem-1', {
      enabled: false,
      underlay: { mode: 'system' },
    })
    expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
    expect(onSuccess).toHaveBeenCalledWith('modemDetail.settings.wifiCallingSuccess')
  })

  it('restores confirmed settings when automatic saving fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const onError = vi.fn()
    api.updateWiFiCallingSettings.mockRejectedValue(new Error('offline'))
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
      onError,
    })
    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
    })

    await settings.updateWiFiCallingSettings({ enabled: false })

    expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
    expect(onError).toHaveBeenCalledWith('modemDetail.settings.wifiCallingUpdateFailed')
    consoleError.mockRestore()
  })

  it('keeps the proxy on if restoring the direct uplink fails', async () => {
    const loggedError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const directUnderlay = { mode: 'modem', modemId: 'uplink-1' } as const
    const active = { mode: 'socks5', proxyId: 'p-1', directUnderlay } as const
    api.getWiFiCallingSettings.mockResolvedValueOnce({
      data: { value: { enabled: true, underlay: active } },
    })
    api.updateWiFiCallingSettings.mockRejectedValueOnce(new Error('offline'))
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => expect(settings.settingsWiFiCallingUnderlay.value).toEqual(active))

    await settings.updateWiFiCallingSettings({
      enabled: true,
      underlay: { ...directUnderlay, proxyId: 'p-1' },
    })

    expect(settings.settingsWiFiCallingUnderlay.value).toEqual(active)
    loggedError.mockRestore()
  })

  it('keeps a committed proxy setting when the readback fails', async () => {
    const onSuccess = vi.fn()
    const onError = vi.fn()
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
      onSuccess,
      onError,
    })
    await vi.waitFor(() => expect(settings.hasWiFiCallingSettings.value).toBe(true))

    api.getWiFiCallingSettings.mockRejectedValueOnce(new Error('status refresh failed'))
    const underlay = { mode: 'socks5', proxyId: 'p-1' } as const
    expect(await settings.updateWiFiCallingSettings({ enabled: true, underlay })).toBe(true)

    expect(settings.settingsWiFiCallingUnderlay.value).toEqual(underlay)
    expect(settings.savedWiFiCallingProxyId.value).toBe('p-1')
    expect(onSuccess).toHaveBeenCalledOnce()
    expect(onError).not.toHaveBeenCalled()
  })

  it('waits for a disabled SIM’s saved proxy before enabling it from a shortcut', async () => {
    const enabled = ref(false)
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => enabled.value),
    })
    const underlay = { mode: 'socks5', proxyId: 'p-1', directUnderlay: { mode: 'self' } } as const
    const readback = { data: { value: { enabled: true, underlay } } }
    api.getWiFiCallingSettings.mockResolvedValueOnce({
      data: { value: { enabled: false, underlay } },
    })
    api.getWiFiCallingSettings.mockResolvedValueOnce(readback)

    enabled.value = true
    expect(await settings.ensureWiFiCallingEnabled()).toBe(true)

    expect(api.updateWiFiCallingSettings).toHaveBeenCalledExactlyOnceWith('modem-1', {
      enabled: true,
      underlay,
    })
    expect(settings.settingsWiFiCallingUnderlay.value).toEqual(underlay)
  })

  it('loads the next SIM while the previous SIM’s request is pending', async () => {
    let resolvePrevious: ((value: { data: { value: { enabled: boolean } } }) => void) | undefined
    const initialRead = new Promise<{ data: { value: { enabled: boolean } } }>((resolve) => {
      resolvePrevious = resolve
    })
    api.getWiFiCallingSettings.mockReturnValueOnce(initialRead)
    const simId = ref('sim-1')
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      simId: computed(() => simId.value),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => expect(api.getWiFiCallingSettings).toHaveBeenCalledTimes(1))
    simId.value = 'sim-2'
    await vi.waitFor(() => expect(settings.hasWiFiCallingSettings.value).toBe(true))
    expect(api.getWiFiCallingSettings).toHaveBeenCalledTimes(2)

    resolvePrevious?.({ data: { value: { enabled: false } } })
    await initialRead
    expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
    expect(settings.settingsWiFiCallingUnderlay.value).toEqual({
      mode: 'modem',
      modemId: 'uplink-1',
    })
  })

  it('discards a settings response started before automatic saving', async () => {
    let resolveFetch:
      ((value: { data: { value: { enabled: boolean; connected: boolean } } }) => void) | undefined
    api.getWiFiCallingSettings.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveFetch = resolve
      }),
    )
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => expect(api.getWiFiCallingSettings).toHaveBeenCalledTimes(1))

    api.getWiFiCallingSettings.mockResolvedValueOnce({
      data: { value: { enabled: true, connected: false } },
    })
    await settings.updateWiFiCallingSettings({ enabled: true })
    resolveFetch?.({
      data: { value: { enabled: false, connected: false } },
    })
    await vi.waitFor(() => expect(settings.isWiFiCallingSettingsUpdating.value).toBe(false))

    expect(settings.settingsWiFiCallingEnabled.value).toBe(true)
  })

  it('does not roll back settings onto a newly selected modem', async () => {
    let rejectUpdate: ((reason?: unknown) => void) | undefined
    api.getWiFiCallingSettings
      .mockResolvedValueOnce({
        data: { value: { enabled: true, connected: false } },
      })
      .mockResolvedValueOnce({
        data: { value: { enabled: false, connected: false } },
      })
    api.updateWiFiCallingSettings.mockReturnValueOnce(
      new Promise((_, reject) => {
        rejectUpdate = reject
      }),
    )
    const modemId = ref('modem-1')
    const onError = vi.fn()
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => modemId.value),
      enabled: computed(() => true),
      onError,
    })
    await vi.waitFor(() => expect(settings.settingsWiFiCallingEnabled.value).toBe(true))

    const update = settings.updateWiFiCallingSettings({ enabled: false })
    modemId.value = 'modem-2'
    await vi.waitFor(() => expect(api.getWiFiCallingSettings).toHaveBeenCalledTimes(2))
    rejectUpdate?.(new Error('offline'))
    await update

    expect(settings.settingsWiFiCallingEnabled.value).toBe(false)
    expect(onError).not.toHaveBeenCalled()
  })

  it('restores the previous state when reconnect fails before starting', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    api.createWiFiCallingSession.mockRejectedValue(new Error('offline'))
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingState.value).toBe('websheet_required')
    })

    await settings.reconnectWiFiCalling()

    expect(settings.settingsWiFiCallingState.value).toBe('websheet_required')
    expect(settings.settingsWiFiCallingConnected.value).toBe(false)
    expect(settings.settingsWiFiCallingDurationSeconds.value).toBe(0)
    consoleError.mockRestore()
  })

  it('disconnects the current Wi-Fi Calling session', async () => {
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.getWiFiCallingSettings).toHaveBeenCalled()
    })

    await settings.disconnectWiFiCalling()

    expect(api.deleteWiFiCallingSession).toHaveBeenCalledWith('modem-1')
    expect(settings.settingsWiFiCallingConnected.value).toBe(false)
  })

  it('restores the previous state when disconnect fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getWiFiCallingSettings.mockResolvedValue({
      data: {
        value: {
          enabled: true,
          connected: true,
          state: 'connected',
          durationSeconds: 25,
          emergencyAddressUpdateAvailable: false,
        },
      },
    })
    api.deleteWiFiCallingSession.mockRejectedValue(new Error('offline'))
    const settings = useModemWiFiCallingSettings({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(settings.settingsWiFiCallingConnected.value).toBe(true)
    })

    await settings.disconnectWiFiCalling()

    expect(settings.settingsWiFiCallingConnected.value).toBe(true)
    expect(settings.settingsWiFiCallingState.value).toBe('connected')
    expect(settings.settingsWiFiCallingDurationSeconds.value).toBe(25)
    consoleError.mockRestore()
  })
})

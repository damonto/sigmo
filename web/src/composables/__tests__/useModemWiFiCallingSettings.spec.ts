import { computed } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemWiFiCallingSettings } from '@/composables/useModemWiFiCallingSettings'

const api = vi.hoisted(() => ({
  getWiFiCallingSettings: vi.fn(),
  updateWiFiCallingSettings: vi.fn(),
  createWiFiCallingSession: vi.fn(),
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
    vi.clearAllMocks()
    api.getWiFiCallingSettings.mockResolvedValue({
      data: {
        value: {
          enabled: true,
          preferred: true,
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
})

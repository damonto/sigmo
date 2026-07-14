import { computed } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemVoLTE } from '@/composables/useModemVoLTE'

const api = vi.hoisted(() => ({
  settings: vi.fn(),
  updateSettings: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/apis/volte', () => ({
  useVoLTEApi: () => api,
}))

describe('useModemVoLTE', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.settings.mockResolvedValue({
      data: {
        value: {
          enabled: false,
          connected: false,
          state: 'idle',
          durationSeconds: 0,
          modemRegistered: false,
          networkDriver: 'legacy_bam_dmux',
          setIMSAPNAsDefault: false,
          enablePCSCFViaPCO: false,
        },
      },
    })
    api.updateSettings.mockResolvedValue({ data: { value: undefined } })
  })

  it('loads and sends the selected network driver when enabling VoLTE', async () => {
    const settings = useModemVoLTE({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(settings.volteNetworkDriver.value).toBe('legacy_bam_dmux')
    })

    await settings.updateVoLTE(true)

    expect(api.updateSettings).toHaveBeenCalledWith('modem-1', {
      enabled: true,
      networkDriver: 'legacy_bam_dmux',
      setIMSAPNAsDefault: false,
      enablePCSCFViaPCO: false,
    })
  })

  it('persists a driver change while VoLTE is disabled', async () => {
    const settings = useModemVoLTE({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.settings).toHaveBeenCalled()
    })

    await settings.updateNetworkDriver('qmap')

    expect(api.updateSettings).toHaveBeenCalledWith('modem-1', {
      enabled: false,
      networkDriver: 'qmap',
      setIMSAPNAsDefault: false,
      enablePCSCFViaPCO: false,
    })
  })

  it('persists IMS profile options while VoLTE is disabled', async () => {
    const settings = useModemVoLTE({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(api.settings).toHaveBeenCalled()
    })

    await settings.updateProfileOptions({
      setIMSAPNAsDefault: true,
      enablePCSCFViaPCO: true,
    })

    expect(api.updateSettings).toHaveBeenCalledWith('modem-1', {
      enabled: false,
      networkDriver: 'legacy_bam_dmux',
      setIMSAPNAsDefault: true,
      enablePCSCFViaPCO: true,
    })
  })

  it('does not send a QMI network driver for MBIM', async () => {
    api.settings.mockResolvedValue({
      data: {
        value: {
          enabled: false,
          connected: false,
          state: 'idle',
          durationSeconds: 0,
          modemRegistered: false,
          networkDriver: 'mbim',
          setIMSAPNAsDefault: false,
          enablePCSCFViaPCO: false,
        },
      },
    })
    const settings = useModemVoLTE({
      modemId: computed(() => 'modem-1'),
      enabled: computed(() => true),
    })
    await vi.waitFor(() => {
      expect(settings.volteNetworkDriver.value).toBe('mbim')
    })

    await settings.updateVoLTE(true)

    expect(api.updateSettings).toHaveBeenCalledWith('modem-1', {
      enabled: true,
      setIMSAPNAsDefault: false,
      enablePCSCFViaPCO: false,
    })
  })
})

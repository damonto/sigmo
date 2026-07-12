import { computed } from 'vue'
import { describe, expect, it, beforeEach, vi } from 'vitest'

import { useModemNetwork } from '@/composables/useModemNetwork'

const api = vi.hoisted(() => ({
  scanNetworks: vi.fn(),
  registerNetwork: vi.fn(),
  getModes: vi.fn(),
  setCurrentModes: vi.fn(),
  getBands: vi.fn(),
  setCurrentBands: vi.fn(),
  getAirplaneMode: vi.fn(),
  setAirplaneMode: vi.fn(),
  getVoLTE: vi.fn(),
  setVoLTE: vi.fn(),
}))

vi.mock('@/apis/network', () => ({
  useNetworkApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const modemId = computed(() => 'modem-1')

const modeResponse = {
  supported: [],
  current: {
    allowed: 0,
    preferred: 0,
    allowedLabel: 'Any',
    preferredLabel: 'None',
    current: true,
  },
}

const bandsResponse = {
  supported: [],
  current: [],
}

const airplaneModeResponse = {
  supported: true,
  enabled: false,
}

const volteResponse = {
  managed: false,
  canEnable: true,
  modemRegistered: false,
}

describe('useModemNetwork', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getModes.mockResolvedValue({ data: { value: modeResponse } })
    api.getBands.mockResolvedValue({ data: { value: bandsResponse } })
    api.getAirplaneMode.mockResolvedValue({ data: { value: airplaneModeResponse } })
    api.getVoLTE.mockResolvedValue({ data: { value: volteResponse } })
    api.setAirplaneMode.mockResolvedValue({})
    api.setVoLTE.mockResolvedValue({})
  })

  it('opens the network dialog after a successful scan', async () => {
    api.scanNetworks.mockResolvedValue({
      data: {
        value: [
          {
            status: 'available',
            operatorName: 'Carrier',
            operatorShortName: 'Carrier',
            operatorCode: '00101',
            accessTechnologies: ['lte'],
          },
        ],
      },
    })

    const network = useModemNetwork({ modemId })

    await network.openNetworkDialog()

    expect(network.networkDialogOpen.value).toBe(true)
    expect(network.availableNetworks.value).toHaveLength(1)
    expect(network.isNetworkLoading.value).toBe(false)
  })

  it('keeps the network dialog closed when scan fails', async () => {
    api.scanNetworks.mockRejectedValue(new Error('gateway timeout'))

    const network = useModemNetwork({ modemId })
    network.networkDialogOpen.value = true

    await network.openNetworkDialog()

    expect(network.networkDialogOpen.value).toBe(false)
    expect(network.availableNetworks.value).toEqual([])
    expect(network.isNetworkLoading.value).toBe(false)
  })

  it('updates airplane mode and refreshes modem state', async () => {
    const onChanged = vi.fn()
    const onSuccess = vi.fn()
    const network = useModemNetwork({ modemId, onChanged, onSuccess })

    await network.refreshNetworkSettings()
    await network.handleAirplaneModeUpdate(true)

    expect(api.setAirplaneMode).toHaveBeenCalledWith('modem-1', { enabled: true })
    expect(api.getAirplaneMode).toHaveBeenCalled()
    expect(onChanged).toHaveBeenCalledWith('modem-1')
    expect(onSuccess).toHaveBeenCalledWith('modemDetail.settings.networkAirplaneModeEnabledSuccess')
    expect(network.isAirplaneModeUpdating.value).toBe(false)
  })

  it('notifies when airplane mode update fails', async () => {
    api.setAirplaneMode.mockRejectedValue(new Error('radio busy'))
    const onError = vi.fn()
    const onSuccess = vi.fn()
    const network = useModemNetwork({ modemId, onError, onSuccess })

    await network.refreshNetworkSettings()
    await network.handleAirplaneModeUpdate(true)

    expect(onError).toHaveBeenCalledWith('modemDetail.settings.networkAirplaneModeUpdateFailed')
    expect(onSuccess).not.toHaveBeenCalled()
    expect(network.isAirplaneModeUpdating.value).toBe(false)
  })

  it('updates volte and refreshes modem state', async () => {
    const onChanged = vi.fn()
    const onSuccess = vi.fn()
    const network = useModemNetwork({ modemId, onChanged, onSuccess })

    await network.refreshNetworkSettings()
    await network.handleVoLTEUpdate(true)

    expect(api.setVoLTE).toHaveBeenCalledWith('modem-1', { managed: true })
    expect(api.getVoLTE).toHaveBeenCalled()
    expect(onChanged).toHaveBeenCalledWith('modem-1')
    expect(onSuccess).toHaveBeenCalledWith('modemDetail.settings.networkVoLTEEnabledSuccess')
    expect(network.isVoLTEUpdating.value).toBe(false)
  })

  it('allows VoLTE takeover when IMSA status is available', async () => {
    api.getVoLTE.mockResolvedValue({
      data: { value: { managed: false, canEnable: true, modemRegistered: false } },
    })
    const network = useModemNetwork({ modemId })

    await network.refreshNetworkSettings()

    expect(network.volteManaged.value).toBe(false)
    expect(network.volteCanEnable.value).toBe(true)
    expect(network.canUpdateVoLTE.value).toBe(true)
  })

  it('disables VoLTE takeover when IMSA status is unavailable', async () => {
    api.getVoLTE.mockResolvedValue({
      data: { value: { managed: false, canEnable: false, modemRegistered: false } },
    })
    const network = useModemNetwork({ modemId })

    await network.refreshNetworkSettings()

    expect(network.volteCanEnable.value).toBe(false)
    expect(network.canUpdateVoLTE.value).toBe(false)
  })

  it('exposes modem IMS registration status', async () => {
    api.getVoLTE.mockResolvedValue({
      data: { value: { managed: false, canEnable: true, modemRegistered: true } },
    })
    const network = useModemNetwork({ modemId })

    await network.refreshNetworkSettings()

    expect(network.volteModemRegistered.value).toBe(true)
  })
})

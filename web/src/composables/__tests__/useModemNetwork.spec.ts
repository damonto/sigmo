import { computed, nextTick, ref } from 'vue'
import { describe, expect, it, beforeEach, vi } from 'vitest'

import { useModemNetwork } from '@/composables/useModemNetwork'

const api = vi.hoisted(() => ({
  scanNetworks: vi.fn(),
  startNetworkScan: vi.fn(),
  getNetworkScan: vi.fn(),
  registerNetwork: vi.fn(),
  getModes: vi.fn(),
  setCurrentModes: vi.fn(),
  getBands: vi.fn(),
  setCurrentBands: vi.fn(),
  getAirplaneMode: vi.fn(),
  setAirplaneMode: vi.fn(),
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
  supported: [
    {
      allowed: 4,
      preferred: 0,
      allowedLabel: 'LTE',
      preferredLabel: 'None',
      current: true,
    },
  ],
  current: {
    allowed: 4,
    preferred: 0,
    allowedLabel: 'LTE',
    preferredLabel: 'None',
    current: true,
  },
}

const bandsResponse = {
  supported: [
    { value: { technology: 4, number: 41 }, label: 'LTE B41', current: true },
    { value: { technology: 4, number: 42 }, label: 'LTE B42', current: false },
  ],
  current: [{ technology: 4, number: 41 }],
}

const airplaneModeResponse = {
  supported: true,
  enabled: false,
}

describe('useModemNetwork', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getModes.mockResolvedValue({ data: { value: modeResponse } })
    api.getBands.mockResolvedValue({ data: { value: bandsResponse } })
    api.getAirplaneMode.mockResolvedValue({ data: { value: airplaneModeResponse } })
    api.setAirplaneMode.mockResolvedValue({})
  })

  it('opens the network dialog after a successful scan', async () => {
    api.startNetworkScan.mockResolvedValue({
      data: {
        value: {
          id: 'scan-1',
          status: 'completed',
          networks: [
            {
              status: 'available',
              operatorName: 'Carrier',
              operatorShortName: 'Carrier',
              operatorCode: '00101',
              accessTechnologies: ['lte'],
            },
          ],
        },
      },
    })

    const network = useModemNetwork({ modemId })

    await network.openNetworkDialog()

    expect(network.networkDialogOpen.value).toBe(true)
    expect(network.availableNetworks.value).toHaveLength(1)
    expect(network.isNetworkLoading.value).toBe(false)
  })

  it('keeps the network dialog closed when scan fails', async () => {
    api.startNetworkScan.mockRejectedValue(new Error('gateway timeout'))

    const network = useModemNetwork({ modemId })
    network.networkDialogOpen.value = true

    await network.openNetworkDialog()

    expect(network.networkDialogOpen.value).toBe(false)
    expect(network.availableNetworks.value).toEqual([])
    expect(network.isNetworkLoading.value).toBe(false)
  })

  it('polls a running scan task until it completes', async () => {
    api.startNetworkScan.mockResolvedValue({
      data: { value: { id: 'scan-1', status: 'running' } },
    })
    api.getNetworkScan.mockResolvedValue({
      data: {
        value: {
          id: 'scan-1',
          status: 'completed',
          networks: [],
        },
      },
    })

    const network = useModemNetwork({ modemId })

    await network.openNetworkDialog()

    expect(api.getNetworkScan).toHaveBeenCalledWith('modem-1', 'scan-1')
    expect(network.networkDialogOpen.value).toBe(true)
    expect(network.isNetworkLoading.value).toBe(false)
  })

  it('stops local polling without canceling the shared server task', async () => {
    const activeModemId = ref('modem-1')
    api.startNetworkScan.mockResolvedValue({
      data: { value: { id: 'scan-1', status: 'running' } },
    })
    api.getNetworkScan.mockResolvedValue({
      data: { value: { id: 'scan-1', status: 'running' } },
    })
    const network = useModemNetwork({ modemId: computed(() => activeModemId.value) })

    const scan = network.openNetworkDialog()
    await vi.waitFor(() => expect(api.getNetworkScan).toHaveBeenCalledTimes(1))

    activeModemId.value = 'modem-2'
    await nextTick()
    await scan

    expect(network.networkDialogOpen.value).toBe(false)
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

  it('toggles concrete bands', () => {
    const network = useModemNetwork({ modemId })
    const lte41 = { technology: 4, number: 41 }
    const lte42 = { technology: 4, number: 42 }

    network.selectedBands.value = [lte41]
    network.toggleBand(lte42, true)
    network.toggleBand({ ...lte42 }, true)
    expect(network.selectedBands.value).toEqual([lte41, lte42])

    network.toggleBand({ ...lte41 }, false)
    expect(network.selectedBands.value).toEqual([lte42])
  })

  it('selects the exact current mode and only enables apply after a change', async () => {
    api.getModes.mockResolvedValue({
      data: {
        value: {
          supported: [
            {
              allowed: 4,
              preferred: 0,
              allowedLabel: 'LTE',
              preferredLabel: 'None',
              current: false,
            },
            {
              allowed: 100,
              preferred: 0,
              allowedLabel: 'LTE + 5G NSA + 5G SA',
              preferredLabel: 'None',
              current: true,
            },
          ],
          current: {
            allowed: 100,
            preferred: 0,
            allowedLabel: 'LTE + 5G NSA + 5G SA',
            preferredLabel: 'None',
            current: true,
          },
        },
      },
    })
    const network = useModemNetwork({ modemId })

    await vi.waitFor(() => expect(network.selectedMode.value).toBe('100:0'))
    expect(network.canUpdateMode.value).toBe(false)
    await network.handleModeUpdate()
    expect(api.setCurrentModes).not.toHaveBeenCalled()

    network.selectedMode.value = '4:0'
    expect(network.canUpdateMode.value).toBe(true)
  })

  it('represents a full band mask as individually selected bands', async () => {
    api.getBands.mockResolvedValue({
      data: {
        value: {
          supported: [
            { value: { technology: 4, number: 41 }, label: 'LTE B41', current: true },
            { value: { technology: 4, number: 42 }, label: 'LTE B42', current: true },
          ],
          current: [
            { technology: 4, number: 41 },
            { technology: 4, number: 42 },
          ],
        },
      },
    })
    const network = useModemNetwork({ modemId })

    await vi.waitFor(() => expect(network.selectedBands.value).toHaveLength(2))
    expect(network.selectedBands.value).toEqual([
      { technology: 4, number: 41 },
      { technology: 4, number: 42 },
    ])
    expect(network.canUpdateBands.value).toBe(false)
    await network.handleBandUpdate()
    expect(api.setCurrentBands).not.toHaveBeenCalled()

    network.toggleBand({ technology: 4, number: 42 }, false)
    expect(network.canUpdateBands.value).toBe(true)
  })
})

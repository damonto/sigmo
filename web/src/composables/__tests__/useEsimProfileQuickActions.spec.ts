import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useEsimProfileQuickActions } from '@/composables/useEsimProfileQuickActions'
import type { EsimProfile } from '@/types/esim'

type TestRef<T> = {
  value: T
}

const harness = vi.hoisted(() => ({
  handleInternetConnect: vi.fn(),
  handleInternetDisconnect: vi.fn(),
  handleWiFiCallingUpdate: vi.fn(),
  reconnectWiFiCalling: vi.fn(),
  disconnectWiFiCalling: vi.fn(),
  resetMsisdnInput: vi.fn(),
  handleMsisdnUpdate: vi.fn(),
  internetConnectionEnabled: undefined as TestRef<boolean> | undefined,
  wifiCallingState: undefined as TestRef<string> | undefined,
  wifiCallingSettingsEnabled: undefined as TestRef<boolean> | undefined,
}))

vi.mock('@/composables/useModemInternet', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemInternet: (options: { enabled: TestRef<boolean> }) => {
      harness.internetConnectionEnabled = options.enabled
      return {
        isInternetLoading: ref(false),
        isInternetConnecting: ref(false),
        isInternetDisconnecting: ref(false),
        isInternetConnected: ref(false),
        handleInternetConnect: harness.handleInternetConnect,
        handleInternetDisconnect: harness.handleInternetDisconnect,
      }
    },
  }
})

vi.mock('@/composables/useModemWiFiCallingSettings', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemWiFiCallingSettings: (options: { enabled: TestRef<boolean> }) => {
      harness.wifiCallingState = ref('')
      harness.wifiCallingSettingsEnabled = options.enabled
      return {
        settingsWiFiCallingEnabled: ref(false),
        settingsWiFiCallingConnected: ref(false),
        settingsWiFiCallingState: harness.wifiCallingState,
        isWiFiCallingSettingsLoading: ref(false),
        isWiFiCallingSettingsUpdating: ref(false),
        isWiFiCallingReconnecting: ref(false),
        isWiFiCallingDisconnecting: ref(false),
        handleWiFiCallingUpdate: harness.handleWiFiCallingUpdate,
        reconnectWiFiCalling: harness.reconnectWiFiCalling,
        disconnectWiFiCalling: harness.disconnectWiFiCalling,
      }
    },
  }
})

vi.mock('@/composables/useModemMsisdn', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemMsisdn: () => ({
      msisdnInput: ref(''),
      isMsisdnUpdating: ref(false),
      isMsisdnValid: ref(true),
      resetMsisdnInput: harness.resetMsisdnInput,
      handleMsisdnUpdate: harness.handleMsisdnUpdate,
    }),
  }
})

const profile: EsimProfile = {
  id: 'profile-1',
  seId: 'default',
  seLabel: 'eUICC',
  name: 'Line',
  iccid: 'iccid-1',
  enabled: true,
  serviceProviderName: 'Carrier',
  profileName: 'Line',
  profileStateName: 'enabled',
  profileClass: 'operational',
  profileOwner: { mcc: '208', mnc: '09' },
  regionCode: 'US',
}

const useActions = (
  loadWiFiCallingSettings = computed(() => true),
  loadInternetConnection = computed(() => true),
) =>
  useEsimProfileQuickActions({
    modemId: computed(() => 'modem-1'),
    modem: ref(null),
    canUseWiFiCalling: computed(() => true),
    loadInternetConnection,
    loadWiFiCallingSettings,
    refreshModem: vi.fn(),
  })

describe('useEsimProfileQuickActions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    harness.handleInternetConnect.mockResolvedValue(undefined)
    harness.handleInternetDisconnect.mockResolvedValue(undefined)
    harness.handleWiFiCallingUpdate.mockResolvedValue(undefined)
    harness.reconnectWiFiCalling.mockResolvedValue(undefined)
    harness.disconnectWiFiCalling.mockResolvedValue(undefined)
    harness.handleMsisdnUpdate.mockResolvedValue(true)
  })

  it('connects and disconnects internet from the profile shortcut', async () => {
    const actions = useActions()

    await actions.handleNetworkQuickToggle(profile, true)
    await actions.handleNetworkQuickToggle(profile, false)

    expect(harness.handleInternetConnect).toHaveBeenCalledTimes(1)
    expect(harness.handleInternetDisconnect).toHaveBeenCalledTimes(1)
  })

  it('keeps internet connection state enabled while a network quick action is running', async () => {
    let finishConnect: () => void = () => {}
    harness.handleInternetConnect.mockReturnValue(
      new Promise<void>((resolve) => {
        finishConnect = resolve
      }),
    )
    const actions = useActions(
      computed(() => true),
      computed(() => false),
    )

    expect(harness.internetConnectionEnabled?.value).toBe(false)

    const pending = actions.handleNetworkQuickToggle(profile, true)
    await Promise.resolve()

    expect(harness.internetConnectionEnabled?.value).toBe(true)

    finishConnect()
    await pending

    expect(harness.internetConnectionEnabled?.value).toBe(false)
  })

  it('enables settings before reconnecting Wi-Fi Calling', async () => {
    const actions = useActions()

    await actions.handleWiFiCallingQuickToggle(profile, true)

    expect(actions.settingsWiFiCallingEnabled.value).toBe(true)
    expect(harness.handleWiFiCallingUpdate).toHaveBeenCalledTimes(1)
    expect(harness.reconnectWiFiCalling).toHaveBeenCalledTimes(1)
  })

  it('disconnects Wi-Fi Calling without saving settings', async () => {
    const actions = useActions()

    await actions.handleWiFiCallingQuickToggle(profile, false)

    expect(harness.disconnectWiFiCalling).toHaveBeenCalledTimes(1)
    expect(harness.handleWiFiCallingUpdate).not.toHaveBeenCalled()
  })

  it('keeps Wi-Fi Calling busy after the connect request resolves while the backend is connecting', async () => {
    harness.reconnectWiFiCalling.mockImplementation(async () => {
      harness.wifiCallingState!.value = 'connecting'
    })
    const actions = useActions()

    await actions.handleWiFiCallingQuickToggle(profile, true)

    expect(actions.isWiFiCallingBusy.value).toBe(true)
  })

  it('keeps Wi-Fi Calling settings enabled while a quick action is running', async () => {
    let finishReconnect: () => void = () => {}
    harness.reconnectWiFiCalling.mockReturnValue(
      new Promise<void>((resolve) => {
        finishReconnect = resolve
      }),
    )
    const actions = useActions(computed(() => false))

    expect(harness.wifiCallingSettingsEnabled?.value).toBe(false)

    const pending = actions.handleWiFiCallingQuickToggle(profile, true)
    await Promise.resolve()

    expect(harness.wifiCallingSettingsEnabled?.value).toBe(true)

    finishReconnect()
    await pending

    expect(harness.wifiCallingSettingsEnabled?.value).toBe(false)
  })

  it('opens and closes the phone number dialog after a successful save', async () => {
    const actions = useActions()

    actions.openMsisdnDialog()
    expect(harness.resetMsisdnInput).toHaveBeenCalledTimes(1)
    expect(actions.msisdnDialogOpen.value).toBe(true)

    await actions.saveMsisdn()

    expect(harness.handleMsisdnUpdate).toHaveBeenCalledTimes(1)
    expect(actions.msisdnDialogOpen.value).toBe(false)
  })

  it('keeps the phone number dialog open when saving fails', async () => {
    harness.handleMsisdnUpdate.mockResolvedValue(false)
    const actions = useActions()

    actions.openMsisdnDialog()
    await actions.saveMsisdn()

    expect(actions.msisdnDialogOpen.value).toBe(true)
  })
})

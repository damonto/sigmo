import { flushPromises, mount } from '@vue/test-utils'
import type { ComputedRef, Ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Modem } from '@/types/modem'
import ModemDetailView from '@/views/ModemDetailView.vue'

const api = vi.hoisted(() => ({
  unlockSim: vi.fn(),
}))

const routeHarness = vi.hoisted(() => ({
  route: undefined as { params: { id: string } } | undefined,
}))

const detailHarness = vi.hoisted(() => ({
  modem: undefined as Ref<Modem | null> | undefined,
  fetchModemDetail: vi.fn(),
  fetchEsimProfiles: vi.fn(),
  resetMsisdnInput: vi.fn(),
  handleMsisdnUpdate: vi.fn(),
  handleInternetConnect: vi.fn(),
  handleInternetDisconnect: vi.fn(),
  handleWiFiCallingUpdate: vi.fn(),
  reconnectWiFiCalling: vi.fn(),
  disconnectWiFiCalling: vi.fn(),
  wifiCallingFeature: false,
  internetConnectionEnabled: undefined as ComputedRef<boolean> | undefined,
  wifiCallingSettingsEnabled: undefined as ComputedRef<boolean> | undefined,
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

vi.mock('vue-router', async () => {
  const { reactive } = await vi.importActual<typeof import('vue')>('vue')
  routeHarness.route = reactive({ params: { id: 'modem-1' } })
  return {
    createRouter: () => ({
      beforeEach: vi.fn(),
      currentRoute: {
        value: {
          name: 'modem-detail',
        },
      },
      replace: vi.fn(),
    }),
    createWebHistory: () => ({}),
    useRoute: () => routeHarness.route,
  }
})

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('vue-sonner', () => ({
  toast: {
    success: vi.fn(),
  },
}))

vi.mock('@/composables/useCapabilities', () => ({
  FEATURE: {
    esimTransfer: 'esim_transfer',
    wifiCalling: 'wifi_calling',
  },
  useCapabilities: () => ({
    hasFeature: (feature: string) =>
      feature === 'wifi_calling' ? detailHarness.wifiCallingFeature : false,
    fetchCapabilities: vi.fn(),
  }),
}))

vi.mock('@/composables/useModemDetail', async () => {
  const { computed, ref } = await vi.importActual<typeof import('vue')>('vue')
  detailHarness.modem = ref(null)
  return {
    useModemDetail: () => ({
      modem: detailHarness.modem,
      euicc: ref(null),
      esimProfiles: ref([]),
      isLoading: ref(false),
      isEsimProfilesLoading: ref(false),
      isPhysicalModem: computed(() => Boolean(detailHarness.modem?.value?.supportsEsim === false)),
      isEsimModem: computed(() => Boolean(detailHarness.modem?.value?.supportsEsim)),
      fetchModemDetail: detailHarness.fetchModemDetail,
      fetchEsimProfiles: detailHarness.fetchEsimProfiles,
    }),
  }
})

vi.mock('@/composables/useSimSlotSwitch', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useSimSlotSwitch: () => ({
      currentSimIdentifier: ref(''),
      simSlots: ref([]),
      handleSimSwitch: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useEsimDiscover', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useEsimDiscover: () => ({
      discoverDialogOpen: ref(false),
      discoverOptions: ref([]),
      selectedDiscoverAddress: ref(''),
      isDiscoverLoading: ref(false),
      hasDiscoverOptions: ref(false),
      hasDiscoverSelection: ref(false),
      openDiscoverDialog: vi.fn(),
      confirmDiscoverSelection: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useEsimDownload', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useEsimDownload: () => ({
      downloadState: ref('idle'),
      downloadStage: ref('initializing'),
      progress: ref(0),
      errorType: ref('none'),
      errorMessage: ref(''),
      previewProfile: ref(null),
      downloadedName: ref(''),
      startDownload: vi.fn(),
      confirmPreview: vi.fn(),
      submitConfirmationCode: vi.fn(),
      cancelDownload: vi.fn(),
      closeDialog: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useModemInternet', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemInternet: (options: { enabled: ComputedRef<boolean> }) => {
      detailHarness.internetConnectionEnabled = options.enabled
      return {
        isInternetLoading: ref(false),
        isInternetConnecting: ref(false),
        isInternetDisconnecting: ref(false),
        isInternetConnected: ref(false),
        handleInternetConnect: detailHarness.handleInternetConnect,
        handleInternetDisconnect: detailHarness.handleInternetDisconnect,
      }
    },
  }
})

vi.mock('@/composables/useModemWiFiCallingSettings', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemWiFiCallingSettings: (options: { enabled: ComputedRef<boolean> }) => {
      detailHarness.wifiCallingSettingsEnabled = options.enabled
      return {
        settingsWiFiCallingEnabled: ref(false),
        settingsWiFiCallingPreferred: ref(false),
        settingsWiFiCallingConnected: ref(false),
        settingsWiFiCallingState: ref(''),
        isWiFiCallingSettingsLoading: ref(false),
        isWiFiCallingSettingsUpdating: ref(false),
        isWiFiCallingReconnecting: ref(false),
        isWiFiCallingDisconnecting: ref(false),
        handleWiFiCallingUpdate: detailHarness.handleWiFiCallingUpdate,
        reconnectWiFiCalling: detailHarness.reconnectWiFiCalling,
        disconnectWiFiCalling: detailHarness.disconnectWiFiCalling,
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
      resetMsisdnInput: detailHarness.resetMsisdnInput,
      handleMsisdnUpdate: detailHarness.handleMsisdnUpdate,
    }),
  }
})

const lockedModem = (supportsEsim = false): Modem => ({
  manufacturer: 'Quectel',
  id: 'modem-1',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name: 'RM520N',
  number: '',
  state: 'locked',
  unlockRequired: 'sim-pin',
  unlockSupported: true,
  sim: {
    active: false,
    operatorName: '',
    operatorIdentifier: '',
    regionCode: '',
    identifier: '',
  },
  slots: [],
  accessTechnology: null,
  registrationState: '',
  registeredOperator: {
    name: '',
    code: '',
  },
  signalQuality: 0,
  supportsEsim,
})

const mountView = () =>
  mount(ModemDetailView, {
    global: {
      stubs: {
        ModemDetailHeader: true,
        SimSlotSwitcher: true,
        ModemDetailCard: true,
        EsimSummaryCard: true,
        EsimProfileSection: {
          name: 'EsimProfileSection',
          emits: ['edit-phone-number', 'profile-actions-open-change'],
          template:
            '<section data-testid="esim-profiles"><button data-testid="edit-msisdn" type="button" @click="$emit(\'edit-phone-number\', {})">edit</button></section>',
        },
        DraggableFab: {
          template: '<button data-testid="install-esim"><slot /></button>',
        },
        EsimInstallDialog: true,
        EsimTransferDialog: true,
        EsimDiscoverDialog: true,
        EsimDownloadProgressModal: true,
        EsimDownloadPreviewModal: true,
        EsimDownloadConfirmationModal: true,
        EsimDownloadResultModal: true,
        ModemLineMsisdnDialog: {
          props: ['open'],
          emits: ['save'],
          template:
            '<form v-if="open" data-testid="msisdn-dialog" @submit.prevent="$emit(\'save\')"><button type="submit">save</button></form>',
        },
        Dialog: { template: '<div><slot /></div>' },
        DialogContent: { template: '<div><slot /></div>' },
        DialogHeader: { template: '<div><slot /></div>' },
        DialogTitle: { template: '<div><slot /></div>' },
        ScrollArea: { template: '<div><slot /></div>' },
        SimPinUnlockDialog: {
          name: 'SimPinUnlockDialog',
          props: ['open', 'pin', 'isSubmitting', 'error', 'lockType'],
          emits: ['update:open', 'update:pin', 'submit', 'cancel'],
          template:
            '<div v-if="open" data-testid="pin-dialog"><span data-testid="pin-error">{{ error }}</span><button data-testid="submit-pin" type="button" @click="$emit(\'submit\')">submit</button></div>',
        },
        Alert: {
          template: '<section data-testid="locked-alert"><slot /></section>',
        },
        AlertTitle: {
          template: '<h2><slot /></h2>',
        },
        AlertDescription: {
          template: '<div><slot /></div>',
        },
        Button: {
          emits: ['click'],
          template: '<button type="button" @click="$emit(\'click\', $event)"><slot /></button>',
        },
      },
    },
  })

describe('ModemDetailView SIM PIN unlock', () => {
  beforeEach(() => {
    if (routeHarness.route) {
      routeHarness.route.params.id = 'modem-1'
    }
    vi.clearAllMocks()
    api.unlockSim.mockResolvedValue(undefined)
    detailHarness.handleMsisdnUpdate.mockResolvedValue(true)
    detailHarness.handleInternetConnect.mockResolvedValue(undefined)
    detailHarness.handleInternetDisconnect.mockResolvedValue(undefined)
    detailHarness.handleWiFiCallingUpdate.mockResolvedValue(undefined)
    detailHarness.reconnectWiFiCalling.mockResolvedValue(undefined)
    detailHarness.disconnectWiFiCalling.mockResolvedValue(undefined)
    detailHarness.wifiCallingFeature = false
    detailHarness.internetConnectionEnabled = undefined
    detailHarness.wifiCallingSettingsEnabled = undefined
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem()
    }
  })

  it('opens the PIN dialog for locked SIM PIN modems', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
  })

  it('unlocks the SIM and refreshes modem details', async () => {
    const wrapper = mountView()
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'SimPinUnlockDialog' })
    dialog.vm.$emit('update:pin', '1234')
    await wrapper.vm.$nextTick()
    dialog.vm.$emit('submit')
    await flushPromises()

    expect(api.unlockSim).toHaveBeenCalledWith('modem-1', '1234')
    expect(detailHarness.fetchModemDetail).toHaveBeenCalledWith('modem-1')
    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(false)
  })

  it('keeps the dialog open when unlocking fails', async () => {
    api.unlockSim.mockRejectedValueOnce(new Error('bad pin'))
    const wrapper = mountView()
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'SimPinUnlockDialog' })
    dialog.vm.$emit('update:pin', '1234')
    await wrapper.vm.$nextTick()
    dialog.vm.$emit('submit')
    await flushPromises()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pin-error"]').text()).toBe('bad pin')
  })

  it('shows a retry action after dismissing the PIN dialog', async () => {
    const wrapper = mountView()
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'SimPinUnlockDialog' })
    dialog.vm.$emit('cancel')
    dialog.vm.$emit('update:open', false)
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="locked-alert"]').exists()).toBe(true)

    await wrapper.find('[data-testid="locked-alert"] button').trigger('click')

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
  })

  it('keeps eSIM profile actions available while the current profile needs PIN unlock', async () => {
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem(true)
    }

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="esim-profiles"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="install-esim"]').exists()).toBe(true)
  })

  it('updates the line number from the eSIM profile shortcut', async () => {
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem(true)
    }

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-testid="edit-msisdn"]').trigger('click')
    expect(detailHarness.resetMsisdnInput).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="msisdn-dialog"]').exists()).toBe(true)

    await wrapper.find('[data-testid="msisdn-dialog"]').trigger('submit')
    await flushPromises()

    expect(detailHarness.handleMsisdnUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="msisdn-dialog"]').exists()).toBe(false)
  })

  it('loads profile quick action state only while the active profile menu is open', async () => {
    detailHarness.wifiCallingFeature = true
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem(true)
    }

    const wrapper = mountView()
    await flushPromises()

    expect(detailHarness.internetConnectionEnabled?.value).toBe(false)
    expect(detailHarness.wifiCallingSettingsEnabled?.value).toBe(false)

    const section = wrapper.findComponent({ name: 'EsimProfileSection' })
    section.vm.$emit('profile-actions-open-change', { id: 'active', enabled: true }, true)
    await wrapper.vm.$nextTick()

    expect(detailHarness.internetConnectionEnabled?.value).toBe(true)
    expect(detailHarness.wifiCallingSettingsEnabled?.value).toBe(true)

    section.vm.$emit('profile-actions-open-change', { id: 'active', enabled: true }, false)
    await wrapper.vm.$nextTick()

    expect(detailHarness.internetConnectionEnabled?.value).toBe(false)
    expect(detailHarness.wifiCallingSettingsEnabled?.value).toBe(false)
  })

  it('stops profile quick action state loading when the modem changes', async () => {
    detailHarness.wifiCallingFeature = true
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem(true)
    }

    const wrapper = mountView()
    await flushPromises()

    const section = wrapper.findComponent({ name: 'EsimProfileSection' })
    section.vm.$emit('profile-actions-open-change', { id: 'active', enabled: true }, true)
    await wrapper.vm.$nextTick()

    expect(detailHarness.internetConnectionEnabled?.value).toBe(true)
    expect(detailHarness.wifiCallingSettingsEnabled?.value).toBe(true)

    routeHarness.route!.params.id = 'modem-2'
    await wrapper.vm.$nextTick()

    expect(detailHarness.internetConnectionEnabled?.value).toBe(false)
    expect(detailHarness.wifiCallingSettingsEnabled?.value).toBe(false)
  })

  it('keeps the line number dialog open when updating fails', async () => {
    detailHarness.handleMsisdnUpdate.mockResolvedValue(false)
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem(true)
    }

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-testid="edit-msisdn"]').trigger('click')
    await wrapper.find('[data-testid="msisdn-dialog"]').trigger('submit')
    await flushPromises()

    expect(detailHarness.handleMsisdnUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="msisdn-dialog"]').exists()).toBe(true)
  })
})

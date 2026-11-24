import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { ComputedRef, Ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ModemNetworkSummary from '@/components/modem/ModemNetworkSummary.vue'
import type { InternetTrafficResponse } from '@/types/internet'
import type { Modem } from '@/types/modem'
import type { SEsResponse } from '@/types/se'
import ModemDetailView from '@/views/ModemDetailView.vue'

const api = vi.hoisted(() => ({
  unlockSim: vi.fn(),
  switchSimSlot: vi.fn(),
  getTraffic: vi.fn<(id: string, signal: AbortSignal) => Promise<InternetTrafficResponse>>(),
}))

const reminderApi = vi.hoisted(() => ({
  savePhysicalReminder: vi.fn(),
  deletePhysicalReminder: vi.fn(),
}))

const routeHarness = vi.hoisted(() => ({
  route: undefined as { params: { id: string } } | undefined,
}))

const detailHarness = vi.hoisted(() => ({
  modem: undefined as Ref<Modem | null> | undefined,
  seInfo: undefined as Ref<SEsResponse | null> | undefined,
  isSELoading: undefined as Ref<boolean> | undefined,
  refreshModemList: vi.fn(),
  fetchModemDetail: vi.fn(),
  fetchEsimProfiles: vi.fn(),
  resetMsisdnInput: vi.fn(),
  handleMsisdnUpdate: vi.fn(),
  handleInternetConnect: vi.fn(),
  handleInternetDisconnect: vi.fn(),
  ensureWiFiCallingEnabled: vi.fn(),
  reconnectWiFiCalling: vi.fn(),
  disconnectWiFiCalling: vi.fn(),
  wifiCallingFeature: false,
  internetConnectionEnabled: undefined as ComputedRef<boolean> | undefined,
  wifiCallingSettingsEnabled: undefined as ComputedRef<boolean> | undefined,
}))

const simAppHarness = vi.hoisted(() => ({
  available: undefined as Ref<boolean> | undefined,
  profileIccid: undefined as Ref<string> | undefined,
  currentView: undefined as Ref<unknown> | undefined,
  dialogOpen: undefined as Ref<boolean> | undefined,
  errorMessage: undefined as Ref<string> | undefined,
  openRootMenu: vi.fn(),
  selectMenuItem: vi.fn(),
  submitInput: vi.fn(),
  submitInkey: vi.fn(),
  respondConfirm: vi.fn(),
  back: vi.fn(),
}))

const toastHarness = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}))

vi.mock('@/apis/simPin', () => ({
  useSimPinApi: () => ({ status: vi.fn().mockRejectedValue({ status: 404 }), submit: vi.fn() }),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

vi.mock('@/apis/internet', () => ({ useInternetApi: () => ({ getTraffic: api.getTraffic }) }))

vi.mock('@/composables/useModems', () => ({
  refreshModemList: detailHarness.refreshModemList,
}))

vi.mock('@/apis/reminder', () => ({
  useReminderApi: () => reminderApi,
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
    locale: { value: 'en' },
    t: (key: string) => key,
  }),
}))

vi.mock('vue-sonner', () => ({
  toast: toastHarness,
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
  detailHarness.seInfo = ref(null)
  detailHarness.isSELoading = ref(false)
  return {
    useModemDetail: () => ({
      modem: detailHarness.modem,
      seInfo: detailHarness.seInfo,
      esimProfiles: ref([]),
      isLoading: ref(false),
      isSELoading: detailHarness.isSELoading,
      isEsimProfilesLoading: ref(false),
      isSIMKindUnknown: computed(() => detailHarness.modem?.value?.simKind === 'unknown'),
      isPhysicalModem: computed(() => detailHarness.modem?.value?.simKind === 'physical'),
      isEsimModem: computed(() => detailHarness.modem?.value?.simKind === 'euicc'),
      fetchModemDetail: detailHarness.fetchModemDetail,
      fetchSEs: vi.fn(),
      fetchEsimProfiles: detailHarness.fetchEsimProfiles,
    }),
  }
})

vi.mock('@/composables/useSimApplicationSession', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  simAppHarness.available = ref(false)
  simAppHarness.profileIccid = ref('')
  simAppHarness.currentView = ref(null)
  simAppHarness.dialogOpen = ref(false)
  simAppHarness.errorMessage = ref('')
  return {
    useSimApplicationSession: () => ({
      available: simAppHarness.available,
      profileIccid: simAppHarness.profileIccid,
      currentView: simAppHarness.currentView,
      dialogOpen: simAppHarness.dialogOpen,
      errorMessage: simAppHarness.errorMessage,
      openRootMenu: simAppHarness.openRootMenu,
      selectMenuItem: simAppHarness.selectMenuItem,
      submitInput: simAppHarness.submitInput,
      submitInkey: simAppHarness.submitInkey,
      respondConfirm: simAppHarness.respondConfirm,
      back: simAppHarness.back,
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
        settingsWiFiCallingConnected: ref(false),
        settingsWiFiCallingState: ref(''),
        isWiFiCallingSettingsLoading: ref(false),
        isWiFiCallingSettingsUpdating: ref(false),
        isWiFiCallingReconnecting: ref(false),
        isWiFiCallingDisconnecting: ref(false),
        ensureWiFiCallingEnabled: detailHarness.ensureWiFiCallingEnabled,
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

const lockedModem = (simKind: Modem['simKind'] = 'physical'): Modem => ({
  manufacturer: 'Quectel',
  id: 'modem-1',
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name: 'RM520N',
  number: '',
  state: 'locked',
  unlockRequired: 'sim-pin',
  unlockSupported: true,
  sim: {
    slot: 0,
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
  airplaneMode: false,
  simKind,
})

const physicalModem = (): Modem => ({
  ...lockedModem(),
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '46000',
    regionCode: 'CN',
    identifier: '8985200012345678901',
    reminder: {
      nextAt: new Date(2026, 6, 18, 10, 30, 0, 0).toISOString(),
      repeatDays: 7,
      content: 'Renew the plan',
    },
  },
})

const mountView = () =>
  mount(ModemDetailView, {
    global: {
      stubs: {
        ModemDetailHeader: true,
        SimSlotSwitcher: {
          props: ['slots', 'onSwitch'],
          template:
            '<button v-if="slots.length" data-testid="switch-slot" @click="onSwitch(slots[0])">switch slot</button>',
        },
        ModemDetailCard: true,
        ReminderDialog: {
          props: ['open'],
          template: '<section v-if="open" data-testid="reminder-dialog"></section>',
        },
        EsimSummaryCard: true,
        EsimProfileSection: {
          name: 'EsimProfileSection',
          props: ['refreshModem'],
          emits: ['edit-phone-number', 'profile-actions-open-change'],
          template:
            '<section data-testid="esim-profiles"><slot name="actions" /><button data-testid="edit-msisdn" type="button" @click="$emit(\'edit-phone-number\', {})">edit</button><button data-testid="refresh-after-profile-switch" type="button" @click="refreshModem()">switch profile</button></section>',
        },
        DraggableFab: {
          props: ['disabled'],
          emits: ['click'],
          template:
            '<button data-testid="install-esim" :disabled="disabled" @click="!disabled && $emit(\'click\')"><slot /></button>',
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
        SimApplicationDialog: {
          props: ['open', 'view'],
          emits: [
            'update:open',
            'select-menu-item',
            'submit-input',
            'submit-inkey',
            'respond-confirm',
            'back',
          ],
          template: '<section v-if="open" data-testid="sim-application-dialog"></section>',
        },
        Dialog: { template: '<div><slot /></div>' },
        DialogContent: { template: '<div><slot /></div>' },
        DialogDescription: { template: '<p><slot /></p>' },
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

enableAutoUnmount(afterEach)

describe('ModemDetailView', () => {
  beforeEach(() => {
    if (routeHarness.route) {
      routeHarness.route.params.id = 'modem-1'
    }
    vi.clearAllMocks()
    api.unlockSim.mockResolvedValue(undefined)
    api.switchSimSlot.mockResolvedValue(undefined)
    api.getTraffic.mockReset().mockResolvedValue({ connected: false, sample: null })
    detailHarness.refreshModemList.mockResolvedValue(undefined)
    detailHarness.handleMsisdnUpdate.mockResolvedValue(true)
    detailHarness.handleInternetConnect.mockResolvedValue(undefined)
    detailHarness.handleInternetDisconnect.mockResolvedValue(undefined)
    detailHarness.ensureWiFiCallingEnabled.mockResolvedValue(true)
    detailHarness.reconnectWiFiCalling.mockResolvedValue(undefined)
    detailHarness.disconnectWiFiCalling.mockResolvedValue(undefined)
    detailHarness.wifiCallingFeature = false
    detailHarness.internetConnectionEnabled = undefined
    detailHarness.wifiCallingSettingsEnabled = undefined
    simAppHarness.openRootMenu.mockClear()
    simAppHarness.selectMenuItem.mockClear()
    simAppHarness.submitInput.mockClear()
    simAppHarness.submitInkey.mockClear()
    simAppHarness.respondConfirm.mockClear()
    simAppHarness.back.mockClear()
    if (simAppHarness.available) {
      simAppHarness.available.value = false
    }
    if (simAppHarness.profileIccid) {
      simAppHarness.profileIccid.value = ''
    }
    if (simAppHarness.currentView) {
      simAppHarness.currentView.value = null
    }
    if (simAppHarness.dialogOpen) {
      simAppHarness.dialogOpen.value = false
    }
    if (simAppHarness.errorMessage) {
      simAppHarness.errorMessage.value = ''
    }
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem()
    }
    if (detailHarness.seInfo) {
      detailHarness.seInfo.value = null
    }
    if (detailHarness.isSELoading) {
      detailHarness.isSELoading.value = false
    }
  })

  it('shows the SIM operator separately from the registered network while roaming', () => {
    const modem = detailHarness.modem
    if (!modem) throw new Error('missing modem detail')
    modem.value = {
      ...physicalModem(),
      registrationState: 'Roaming',
      registeredOperator: { name: 'Visited Carrier', code: '46001' },
      signalQuality: 67,
    }

    const summary = mountView().getComponent(ModemNetworkSummary)
    const [simOperator, registeredNetwork, registration] = summary.get('dl').findAll('dd')
    expect(simOperator?.findAll('span').map((part) => part.text())).toEqual(['Carrier', '46000'])
    expect(registeredNetwork?.findAll('span').map((part) => part.text())).toEqual([
      'Visited Carrier',
      '46001',
    ])
    expect(registration?.text()).toBe('labels.roaming')
    expect(summary.text()).toContain('67%')
  })

  it('shows live traffic, hides it on disconnect, and starts fresh on reconnect', async () => {
    const modem = detailHarness.modem
    if (!modem) throw new Error('missing modem detail')
    modem.value = physicalModem()
    const response = (
      elapsedMilliseconds: number,
      rxBytes: number,
      txBytes: number,
      sessionId = 'session-1',
    ): InternetTrafficResponse => ({
      connected: true,
      sample: { sessionId, elapsedMilliseconds, rxBytes, txBytes },
    })
    api.getTraffic
      .mockResolvedValueOnce(response(1000, 2000, 1000))
      .mockResolvedValueOnce(response(3000, 1_002_000, 501_000))
      .mockResolvedValueOnce(response(4000, 1_002_000, 501_000))
      .mockResolvedValueOnce({ connected: false, sample: null })
      .mockResolvedValueOnce(response(1500, 32, 8, 'session-2'))

    vi.useFakeTimers()
    try {
      const summary = mountView().getComponent(ModemNetworkSummary)
      const traffic = '[aria-label="modemDetail.overview.traffic.title"]'
      await vi.advanceTimersByTimeAsync(0)
      expect(summary.get(traffic).text()).toContain('3 kB')

      await vi.advanceTimersByTimeAsync(1000)
      expect(summary.get(traffic).text()).toContain('4 Mb/s')
      expect(summary.get(traffic).text()).toContain('2 Mb/s')
      expect(summary.get(traffic).text()).toContain('1.5 MB')

      await vi.advanceTimersByTimeAsync(1000)
      expect(summary.get(traffic).text()).toContain('modemDetail.overview.traffic.idle')
      expect(summary.get(traffic).text()).toContain('1.5 MB')

      await vi.advanceTimersByTimeAsync(1000)
      expect(summary.find(traffic).exists()).toBe(false)
      expect(summary.text()).toContain('modemDetail.overview.network')

      await vi.advanceTimersByTimeAsync(5000)
      expect(summary.get(traffic).text()).toContain('40 B')
      expect(summary.get(traffic).text()).toContain('—')
    } finally {
      vi.useRealTimers()
    }
  })

  it('stops traffic polling when the page is hidden', async () => {
    if (!detailHarness.modem) throw new Error('missing modem detail')
    detailHarness.modem.value = physicalModem()
    api.getTraffic.mockImplementationOnce(
      (_, signal) =>
        new Promise((_, reject) => {
          signal.addEventListener('abort', () => reject(signal.reason), { once: true })
        }),
    )
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    vi.useFakeTimers()
    try {
      mountView()
      await vi.advanceTimersByTimeAsync(0)
      expect(api.getTraffic).toHaveBeenCalledTimes(1)
      const signal = api.getTraffic.mock.calls[0]?.[1]

      visibility.mockReturnValue('hidden')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(10_000)
      expect(signal?.aborted).toBe(true)
      expect(api.getTraffic).toHaveBeenCalledTimes(1)

      visibility.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)
      expect(api.getTraffic).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
      visibility.mockRestore()
    }
  })

  it.each([
    ['Searching', false, 'modemDetail.overview.registration.searching'],
    ['Denied', false, 'modemDetail.overview.registration.denied'],
    ['Home', true, 'modemDetail.settings.networkAirplaneModeStatus'],
  ])(
    'clears the displayed serving network for %s with airplane mode %s',
    (state, airplaneMode, label) => {
      const modem = detailHarness.modem
      if (!modem) throw new Error('missing modem detail')
      modem.value = {
        ...physicalModem(),
        registrationState: state,
        registeredOperator: { name: 'Previous network', code: '46001' },
        airplaneMode,
        wifiCallingConnected: true,
      }

      const summary = mountView().getComponent(ModemNetworkSummary)
      const [simOperator, registeredNetwork, registration] = summary.get('dl').findAll('dd')
      expect(simOperator?.findAll('span').map((part) => part.text())).toEqual(['Carrier', '46000'])
      expect(registeredNetwork?.text()).toBe('—')
      expect(registration?.text()).toBe(label)
      expect(summary.text()).toContain('modemDetail.calls.routes.wifiCalling')
    },
  )

  it('opens the PIN dialog for locked SIM PIN modems', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
  })

  it.each([
    { simKind: 'physical', action: 'switch-slot' },
    { simKind: 'euicc', action: 'refresh-after-profile-switch' },
  ] as const)(
    'refreshes the list and detail after a $simKind switch',
    async ({ simKind, action }) => {
      const device = physicalModem()
      const modem = detailHarness.modem
      if (!modem) throw new Error('missing modem detail')
      modem.value = { ...device, simKind, slots: [{ ...device.sim, slot: 2, active: false }] }
      const wrapper = mountView()
      await flushPromises()
      expect(detailHarness.refreshModemList).not.toHaveBeenCalled()
      detailHarness.fetchModemDetail.mockClear()

      await wrapper.get(`[data-testid="${action}"]`).trigger('click')
      await flushPromises()

      expect(detailHarness.refreshModemList).toHaveBeenCalledOnce()
      expect(detailHarness.fetchModemDetail).toHaveBeenCalledExactlyOnceWith(device.id)
    },
  )

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
      detailHarness.modem.value = lockedModem('euicc')
    }

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="pin-dialog"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="esim-profiles"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="install-esim"]').exists()).toBe(true)
  })

  it('disables eSIM install until SE info is loaded', async () => {
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem('euicc')
    }
    if (detailHarness.isSELoading) {
      detailHarness.isSELoading.value = true
    }

    const wrapper = mountView()
    await flushPromises()

    const desktopInstall = wrapper.get('[data-testid="esim-profiles"] button')
    expect(wrapper.find('[data-testid="install-esim"]').attributes('disabled')).toBeDefined()
    expect(desktopInstall.text()).toBe('modemDetail.esim.installButton')
    expect(desktopInstall.attributes('disabled')).toBeDefined()

    if (detailHarness.isSELoading) {
      detailHarness.isSELoading.value = false
    }
    if (detailHarness.seInfo) {
      detailHarness.seInfo.value = {
        ses: [{ id: 'default', label: 'eUICC', eid: 'eid-1' }],
      }
    }
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="install-esim"]').attributes('disabled')).toBeUndefined()
    expect(desktopInstall.attributes('disabled')).toBeUndefined()
    await desktopInstall.trigger('click')
    expect(wrapper.findComponent({ name: 'EsimInstallDialog' }).props('open')).toBe(true)
  })

  it('shows SIM Application for physical SIM only when available', async () => {
    if (simAppHarness.available) {
      simAppHarness.available.value = true
    }

    const wrapper = mountView()
    await flushPromises()

    const action = wrapper
      .findAll('button')
      .find((button) => button.text().includes('simApplication.title'))
    expect(action?.exists()).toBe(true)

    await action!.trigger('click')

    expect(simAppHarness.openRootMenu).toHaveBeenCalledTimes(1)
  })

  it('shows Reminder and STK actions above the physical SIM details', async () => {
    if (detailHarness.modem) {
      detailHarness.modem.value = physicalModem()
    }
    if (simAppHarness.available) {
      simAppHarness.available.value = true
    }

    const wrapper = mountView()
    await flushPromises()

    const actions = wrapper.findAll('button').map((button) => button.text())
    expect(actions).toContain('modemDetail.reminder.title')
    expect(actions).toContain('modemDetail.simApplication.title')
    expect(wrapper.find('[aria-label="simPin.simActions"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Renew the plan')
    expect(wrapper.text()).toContain('modemDetail.reminder.everyDays')

    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('Renew the plan'))
      ?.trigger('click')

    expect(wrapper.find('[data-testid="reminder-dialog"]').exists()).toBe(true)
  })

  it('shows SIM Application session errors', async () => {
    const wrapper = mountView()
    await flushPromises()

    if (simAppHarness.errorMessage) {
      simAppHarness.errorMessage.value = 'open USIM card: reader busy'
    }
    await wrapper.vm.$nextTick()

    expect(toastHarness.error).toHaveBeenCalledWith('modemDetail.simApplication.errorTitle', {
      description: 'open USIM card: reader busy',
    })
  })

  it('updates the line number from the eSIM profile shortcut', async () => {
    if (detailHarness.modem) {
      detailHarness.modem.value = lockedModem('euicc')
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
      detailHarness.modem.value = lockedModem('euicc')
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
      detailHarness.modem.value = lockedModem('euicc')
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
      detailHarness.modem.value = lockedModem('euicc')
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

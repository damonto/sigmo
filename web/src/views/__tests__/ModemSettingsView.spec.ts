import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en'
import ModemSettingsView from '@/views/ModemSettingsView.vue'
import type { Modem } from '@/types/modem'

const harness = vi.hoisted(() => ({
  canUseWiFiCalling: false,
  fetchCapabilities: vi.fn(),
  fetchModem: vi.fn(),
  handleMsisdnUpdate: vi.fn(),
}))

const modem: Modem = {
  manufacturer: 'Quectel',
  id: '869710031623444',
  firmwareRevision: '1.0',
  hardwareRevision: 'A',
  name: 'RM520N',
  number: '+12242255559',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    active: true,
    operatorName: 'T-Mobile',
    operatorIdentifier: '310260',
    regionCode: 'US',
    identifier: '8901',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: {
    name: 'T-Mobile',
    code: '310260',
  },
  signalQuality: 76,
  supportsEsim: false,
}

vi.mock('vue-router', () => ({
  useRoute: () => ({
    params: { id: 'modem-1' },
  }),
  RouterLink: {
    props: ['to'],
    template: '<a><slot /></a>',
  },
}))

vi.mock('@/composables/useCapabilities', () => ({
  FEATURE: {
    wifiCalling: 'wifi_calling',
  },
  useCapabilities: () => ({
    hasFeature: () => harness.canUseWiFiCalling,
    fetchCapabilities: harness.fetchCapabilities,
  }),
}))

vi.mock('@/composables/useFeedbackBanner', () => ({
  useFeedbackBanner: () => ({
    showFeedback: vi.fn(),
  }),
}))

vi.mock('@/composables/useModemOverview', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemOverview: () => ({
      modem: ref(modem),
      isModemLoading: false,
      currentOperatorLabel: 'T-Mobile (310260)',
      currentAccessTechnology: 'LTE',
      fetchModem: harness.fetchModem,
    }),
  }
})

vi.mock('@/composables/useModemMsisdn', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useModemMsisdn: () => ({
      msisdnInput: ref(modem.number),
      isMsisdnUpdating: false,
      isMsisdnValid: true,
      handleMsisdnUpdate: harness.handleMsisdnUpdate,
    }),
  }
})

const stubs = {
  Button: {
    props: ['disabled', 'type'],
    template: '<button :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Card: {
    template: '<section><slot /></section>',
  },
  CardContent: {
    template: '<div><slot /></div>',
  },
  Dialog: {
    props: ['open'],
    emits: ['update:open'],
    template: '<div v-if="open"><slot /></div>',
  },
  DialogContent: {
    template: '<div><slot /></div>',
  },
  DialogDescription: {
    template: '<p><slot /></p>',
  },
  DialogFooter: {
    template: '<footer><slot /></footer>',
  },
  DialogHeader: {
    template: '<header><slot /></header>',
  },
  DialogTitle: {
    template: '<h2><slot /></h2>',
  },
  Input: {
    props: ['disabled', 'id', 'modelValue', 'type'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" :type="type || \'text\'" :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  Label: {
    props: ['for'],
    template: '<label :for="$props.for"><slot /></label>',
  },
  ModemStickyTopBar: {
    template: '<div />',
  },
}

const mountView = () => {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en },
  })

  return mount(ModemSettingsView, {
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('ModemSettingsView', () => {
  beforeEach(() => {
    harness.canUseWiFiCalling = false
    modem.number = '+12242255559'
    modem.wifiCallingConnected = false
    vi.clearAllMocks()
    harness.handleMsisdnUpdate.mockResolvedValue(true)
  })

  it('renders the formatted line number and network summary', () => {
    const wrapper = mountView()

    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('T-Mobile')
    expect(wrapper.text()).not.toContain('310260')
    expect(wrapper.text()).toContain('LTE')
    expect(wrapper.text()).not.toContain('76%')
    expect(wrapper.find('[data-testid="line-signal-icon"]').exists()).toBe(true)
  })

  it('renders a Wi-Fi Calling indicator when the current line is connected over Wi-Fi Calling', () => {
    modem.wifiCallingConnected = true

    const wrapper = mountView()

    expect(wrapper.find('[aria-label="Wi-Fi Calling"]').exists()).toBe(true)
  })

  it('renders the localized empty number label when the line has no number', () => {
    modem.number = ''

    const wrapper = mountView()

    expect(wrapper.text()).toContain('No Number')
  })

  it('hides the Wi-Fi Calling category when Wi-Fi Calling is unavailable', () => {
    const wrapper = mountView()

    expect(wrapper.text()).not.toContain('Wi-Fi Calling.')
    expect(wrapper.text()).toContain('Network')
    expect(wrapper.text()).toContain('Internet')
    expect(wrapper.text()).toContain('Device Settings')
    expect(wrapper.text()).not.toContain('Advanced')
  })

  it('shows the Wi-Fi Calling category when Wi-Fi Calling is available', () => {
    harness.canUseWiFiCalling = true

    const wrapper = mountView()

    expect(wrapper.text()).toContain('Wi-Fi Calling')
  })

  it('updates the line number from the edit dialog', async () => {
    const wrapper = mountView()

    await wrapper.find('button[aria-label="Edit number"]').trigger('click')
    await wrapper.find('#modem-line-msisdn').setValue('+12242255558')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(harness.handleMsisdnUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#modem-line-msisdn').exists()).toBe(false)
  })

  it('keeps the edit dialog open when the line number update fails', async () => {
    harness.handleMsisdnUpdate.mockResolvedValue(false)
    const wrapper = mountView()

    await wrapper.find('button[aria-label="Edit number"]').trigger('click')
    await wrapper.find('#modem-line-msisdn').setValue('+12242255558')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(harness.handleMsisdnUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#modem-line-msisdn').exists()).toBe(true)
  })
})

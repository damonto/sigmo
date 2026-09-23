import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import NetworkSettingsContent from '@/views/modem-settings/NetworkSettingsContent.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const stubs = {
  Badge: {
    template: '<span><slot /></span>',
  },
  Button: {
    props: ['type', 'disabled'],
    template: '<button :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Card: {
    template: '<section><slot /></section>',
  },
  CardContent: {
    template: '<div><slot /></div>',
  },
  CardHeader: {
    template: '<div><slot /></div>',
  },
  CardTitle: {
    template: '<h2><slot /></h2>',
  },
  Checkbox: {
    props: ['id', 'modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
  Label: {
    props: ['for'],
    template: '<label :for="$props.for"><slot /></label>',
  },
  Select: {
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<div :data-value="modelValue" :data-disabled="disabled"><slot /><button type="button" data-testid="select-manual" @click="$emit(\'update:modelValue\', \'manual\')" /><button type="button" data-testid="select-automatic" @click="$emit(\'update:modelValue\', \'automatic\')" /></div>',
  },
  SelectContent: {
    template: '<div><slot /></div>',
  },
  SelectItem: {
    props: ['value'],
    template: '<div><slot /></div>',
  },
  SelectTrigger: {
    props: ['id'],
    template: '<button type="button" :id="id"><slot /></button>',
  },
  SelectValue: {
    props: ['placeholder'],
    template: '<span>{{ placeholder }}</span>',
  },
  Spinner: {
    template: '<span />',
  },
  Switch: {
    props: ['id', 'modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
}

const mountSection = (overrides: Record<string, unknown> = {}) =>
  mount(NetworkSettingsContent, {
    props: {
      operatorLabel: 'Carrier',
      registrationState: 'Registered',
      accessTechnology: 'LTE',
      registrationMode: 'automatic',
      registeredOperatorCode: '',
      isScanning: false,
      isRegistrationUpdating: false,
      canScanNetworks: true,
      canUpdateRegistration: true,
      modeOptions: [
        {
          allowed: 4,
          preferred: 0,
          allowedLabel: 'LTE',
          preferredLabel: 'None',
          current: true,
        },
      ],
      selectedMode: '4:0',
      supportedBands: [
        { value: { technology: 4, number: 41 }, label: 'LTE B41', current: true },
        { value: { technology: 4, number: 42 }, label: 'LTE B42', current: false },
      ],
      selectedBands: [{ technology: 4, number: 41 }],
      airplaneModeSupported: true,
      airplaneModeEnabled: false,
      isSettingsLoading: false,
      isModeUpdating: false,
      isBandUpdating: false,
      isAirplaneModeUpdating: false,
      canUpdateMode: true,
      canUpdateBands: true,
      canUpdateAirplaneMode: true,
      ...overrides,
    },
    global: {
      stubs,
    },
  })

describe('NetworkSettingsContent', () => {
  it('renders supported modes and bands', () => {
    const wrapper = mountSection()

    expect(wrapper.text()).toContain('LTE')
    expect(wrapper.text()).toContain('LTE B41')
  })

  it('emits band toggle events', async () => {
    const wrapper = mountSection()

    await wrapper.find('#band-4-42').setValue(true)

    expect(wrapper.emitted('toggleBand')).toEqual([[{ technology: 4, number: 42 }, true]])
  })

  it('emits registration mode changes only when the mode differs', async () => {
    const wrapper = mountSection()

    await wrapper.find('[data-testid="select-automatic"]').trigger('click')
    expect(wrapper.emitted('updateRegistrationMode')).toBeUndefined()

    await wrapper.find('[data-testid="select-manual"]').trigger('click')
    expect(wrapper.emitted('updateRegistrationMode')).toEqual([['manual']])
  })

  it('labels the manual option with the pinned operator', () => {
    const wrapper = mountSection({ registrationMode: 'manual', registeredOperatorCode: '46001' })

    expect(wrapper.text()).toContain('modemDetail.settings.networkRegistrationManualOperator')
  })

  it('emits airplane mode updates', async () => {
    const wrapper = mountSection()

    await wrapper.find('#network-airplane-mode').setValue(true)

    expect(wrapper.emitted('updateAirplaneMode')).toEqual([[true]])
  })
})

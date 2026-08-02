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
    template: '<div><slot /></div>',
  },
  SelectContent: {
    template: '<div><slot /></div>',
  },
  SelectItem: {
    props: ['value'],
    template: '<div><slot /></div>',
  },
  SelectTrigger: {
    template: '<button type="button"><slot /></button>',
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

const mountSection = () =>
  mount(NetworkSettingsContent, {
    props: {
      operatorLabel: 'Carrier',
      registrationState: 'Registered',
      accessTechnology: 'LTE',
      isScanning: false,
      canScanNetworks: true,
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
        { value: { technology: 0, number: 0 }, label: 'Any', current: false },
        { value: { technology: 4, number: 41 }, label: 'LTE B41', current: true },
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

    await wrapper.find('#band-0-0').setValue(true)

    expect(wrapper.emitted('toggleBand')).toEqual([[{ technology: 0, number: 0 }, true]])
  })

  it('emits airplane mode updates', async () => {
    const wrapper = mountSection()

    await wrapper.find('#network-airplane-mode').setValue(true)

    expect(wrapper.emitted('updateAirplaneMode')).toEqual([[true]])
  })
})

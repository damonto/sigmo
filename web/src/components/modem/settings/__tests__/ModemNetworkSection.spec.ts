import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ModemNetworkSection from '@/components/modem/settings/ModemNetworkSection.vue'

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
}

const mountSection = () =>
  mount(ModemNetworkSection, {
    props: {
      operatorLabel: 'Carrier',
      registrationState: 'Registered',
      accessTechnology: 'LTE',
      isScanning: false,
      modeOptions: [
        {
          allowed: 8,
          preferred: 0,
          allowedLabel: '4G',
          preferredLabel: 'None',
          current: true,
        },
      ],
      selectedMode: '8:0',
      supportedBands: [
        { value: 256, label: 'Any', current: false },
        { value: 71, label: 'LTE B41', current: true },
      ],
      selectedBands: [71],
      isSettingsLoading: false,
      isModeUpdating: false,
      isBandUpdating: false,
      canUpdateMode: true,
      canUpdateBands: true,
    },
    global: {
      stubs,
    },
  })

describe('ModemNetworkSection', () => {
  it('renders supported modes and bands', () => {
    const wrapper = mountSection()

    expect(wrapper.text()).toContain('4G')
    expect(wrapper.text()).toContain('LTE B41')
  })

  it('emits band toggle events', async () => {
    const wrapper = mountSection()

    await wrapper.find('#band-256').setValue(true)

    expect(wrapper.emitted('toggleBand')).toEqual([[256, true]])
  })
})

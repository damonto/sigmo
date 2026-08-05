import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import WiFiCallingSettingsPanel from '@/views/modem-settings/WiFiCallingSettingsPanel.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { Modem } from '@/types/modem'

const stubs = {
  Button: {
    name: 'Button',
    props: ['type', 'disabled'],
    template:
      '<button data-testid="action-button" :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Card: {
    template: '<section><slot /></section>',
  },
  CardContent: {
    template: '<div><slot /></div>',
  },
  CardHeader: {
    template: '<header><slot /></header>',
  },
  CardTitle: {
    template: '<h2><slot /></h2>',
  },
  Label: {
    props: ['for'],
    template: '<label :for="$props.for"><slot /></label>',
  },
  Spinner: {
    template: '<span />',
  },
  Select: {
    name: 'Select',
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template: '<div data-testid="underlay-select"><slot /></div>',
  },
  SelectContent: {
    template: '<div><slot /></div>',
  },
  SelectItem: {
    props: ['value', 'disabled'],
    template: '<div :data-value="value" :data-disabled="disabled"><slot /></div>',
  },
  SelectTrigger: {
    props: ['id'],
    template: '<button :id="id" type="button"><slot /></button>',
  },
  SelectValue: {
    props: ['placeholder'],
    template: '<span>{{ placeholder }}</span>',
  },
  Switch: {
    props: ['id', 'modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
}

const modem = (id: string, name: string, internetConnected = false): Modem => ({
  manufacturer: 'Example',
  id,
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name,
  number: '',
  state: 'registered',
  unlockRequired: '',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '00101',
    regionCode: '001',
    identifier: `${id}-sim`,
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'registered',
  registeredOperator: { name: 'Carrier', code: '00101' },
  signalQuality: 80,
  airplaneMode: false,
  simKind: 'physical',
  internetConnected,
})

const mountCard = (
  locale: 'en' | 'zh',
  props: Partial<InstanceType<typeof WiFiCallingSettingsPanel>['$props']> = {},
) => {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })

  return mount(WiFiCallingSettingsPanel, {
    props: {
      enabled: true,
      underlay: { mode: 'system' },
      modems: [],
      modemId: 'modem-1',
      isLoading: false,
      isUpdating: false,
      isWebsheetStarting: false,
      isEmergencyAddressStarting: false,
      state: 'connected',
      websheet: null,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('WiFiCallingSettingsPanel', () => {
  it('saves switch changes without rendering an update button', async () => {
    const wrapper = mountCard('en')
    const switches = wrapper.findAll('input[type="checkbox"]')

    expect(wrapper.find('[data-testid="action-button"]').exists()).toBe(false)
    expect(wrapper.get('#modem-wifi-calling-underlay').attributes('type')).toBe('button')

    await switches[0]?.setValue(false)

    expect(wrapper.emitted('update')).toEqual([[{ enabled: false, underlay: { mode: 'system' } }]])
  })

  it('saves current and other modem underlays immediately', async () => {
    const wrapper = mountCard('en', {
      modems: [modem('modem-1', 'Voice', true), modem('modem-2', 'Data', true)],
    })
    const select = wrapper.getComponent({ name: 'Select' })

    select.vm.$emit('update:modelValue', 'self')
    await wrapper.vm.$nextTick()
    select.vm.$emit('update:modelValue', 'modem:modem-2')

    expect(wrapper.text()).toContain('This modem')
    expect(wrapper.text()).toContain('Data (modem-2)')
    expect(wrapper.emitted('update')).toEqual([
      [{ enabled: true, underlay: { mode: 'self' } }],
      [{ enabled: true, underlay: { mode: 'modem', modemId: 'modem-2' } }],
    ])
  })

  it('keeps a configured offline modem visible', () => {
    const wrapper = mountCard('en', {
      underlay: { mode: 'modem', modemId: 'offline-imei' },
      modems: [modem('modem-1', 'Voice')],
    })

    expect(wrapper.text()).toContain('Modem offline-imei (currently offline)')
    expect(
      wrapper.get('[data-value="modem:offline-imei"]').attributes('data-disabled'),
    ).toBeDefined()
  })

  it('only allows modems with a connected Internet connection', async () => {
    const wrapper = mountCard('en', {
      modems: [
        modem('modem-1', 'Voice'),
        modem('modem-2', 'Connected data', true),
        modem('modem-3', 'Disconnected data'),
      ],
    })
    const select = wrapper.getComponent({ name: 'Select' })

    expect(wrapper.find('[data-value="self"]').exists()).toBe(false)
    expect(wrapper.find('[data-value="modem:modem-2"]').exists()).toBe(true)
    expect(wrapper.find('[data-value="modem:modem-3"]').exists()).toBe(false)

    select.vm.$emit('update:modelValue', 'self')
    select.vm.$emit('update:modelValue', 'modem:modem-3')
    select.vm.$emit('update:modelValue', 'modem:modem-2')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update')).toEqual([
      [{ enabled: true, underlay: { mode: 'modem', modemId: 'modem-2' } }],
    ])
  })
})

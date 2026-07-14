import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import type { InternetConnectionResponse } from '@/types/internet'
import InternetConnectionPanel from '@/views/modem-settings/InternetConnectionPanel.vue'

const connection: InternetConnectionResponse = {
  status: 'connected',
  apn: 'internet',
  ipType: 'ipv4v6',
  apnUsername: '',
  apnPassword: '',
  apnAuth: '',
  defaultRoute: true,
  proxyEnabled: false,
  alwaysOn: true,
  proxy: { enabled: false },
  interfaceName: 'wwan0',
  bearer: '/bearer/1',
  ipv4Addresses: ['10.0.0.2/30'],
  ipv6Addresses: [],
  dns: ['1.1.1.1'],
  durationSeconds: 10,
  txBytes: 20,
  rxBytes: 30,
  routeMetric: 10,
}

const stubs = {
  Card: { template: '<section><slot /></section>' },
  CardContent: { template: '<div><slot /></div>' },
  CardHeader: { template: '<header><slot /></header>' },
  CardTitle: { template: '<h2><slot /></h2>' },
  Collapsible: { template: '<div><slot /></div>' },
  CollapsibleContent: { template: '<div><slot /></div>' },
  CollapsibleTrigger: { template: '<button><slot /></button>' },
  Input: {
    props: ['id', 'modelValue', 'disabled', 'type'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" :type="type || \'text\'" :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  Label: { props: ['for'], template: '<label :for="$props.for"><slot /></label>' },
  Select: { template: '<div><slot /></div>' },
  SelectContent: { template: '<div><slot /></div>' },
  SelectItem: { template: '<div><slot /></div>' },
  SelectTrigger: { props: ['id'], template: '<button :id="id"><slot /></button>' },
  SelectValue: { template: '<span />' },
  Switch: {
    props: ['id', 'modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
}

const mountPanel = () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(InternetConnectionPanel, {
    props: {
      apn: 'internet',
      ipType: 'ipv4v6',
      apnUsername: '',
      apnPassword: '',
      apnAuth: 'default',
      defaultRoute: true,
      proxyEnabled: false,
      alwaysOn: true,
      connection,
      isLoading: false,
      isConnecting: false,
      isDisconnecting: false,
      isPreferencesUpdating: false,
      isConnected: true,
      canConnect: false,
    },
    global: { plugins: [i18n], stubs },
  })
}

describe('InternetConnectionPanel', () => {
  it('keeps APN locked while allowing connected preferences to update', async () => {
    const wrapper = mountPanel()

    expect(wrapper.get('#modem-internet-apn').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#modem-internet-default-route').attributes('disabled')).toBeUndefined()

    await wrapper.get('#modem-internet-default-route').setValue(false)

    expect(wrapper.emitted('update-preferences')).toEqual([
      [{ defaultRoute: false, proxyEnabled: false, alwaysOn: true }],
    ])
  })
})

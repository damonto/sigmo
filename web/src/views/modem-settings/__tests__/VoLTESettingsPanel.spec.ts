import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import type { VoLTENetworkDriver } from '@/types/volte'
import VoLTESettingsPanel from '@/views/modem-settings/VoLTESettingsPanel.vue'

const stubs = {
  Card: { template: '<section><slot /></section>' },
  CardContent: { template: '<div><slot /></div>' },
  CardHeader: { template: '<header><slot /></header>' },
  CardTitle: { template: '<h2><slot /></h2>' },
  Label: { template: '<label><slot /></label>' },
  RadioGroup: {
    name: 'RadioGroup',
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template: '<div data-radio-group :data-disabled="disabled"><slot /></div>',
  },
  RadioGroupItem: { template: '<input type="radio" />' },
  Spinner: { template: '<span />' },
  Switch: { template: '<input type="checkbox" />' },
}

const mountPanel = (enabled: boolean, networkDriver: VoLTENetworkDriver = 'legacy_bam_dmux') => {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en },
  })
  return mount(VoLTESettingsPanel, {
    props: {
      enabled,
      networkDriver,
      setImsApnAsDefault: false,
      enablePcscfViaPco: false,
      modemRegistered: false,
      isLoading: false,
      isUpdating: false,
    },
    global: { plugins: [i18n], stubs },
  })
}

describe('VoLTESettingsPanel', () => {
  it('explains the IMS-first legacy BAM-DMUX tradeoff', () => {
    const wrapper = mountPanel(false)

    expect(wrapper.text()).toContain('Only for older Qualcomm BAM-DMUX devices')
    expect(wrapper.text()).toContain('IMS exclusively uses the primary wwan0 channel')
  })

  it('locks the network driver while VoLTE is enabled', () => {
    const wrapper = mountPanel(true)

    expect(wrapper.get('[data-radio-group]').attributes('data-disabled')).toBe('true')
  })

  it('hides the QMI network driver selection for MBIM', () => {
    const wrapper = mountPanel(false, 'mbim')

    expect(wrapper.find('[data-radio-group]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Set IMS APN as default')
    expect(wrapper.text()).not.toContain('Repair IMS APN')
  })

  it('shows the QMI IMS profile options while VoLTE is disabled', () => {
    const wrapper = mountPanel(false, 'qmap')

    expect(wrapper.text()).toContain('Set IMS APN as default')
    expect(wrapper.text()).toContain('Repair IMS APN')
  })
})

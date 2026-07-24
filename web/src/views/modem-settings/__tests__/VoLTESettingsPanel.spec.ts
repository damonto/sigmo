import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import type { VoLTEDataPath } from '@/types/volte'
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

const mountPanel = (enabled: boolean, dataPath: VoLTEDataPath = 'legacy_bam_dmux') => {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en },
  })
  return mount(VoLTESettingsPanel, {
    props: {
      enabled,
      dataPath,
      modemRegistered: false,
      isLoading: false,
      isUpdating: false,
    },
    global: { plugins: [i18n], stubs },
  })
}

describe('VoLTESettingsPanel', () => {
  it('describes legacy BAM-DMUX support without the removed warning', () => {
    const wrapper = mountPanel(false)

    expect(wrapper.text()).toContain('Only for older Qualcomm BAM-DMUX devices')
  })

  it('locks the data path while VoLTE is enabled', () => {
    const wrapper = mountPanel(true)

    expect(wrapper.get('[data-radio-group]').attributes('data-disabled')).toBe('true')
  })

  it('hides the QMI data path selection for MBIM', () => {
    const wrapper = mountPanel(false, 'mbim')

    expect(wrapper.find('[data-radio-group]').exists()).toBe(false)
  })
})

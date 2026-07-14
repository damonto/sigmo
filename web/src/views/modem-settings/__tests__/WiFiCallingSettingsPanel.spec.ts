import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import WiFiCallingSettingsPanel from '@/views/modem-settings/WiFiCallingSettingsPanel.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const stubs = {
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
  Switch: {
    props: ['id', 'modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<input :id="id" type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
}

const mountCard = (locale: 'en' | 'zh') => {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })

  return mount(WiFiCallingSettingsPanel, {
    props: {
      enabled: true,
      preferred: true,
      isLoading: false,
      isUpdating: false,
      isWebsheetStarting: false,
      isEmergencyAddressStarting: false,
      state: 'connected',
      websheet: null,
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

    expect(wrapper.find('button').exists()).toBe(false)

    await switches[0]?.setValue(false)
    await switches[1]?.setValue(false)

    expect(wrapper.emitted('update')).toEqual([
      [{ enabled: false, preferred: false }],
      [{ enabled: true, preferred: false }],
    ])
  })

  it('renders the English preferred Wi-Fi Calling copy for calls', () => {
    const wrapper = mountCard('en')

    expect(wrapper.text()).toContain('Use Wi-Fi Calling when available')
    expect(wrapper.text()).toContain(
      'Use Wi-Fi Calling for messages, USSD, and calls when available',
    )
  })

  it('renders the Chinese preferred Wi-Fi Calling copy for calls', () => {
    const wrapper = mountCard('zh')

    expect(wrapper.text()).toContain('可用时使用 Wi-Fi Calling')
    expect(wrapper.text()).toContain('可用时优先通过 Wi-Fi Calling 处理短信、USSD 和通话')
  })
})

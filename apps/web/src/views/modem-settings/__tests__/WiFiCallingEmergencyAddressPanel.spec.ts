import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import WiFiCallingEmergencyAddressPanel from '@/views/modem-settings/WiFiCallingEmergencyAddressPanel.vue'
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
  Spinner: {
    template: '<span />',
  },
}

const mountCard = (locale: 'en' | 'zh', isStarting = false) => {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })

  return mount(WiFiCallingEmergencyAddressPanel, {
    props: { isStarting },
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('WiFiCallingEmergencyAddressPanel', () => {
  it('renders the English E911 copy', () => {
    const wrapper = mountCard('en')

    expect(wrapper.text()).toContain('E911 Address')
    expect(wrapper.text()).toContain('Update E911 address')
  })

  it('renders the Chinese E911 copy', () => {
    const wrapper = mountCard('zh')

    expect(wrapper.text()).toContain('E911 地址')
    expect(wrapper.text()).toContain('更新 E911 地址')
  })

  it('emits when starting an E911 update', async () => {
    const wrapper = mountCard('en')

    await wrapper.get('button').trigger('click')

    expect(wrapper.emitted('start')).toHaveLength(1)
  })

  it('disables the action while starting', () => {
    const wrapper = mountCard('en', true)

    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })
})

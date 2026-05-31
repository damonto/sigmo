import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import WiFiCallingStatusPanel from '@/views/modem-settings/WiFiCallingStatusPanel.vue'

const stubs = {
  Button: {
    props: ['type', 'title', 'ariaLabel'],
    template:
      '<button :type="type || \'button\'" :title="title" :aria-label="ariaLabel"><slot /></button>',
  },
  Card: {
    template: '<section><slot /></section>',
  },
  CardContent: {
    template: '<div><slot /></div>',
  },
}

const mountPanel = (
  locale: 'en' | 'zh',
  props: Partial<InstanceType<typeof WiFiCallingStatusPanel>['$props']> = {},
) => {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })

  return mount(WiFiCallingStatusPanel, {
    props: {
      enabled: true,
      connected: false,
      state: 'disconnected',
      durationSeconds: 0,
      isLoading: false,
      isUpdating: false,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('WiFiCallingStatusPanel', () => {
  it('shows a reconnect action when Wi-Fi Calling is disconnected', async () => {
    const wrapper = mountPanel('en')

    expect(wrapper.text()).toContain('Disconnected')
    expect(wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').attributes('title')).toBe(
      'Reconnect Wi-Fi Calling',
    )

    await wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').trigger('click')

    expect(wrapper.emitted('reconnect')).toHaveLength(1)
  })

  it('hides reconnect while connected', () => {
    const wrapper = mountPanel('en', { connected: true, state: 'connected', durationSeconds: 65 })

    expect(wrapper.text()).toContain('Connected')
    expect(wrapper.text()).toContain('1m 5s')
    expect(wrapper.find('button[aria-label="Reconnect Wi-Fi Calling"]').exists()).toBe(false)
  })

  it('renders Chinese disabled status', () => {
    const wrapper = mountPanel('zh', { enabled: false, state: 'idle' })

    expect(wrapper.text()).toContain('已关闭')
    expect(wrapper.text()).toContain('当前线路未启用 Wi-Fi Calling')
  })
})

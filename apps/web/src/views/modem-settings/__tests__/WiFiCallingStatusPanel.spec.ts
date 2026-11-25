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
      isReconnecting: false,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('WiFiCallingStatusPanel', () => {
  it('renders the Chinese loading state with a spinner and no reconnect action', () => {
    const wrapper = mountPanel('zh', { isLoading: true })

    expect(wrapper.text()).toContain('检查中')
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('shows a reconnect action when Wi-Fi Calling is disconnected', async () => {
    const wrapper = mountPanel('en')

    expect(wrapper.text()).toContain('Disconnected')
    expect(wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').attributes('title')).toBe(
      'Reconnect Wi-Fi Calling',
    )

    await wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').trigger('click')

    expect(wrapper.emitted('reconnect')).toHaveLength(1)
  })

  it('keeps reconnect available while connected', async () => {
    const wrapper = mountPanel('en', { connected: true, state: 'connected', durationSeconds: 65 })

    expect(wrapper.text()).toContain('Connected')
    expect(wrapper.text()).toContain('1m 5s')
    expect(
      wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').attributes('disabled'),
    ).toBeUndefined()

    await wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').trigger('click')

    expect(wrapper.emitted('reconnect')).toHaveLength(1)
  })

  it('keeps the key nodes together under the status and resets them on a new attempt', async () => {
    const startedAt = '2026-11-25T02:30:00Z'
    const progress = {
      attempt: 1,
      status: 'running',
      stage: 'epdg_eap_aka',
      stageStartedAt: startedAt,
      steps: [{ stage: 'epdg_ike_init', status: 'completed', startedAt }],
    }
    const wrapper = mountPanel('en', { state: 'connecting', progress })
    const row = wrapper.get('ol[aria-label="Connection progress"]')
    const nodes = () => row.findAll('li')
    const statuses = () => nodes().map((node) => node.attributes('title'))

    expect(row.element.parentElement?.firstElementChild?.textContent).toContain('Connecting')
    expect(row.element.parentElement?.parentElement?.nextElementSibling?.textContent).toContain(
      'Connection duration',
    )
    expect(nodes().map((node) => node.get('span.text-xs').text())).toEqual([
      'ePDG',
      'AKA',
      'IPsec',
      'IMS',
    ])
    expect(statuses()).toEqual(['Completed', 'In progress', 'Not reached', 'Not reached'])

    await wrapper.setProps({
      progress: {
        ...progress,
        status: 'waiting',
        stage: 'retry_wait',
        steps: [...progress.steps, { stage: 'epdg_eap_aka', status: 'failed', startedAt }],
      },
    })
    expect(statuses()).toEqual(['Completed', 'Failed', 'Not reached', 'Not reached'])

    await wrapper.setProps({
      progress: { ...progress, attempt: 2, stage: 'epdg_ike_init', steps: [] },
    })
    expect(statuses()).toEqual(['In progress', 'Not reached', 'Not reached', 'Not reached'])

    await wrapper.setProps({
      connected: true,
      state: 'connected',
      progress: { ...progress, attempt: 2, stage: 'ims_starting', status: 'ready', steps: [] },
    })
    expect(row.element.previousElementSibling?.textContent).toBe('Connected')
    expect(statuses()).toEqual(['Completed', 'Completed', 'Completed', 'Completed'])
  })

  it('disables reconnect while reconnecting', () => {
    const wrapper = mountPanel('en', { isReconnecting: true })

    expect(wrapper.text()).toContain('Connecting')
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    expect(
      wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').attributes('disabled'),
    ).toBeDefined()
  })

  it('shows the selected underlay is offline while retrying automatically', () => {
    const wrapper = mountPanel('en', { state: 'waiting_for_uplink' })

    expect(wrapper.text()).toContain('Waiting for Internet')
    expect(wrapper.text()).toContain(
      'The selected modem is offline. Wi-Fi Calling will retry automatically when it reconnects.',
    )
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    expect(
      wrapper.get('button[aria-label="Reconnect Wi-Fi Calling"]').attributes('disabled'),
    ).toBeUndefined()
  })

  it('renders Chinese disabled status', () => {
    const wrapper = mountPanel('zh', { enabled: false, state: 'idle' })

    expect(wrapper.text()).toContain('已关闭')
  })
})

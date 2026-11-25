import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import VoLTEStatusPanel from '@/views/modem-settings/VoLTEStatusPanel.vue'

const stubs = {
  Card: {
    template: '<section><slot /></section>',
  },
  CardContent: {
    template: '<div><slot /></div>',
  },
}

const mountPanel = (
  locale: 'en' | 'zh',
  props: Partial<InstanceType<typeof VoLTEStatusPanel>['$props']> = {},
) => {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })

  return mount(VoLTEStatusPanel, {
    props: {
      enabled: true,
      connected: false,
      modemRegistered: false,
      state: 'disconnected',
      durationSeconds: 0,
      isLoading: false,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

describe('VoLTEStatusPanel', () => {
  it('renders the Chinese loading state', () => {
    const wrapper = mountPanel('zh', { isLoading: true })

    expect(wrapper.text()).toContain('检查中')
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
  })

  it('renders the connecting state with a spinner', () => {
    const wrapper = mountPanel('en', { state: 'connecting' })

    expect(wrapper.text()).toContain('Connecting')
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
  })

  it('lights up the nodes reached during IMS restoration', () => {
    const wrapper = mountPanel('en', {
      connected: true,
      state: 'connected',
      progress: {
        attempt: 2,
        stage: 'ims_aka',
        status: 'running',
        stageStartedAt: '2026-11-25T02:30:00Z',
        steps: [
          { stage: 'ims_transport', status: 'completed', startedAt: '2026-11-25T02:29:59Z' },
          { stage: 'ims_aka', status: 'running', startedAt: '2026-11-25T02:30:00Z' },
        ],
        lastFailure: {
          attempt: 1,
          stage: 'ims_pdn',
          code: 'timeout',
          at: '2026-11-25T02:29:45Z',
        },
      },
    })

    const nodes = wrapper.findAll('ol li')
    expect(nodes.map((node) => node.get('span.text-xs').text())).toEqual([
      'LTE',
      'IMS PDN',
      'AKA',
      'IMS',
    ])
    expect(nodes.map((node) => node.attributes('title'))).toEqual([
      'Completed',
      'Completed',
      'In progress',
      'Not reached',
    ])
    expect(wrapper.get('li[aria-current="step"]').get('span.text-xs').text()).toBe('AKA')
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
  })

  it('shows a failed SIP startup even while the connection state catches up', () => {
    const startedAt = '2026-11-25T02:30:00Z'
    const wrapper = mountPanel('en', {
      state: 'connected',
      connected: true,
      progress: {
        attempt: 1,
        stage: 'ims_starting',
        status: 'failed',
        stageStartedAt: startedAt,
        steps: [{ stage: 'ims_starting', status: 'failed', startedAt }],
        lastFailure: { attempt: 1, stage: 'ims_starting', code: 'timeout', at: startedAt },
      },
    })

    expect(wrapper.text()).toContain('Disconnected')
    expect(wrapper.get('li[title="Failed"]').get('span.text-xs').text()).toBe('IMS')
    expect(wrapper.find('li[aria-current="step"]').exists()).toBe(false)
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('renders the connected tone and formatted duration', () => {
    const wrapper = mountPanel('en', {
      connected: true,
      state: 'connected',
      durationSeconds: 3665,
    })

    expect(wrapper.text()).toContain('Connected')
    expect(wrapper.text()).toContain('1h 1m 5s')
    expect(wrapper.get('section').classes()).toContain('bg-emerald-50/40')
  })

  it('shows zero duration while disconnected', () => {
    const wrapper = mountPanel('en', { durationSeconds: 65 })

    expect(wrapper.text()).toContain('Disconnected')
    expect(wrapper.text()).toContain('Connection duration')
    expect(wrapper.text()).toContain('0s')
  })

  it('renders the Chinese disabled state', () => {
    const wrapper = mountPanel('zh', { enabled: false, state: 'idle' })

    expect(wrapper.text()).toContain('已关闭')
  })

  it('shows modem-managed VoLTE without inventing a duration', () => {
    const wrapper = mountPanel('en', {
      enabled: false,
      modemRegistered: true,
      durationSeconds: 65,
    })

    expect(wrapper.text()).toContain('Connected')
    expect(wrapper.text()).not.toContain('Managed by modem')
    expect(wrapper.text()).toContain('VoLTE is managed by Modem.')
    expect(wrapper.text()).toContain('—')
    expect(wrapper.get('section').classes()).toContain('bg-emerald-50/40')
  })

  it('falls back to disconnected for unsupported states', () => {
    const wrapper = mountPanel('en', { state: 'websheet_required' })

    expect(wrapper.text()).toContain('Disconnected')
  })
})

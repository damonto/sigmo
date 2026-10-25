import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'

import App from '@/App.vue'
import { useLicenseStore } from '@/stores/license'

vi.mock('@/apis/communication', () => ({
  communicationApi: { contacts: vi.fn().mockResolvedValue({ items: [] }) },
}))
vi.mock('@/composables/useCallMonitor', () => ({ useCallMonitor: vi.fn() }))
vi.mock('@/composables/useCapabilities', () => ({
  useCapabilities: () => ({ hasFeature: () => false, fetchCapabilities: async () => undefined }),
}))

const route = vi.hoisted(() => ({
  params: {
    id: 'modem-1',
  },
}))

const sessionHarness = vi.hoisted(() => ({
  activeCall: {
    value: {
      number: '+12242255559',
    },
  },
  callAudio: {
    remoteStream: {
      value: null,
    },
    bindOutputElement: vi.fn(),
  },
}))

const provideModemCallSession = vi.hoisted(() => vi.fn(() => sessionHarness))

vi.mock('vue-router', () => ({
  RouterView: { template: '<main data-testid="route-view">Route</main>' },
  useRoute: () => route,
}))

vi.mock('@/composables/useModemCallSession', () => ({
  provideModemCallSession,
}))

vi.mock('@/composables/useModemPhoneCountry', () => ({
  useModemPhoneCountry: () => ({ phoneCountry: { value: 'US' } }),
}))

describe('App', () => {
  it('renders the call banner at the app shell level', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useLicenseStore().mode = 'unlicensed'
    const wrapper = mount(App, {
      global: {
        plugins: [pinia],
        stubs: {
          ModemCallBanner: {
            props: ['session'],
            template:
              '<aside data-testid="call-banner">{{ session.activeCall.value.number }}</aside>',
          },
          Toaster: { template: '<div data-testid="toaster" />' },
        },
      },
    })

    expect(provideModemCallSession).toHaveBeenCalled()
    expect(wrapper.get('[data-testid="route-view"]').text()).toBe('Route')
    expect(wrapper.get('[data-testid="call-banner"]').text()).toBe('+12242255559')
  })
})

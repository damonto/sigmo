import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import App from '@/App.vue'

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
    const wrapper = mount(App, {
      global: {
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

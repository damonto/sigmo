import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import HomeModemCard from '@/components/home/HomeModemCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountCard = (number: string) =>
  mount(HomeModemCard, {
    props: {
      name: 'RM520N',
      regionCode: 'US',
      operatorName: 'T-Mobile',
      registeredOperatorName: 'T-Mobile',
      registeredOperatorCode: '310260',
      registrationState: 'Registered',
      accessTechnology: 'LTE',
      supportsEsim: true,
      number,
      signalQuality: 72,
      wifiCallingConnected: false,
    },
    global: {
      stubs: {
        RegionFlag: {
          template: '<span />',
        },
        ModemSignalStatus: {
          template: '<span />',
        },
      },
    },
  })

describe('HomeModemCard', () => {
  it('formats the modem number for display', () => {
    const wrapper = mountCard('+12242255559')

    expect(wrapper.text()).toContain('(224) 225-5559')
  })

  it('renders the empty number label when there is no modem number', () => {
    const wrapper = mountCard('')

    expect(wrapper.text()).toContain('home.noNumber')
  })
})

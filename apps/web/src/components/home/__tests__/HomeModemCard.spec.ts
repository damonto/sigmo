import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import HomeModemCard from '@/components/home/HomeModemCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountCard = (number: string, simKind: 'unknown' | 'physical' | 'euicc' = 'euicc') =>
  mount(HomeModemCard, {
    props: {
      name: 'RM520N',
      regionCode: 'US',
      operatorName: 'T-Mobile',
      registeredOperatorName: 'T-Mobile',
      registeredOperatorCode: '310260',
      registrationState: 'Registered',
      accessTechnology: 'LTE',
      simKind,
      number,
      signalQuality: 72,
      airplaneMode: false,
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
  it.each([
    { number: '+12242255559', regionCode: 'US', expected: '+1 (224) 225-5559' },
    { number: '64210647889', regionCode: 'NZ', expected: '+64 210 647 889' },
    { number: '447386042885', regionCode: 'GB', expected: '+44 7386 042885' },
  ])('formats $number with its calling code', async ({ number, regionCode, expected }) => {
    const wrapper = mountCard(number)
    await wrapper.setProps({ regionCode })
    expect(wrapper.text()).toContain(expected)
  })

  it('renders the empty number label when there is no modem number', () => {
    const wrapper = mountCard('')

    expect(wrapper.text()).toContain('home.noNumber')
  })

  it('shows a pending label while SIM classification is unknown', () => {
    const wrapper = mountCard('', 'unknown')

    expect(wrapper.text()).toContain('labels.simUnknown')
    expect(wrapper.text()).not.toContain('labels.psim')
  })
})

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ModemDetailCard from '@/components/modem/ModemDetailCard.vue'
import type { Modem } from '@/types/modem'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) =>
      key === 'modemDetail.fields.controlDevice' ? 'Control Device' : key,
  }),
}))

const modem: Modem = {
  manufacturer: 'Quectel',
  id: '869710031623444',
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1.0.0',
  hardwareRevision: '1.0',
  name: 'RM520N',
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '00101',
    regionCode: 'US',
    identifier: 'sim-1',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'registered',
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 75,
  airplaneMode: false,
  simKind: 'physical',
}

describe('ModemDetailCard', () => {
  it('shows the control device used by Sigmo', () => {
    const wrapper = mount(ModemDetailCard, {
      props: { modem },
      global: {
        stubs: {
          Badge: { template: '<span><slot /></span>' },
          RegionFlag: { template: '<span />' },
        },
      },
    })

    expect(wrapper.text()).toContain('Control Device')
    expect(wrapper.text()).toContain('/dev/cdc-wdm0')
  })

  it('does not report a physical SIM while classification is unknown', () => {
    const wrapper = mount(ModemDetailCard, {
      props: { modem: { ...modem, simKind: 'unknown' } },
      global: {
        stubs: {
          Badge: { template: '<span><slot /></span>' },
          RegionFlag: { template: '<span />' },
        },
      },
    })

    expect(wrapper.text()).toContain('labels.simUnknown')
    expect(wrapper.text()).not.toContain('Physical SIM Only')
  })
})

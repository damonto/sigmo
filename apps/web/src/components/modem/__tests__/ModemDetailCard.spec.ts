import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ModemDetailCard from '@/components/modem/ModemDetailCard.vue'
import type { Modem } from '@/types/modem'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => (key === 'modemDetail.fields.controlDevice' ? 'Control Device' : key),
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
  it('formats the phone number using the SIM region', () => {
    const wrapper = mount(ModemDetailCard, {
      props: { modem: { ...modem, number: '+12025550123' } },
      global: { stubs: { Badge: true, RegionFlag: true } },
    })
    expect(wrapper.text()).toContain('+1 (202) 555-0123')
    expect(wrapper.text()).not.toContain('+12025550123')
  })

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

  it('shows IMEI and every eUICC in the desktop overview', () => {
    const wrapper = mount(ModemDetailCard, {
      props: {
        modem: { ...modem, simKind: 'euicc' },
        variant: 'overview',
        seInfo: {
          ses: [
            { id: 'se-1', label: 'SE 1', eid: 'eid-1', freeSpace: 102400 },
            { id: 'se-2', label: 'SE 2', eid: 'eid-2', freeSpace: 0 },
          ],
        },
      },
      global: { stubs: { Badge: true, RegionFlag: true } },
    })

    expect(wrapper.text()).toContain(modem.id)
    const euicc = wrapper.findAll('section')[1]
    expect(euicc?.findAll('h3').map((heading) => heading.text())).toEqual(['SE 1', 'SE 2'])
    const rows = euicc?.findAll('dl').map((list) => list.text())
    expect(rows).toEqual([expect.stringContaining('eid-1'), expect.stringContaining('eid-2')])
    expect(rows?.[0]).toContain('100 KiB')
    expect(rows?.[1]).toContain('0 B')
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

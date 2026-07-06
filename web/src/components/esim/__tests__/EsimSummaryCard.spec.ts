import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import EsimSummaryCard from '@/components/esim/EsimSummaryCard.vue'
import type { Modem } from '@/types/modem'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      if (key === 'modemDetail.fields.imei') return 'IMEI'
      if (key === 'modemDetail.fields.eid') return 'EID'
      if (key === 'modemDetail.fields.storageRemaining') return 'Storage Remaining'
      if (key === 'modemDetail.actions.copy') return 'Copy'
      return key
    },
  }),
}))

vi.mock('@/lib/clipboard', () => ({
  writeClipboardText: vi.fn(),
}))

const passthrough = {
  template: '<div><slot /></div>',
}

const modem: Modem = {
  manufacturer: 'Quectel',
  id: 'imei-1',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name: 'RM520N',
  number: '',
  state: 'registered',
  unlockRequired: '',
  unlockSupported: false,
  sim: {
    active: true,
    operatorName: '',
    operatorIdentifier: '',
    regionCode: '',
    identifier: '',
  },
  slots: [],
  accessTechnology: null,
  registrationState: '',
  registeredOperator: {
    name: '',
    code: '',
  },
  signalQuality: 0,
  airplaneMode: false,
  supportsEsim: true,
}

describe('EsimSummaryCard', () => {
  it('shows dual SE EIDs and storage summary', () => {
    const wrapper = mount(EsimSummaryCard, {
      props: {
        modem,
        seInfo: {
          ses: [
            { id: 'se0', label: 'SE1', eid: 'eid-1', freeSpace: 102400 },
            { id: 'se1', label: 'SE2', eid: 'eid-2', freeSpace: 204800 },
          ],
        },
      },
      global: {
        stubs: {
          Button: {
            props: ['disabled'],
            template: '<button type="button" :disabled="disabled"><slot /></button>',
          },
          Card: passthrough,
          CardContent: passthrough,
        },
      },
    })

    const text = wrapper.text()

    expect(text).toContain('EID1')
    expect(text).toContain('eid-1')
    expect(text).toContain('EID2')
    expect(text).toContain('eid-2')
    expect(text).toContain('Storage Remaining')
    expect(text).toContain('SE1:')
    expect(text).toContain('100 KiB')
    expect(text).toContain('SE2:')
    expect(text).toContain('200 KiB')
    expect(text).not.toContain('SE 1 Storage Remaining')
    expect(text).not.toContain('SE 2 Storage Remaining')
  })
})

import { mount } from '@vue/test-utils'
import { computed, ref, type Ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useSimSlotSwitch } from '@/composables/useSimSlotSwitch'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  switchSimSlot: vi.fn(),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const modem = (slots: Modem['slots']): Modem => ({
  manufacturer: 'Quectel',
  id: 'modem-1',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name: 'RM520N',
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '00101',
    regionCode: 'US',
    identifier: 'sim-1',
  },
  slots,
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 80,
  supportsEsim: false,
})

describe('useSimSlotSwitch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the active SIM slot when slots are available', () => {
    let current!: ReturnType<typeof useSimSlotSwitch>['currentSimIdentifier']

    mount({
      template: '<div />',
      setup() {
        const currentModem = ref(
          modem([
            {
              active: false,
              operatorName: 'Carrier',
              operatorIdentifier: '00101',
              regionCode: 'US',
              identifier: 'sim-1',
            },
            {
              active: true,
              operatorName: 'Carrier',
              operatorIdentifier: '00101',
              regionCode: 'US',
              identifier: 'sim-2',
            },
          ]),
        )
        current = useSimSlotSwitch({
          modemId: computed(() => 'modem-1'),
          modem: currentModem,
          refreshModem: async () => {},
        }).currentSimIdentifier
      },
    })

    expect(current.value).toBe('sim-2')
  })

  it('treats null slots from lightweight modem responses as empty', async () => {
    let result!: ReturnType<typeof useSimSlotSwitch>
    const currentModem = ref(modem(null as unknown as Modem['slots'])) as Ref<Modem | null>

    mount({
      template: '<div />',
      setup() {
        result = useSimSlotSwitch({
          modemId: computed(() => 'modem-1'),
          modem: currentModem,
          refreshModem: async () => {},
        })
      },
    })

    expect(result.currentSimIdentifier.value).toBe('')
    expect(result.simSlots.value).toEqual([])

    currentModem.value = null
    await Promise.resolve()

    expect(result.currentSimIdentifier.value).toBe('')
    expect(result.simSlots.value).toEqual([])
  })
})

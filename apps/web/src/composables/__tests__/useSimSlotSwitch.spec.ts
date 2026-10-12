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
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1',
  hardwareRevision: '1',
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
  slots,
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 80,
  airplaneMode: false,
  simKind: 'physical',
})

describe('useSimSlotSwitch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the active SIM slot when slots are available', () => {
    let current!: ReturnType<typeof useSimSlotSwitch>['currentSimSlot']

    mount({
      template: '<div />',
      setup() {
        const currentModem = ref(
          modem([
            {
              slot: 1,
              active: false,
              operatorName: 'Carrier',
              operatorIdentifier: '00101',
              regionCode: 'US',
              identifier: 'duplicate-iccid',
            },
            {
              slot: 2,
              active: true,
              operatorName: 'Carrier',
              operatorIdentifier: '00101',
              regionCode: 'US',
              identifier: 'duplicate-iccid',
            },
          ]),
        )
        current = useSimSlotSwitch({
          modemId: computed(() => 'modem-1'),
          modem: currentModem,
          refreshModem: async () => {},
        }).currentSimSlot
      },
    })

    expect(current.value).toBe('2')
  })

  it('switches by physical slot number when ICCIDs are duplicated', async () => {
    let result!: ReturnType<typeof useSimSlotSwitch>
    const refreshModem = vi.fn().mockResolvedValue(undefined)
    const slots = [
      {
        slot: 1,
        active: true,
        operatorName: 'Carrier',
        operatorIdentifier: '00101',
        regionCode: 'US',
        identifier: 'duplicate-iccid',
      },
      {
        slot: 2,
        active: false,
        operatorName: 'Carrier',
        operatorIdentifier: '00101',
        regionCode: 'US',
        identifier: 'duplicate-iccid',
      },
    ]

    mount({
      template: '<div />',
      setup() {
        result = useSimSlotSwitch({
          modemId: computed(() => 'modem-1'),
          modem: ref(modem(slots)),
          refreshModem,
        })
      },
    })

    await result.handleSimSwitch(slots[1])

    expect(api.switchSimSlot).toHaveBeenCalledWith('modem-1', 2)
    expect(refreshModem).toHaveBeenCalledOnce()
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

    expect(result.currentSimSlot.value).toBe('')
    expect(result.simSlots.value).toEqual([])

    currentModem.value = null
    await Promise.resolve()

    expect(result.currentSimSlot.value).toBe('')
    expect(result.simSlots.value).toEqual([])
  })
})

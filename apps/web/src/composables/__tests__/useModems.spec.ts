import { enableAutoUnmount, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModems } from '@/composables/useModems'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  getModems: vi.fn(),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

enableAutoUnmount(afterEach)

const modem = (simKind: Modem['simKind']): Modem => ({
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
    operatorIdentifier: '310260',
    regionCode: 'US',
    identifier: 'sim-1',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'registered',
  registeredOperator: {
    name: 'Carrier',
    code: '310260',
  },
  signalQuality: 80,
  airplaneMode: false,
  simKind,
})

describe('useModems', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('continues polling after the fast retries while SIM classification is pending', async () => {
    vi.useFakeTimers()
    api.getModems
      .mockResolvedValueOnce({ data: ref([modem('unknown')]) })
      .mockResolvedValueOnce({ data: ref([modem('unknown')]) })
      .mockResolvedValueOnce({ data: ref([modem('unknown')]) })
      .mockResolvedValueOnce({ data: ref([modem('unknown')]) })
      .mockResolvedValueOnce({ data: ref([modem('euicc')]) })
    let state!: ReturnType<typeof useModems>

    mount({
      template: '<div />',
      setup() {
        state = useModems()
      },
    })

    await state.fetchModems()

    expect(state.modems.value[0]?.simKind).toBe('unknown')
    expect(api.getModems).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1000 + 2000 + 4000 + 10_000)

    expect(api.getModems).toHaveBeenCalledTimes(5)
    expect(state.modems.value[0]?.simKind).toBe('euicc')
  })

  it('stops polling when the last list consumer is unmounted', async () => {
    vi.useFakeTimers()
    api.getModems.mockResolvedValue({ data: ref([modem('unknown')]) })
    let state!: ReturnType<typeof useModems>

    const wrapper = mount({
      template: '<div />',
      setup() {
        state = useModems()
      },
    })
    await state.fetchModems()
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(30_000)

    expect(api.getModems).toHaveBeenCalledOnce()
  })
})

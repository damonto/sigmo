import { enableAutoUnmount, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshModemList, useModems } from '@/composables/useModems'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  getModems: vi.fn(),
}))
const { updating } = await vi.hoisted(async () => ({ updating: (await import('vue')).ref(false) }))

vi.mock('@/composables/useAppUpdate', () => ({ useAppUpdate: () => ({ updating }) }))

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
    updating.value = false
    vi.clearAllMocks()
  })

  afterEach(() => {
    updating.value = false
    vi.useRealTimers()
  })

  it('keeps the new active profile when a superseded list request completes later', async () => {
    const beforeSwitch = modem('euicc')
    const afterSwitch = {
      ...beforeSwitch,
      sim: { ...beforeSwitch.sim, identifier: 'sim-2', operatorName: 'New carrier' },
    }
    let finishFirstRequest!: () => void
    const blocked = new Promise<void>((resolve) => {
      finishFirstRequest = resolve
    })
    api.getModems
      .mockImplementationOnce(async () => {
        await blocked
        return { data: ref([beforeSwitch]) }
      })
      .mockResolvedValueOnce({ data: ref([afterSwitch]) })

    const state = useModems()
    const firstRequest = state.fetchModems()
    await refreshModemList()
    const activeProfile = state.getModemById(afterSwitch.id)?.sim
    finishFirstRequest()
    await firstRequest

    expect(activeProfile).toEqual(afterSwitch.sim)
    expect(state.getModemById(afterSwitch.id)?.sim).toEqual(afterSwitch.sim)
    expect(state.isLoading.value).toBe(false)
    expect(api.getModems).toHaveBeenCalledTimes(2)
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

  it('pauses SIM classification retries during updates and resumes afterward', async () => {
    vi.useFakeTimers()
    api.getModems
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

    updating.value = true
    await state.fetchModems()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.getModems).toHaveBeenCalledTimes(1)
    expect(state.modems.value[0]?.simKind).toBe('unknown')

    updating.value = false
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.getModems).toHaveBeenCalledTimes(2)
    expect(state.modems.value[0]?.simKind).toBe('euicc')
  })

  it('continues SIM classification retries after a request fails', async () => {
    vi.useFakeTimers()
    api.getModems
      .mockResolvedValueOnce({ data: ref([modem('unknown')]) })
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce({ data: ref([modem('euicc')]) })
    let state!: ReturnType<typeof useModems>
    mount({
      template: '<div />',
      setup() {
        state = useModems()
      },
    })
    await state.fetchModems()
    await vi.advanceTimersByTimeAsync(1000 + 2000)

    expect(api.getModems).toHaveBeenCalledTimes(3)
    expect(api.getModems).toHaveBeenLastCalledWith({ quiet: true })
    expect(state.modems.value[0]?.simKind).toBe('euicc')
  })
})

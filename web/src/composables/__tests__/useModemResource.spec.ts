import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'
import { useModemResource } from '@/composables/useModemResource'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  getModem: vi.fn(),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

enableAutoUnmount(afterEach)

const modem = (id: string, regionCode = 'US', simKind: Modem['simKind'] = 'physical'): Modem => ({
  manufacturer: 'Quectel',
  id,
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1',
  hardwareRevision: '1',
  name: id,
  number: '+12242255559',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '310260',
    regionCode,
    identifier: 'sim-1',
  },
  slots: [],
  accessTechnology: null,
  registrationState: 'registered',
  registeredOperator: {
    name: 'Carrier',
    code: '310260',
  },
  signalQuality: 80,
  airplaneMode: false,
  simKind,
})

describe('useModemResource', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shares one modem request for the same modem id', async () => {
    api.getModem.mockResolvedValueOnce({ data: ref(modem('shared-modem', 'US')) })
    let first!: ReturnType<typeof useModemResource>
    let second!: ReturnType<typeof useModemPhoneCountry>

    mount({
      template: '<div />',
      setup() {
        const modemId = computed(() => 'shared-modem')
        first = useModemResource(modemId)
        second = useModemPhoneCountry(modemId)
      },
    })

    await flushPromises()

    expect(api.getModem).toHaveBeenCalledTimes(1)
    expect(first.modem.value?.id).toBe('shared-modem')
    expect(second.phoneCountry.value).toBe('US')
  })

  it('refreshes the cached modem resource on demand', async () => {
    api.getModem
      .mockResolvedValueOnce({ data: ref(modem('refresh-modem', 'US')) })
      .mockResolvedValueOnce({ data: ref(modem('refresh-modem', 'CN')) })
    let resource!: ReturnType<typeof useModemResource>
    let country!: ReturnType<typeof useModemPhoneCountry>

    mount({
      template: '<div />',
      setup() {
        const modemId = computed(() => 'refresh-modem')
        resource = useModemResource(modemId)
        country = useModemPhoneCountry(modemId)
      },
    })
    await flushPromises()

    expect(resource.modem.value?.sim.regionCode).toBe('US')
    expect(country.phoneCountry.value).toBe('US')

    await resource.refresh()

    expect(api.getModem).toHaveBeenCalledTimes(2)
    expect(resource.modem.value?.sim.regionCode).toBe('CN')
    expect(country.phoneCountry.value).toBe('CN')
  })

  it('continues polling after the fast retries while SIM classification is pending', async () => {
    vi.useFakeTimers()
    api.getModem
      .mockResolvedValueOnce({ data: ref(modem('slow-pending-modem', 'US', 'unknown')) })
      .mockResolvedValueOnce({ data: ref(modem('slow-pending-modem', 'US', 'unknown')) })
      .mockResolvedValueOnce({ data: ref(modem('slow-pending-modem', 'US', 'unknown')) })
      .mockResolvedValueOnce({ data: ref(modem('slow-pending-modem', 'US', 'unknown')) })
      .mockResolvedValueOnce({ data: ref(modem('slow-pending-modem', 'US', 'euicc')) })
    let resource!: ReturnType<typeof useModemResource>

    mount({
      template: '<div />',
      setup() {
        resource = useModemResource(computed(() => 'slow-pending-modem'))
      },
    })
    await flushPromises()

    expect(resource.modem.value?.simKind).toBe('unknown')
    expect(api.getModem).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1000 + 2000 + 4000 + 10_000)

    expect(api.getModem).toHaveBeenCalledTimes(5)
    expect(resource.modem.value?.simKind).toBe('euicc')
  })

  it('stops polling when the last resource consumer is unmounted', async () => {
    vi.useFakeTimers()
    api.getModem.mockResolvedValue({
      data: ref(modem('disposed-pending-modem', 'US', 'unknown')),
    })

    const wrapper = mount({
      template: '<div />',
      setup() {
        useModemResource(computed(() => 'disposed-pending-modem'))
      },
    })
    await flushPromises()
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(30_000)

    expect(api.getModem).toHaveBeenCalledOnce()
  })
})

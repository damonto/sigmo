import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref, type Ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemDetail } from '@/composables/useModemDetail'
import { invalidateSession } from '@/lib/authSession'
import type { EsimProfilesResponse } from '@/types/esim'
import type { Modem } from '@/types/modem'
import type { SEsResponse } from '@/types/se'

const modemResource = vi.hoisted(() => ({
  modem: undefined as Ref<Modem | null> | undefined,
  refresh: vi.fn(),
}))

const esimApi = vi.hoisted(() => ({
  getEsims: vi.fn(),
}))

const seApi = vi.hoisted(() => ({
  getSEs: vi.fn(),
}))

vi.mock('@/composables/useModemResource', async () => {
  const { computed, ref } = await vi.importActual<typeof import('vue')>('vue')
  modemResource.modem = ref(null)
  return {
    useModemResource: () => ({
      modem: modemResource.modem,
      isLoading: computed(() => false),
      error: computed(() => null),
      refresh: modemResource.refresh,
    }),
  }
})

vi.mock('@/apis/esim', () => ({
  useEsimApi: () => esimApi,
}))

vi.mock('@/apis/se', () => ({
  useSEApi: () => seApi,
}))

enableAutoUnmount(afterEach)

const modem = (simKind: Modem['simKind'], id = 'modem-1'): Modem => ({
  manufacturer: 'Quectel',
  id,
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
    identifier: `sim-${id}`,
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'registered',
  registeredOperator: { name: 'Carrier', code: '310260' },
  signalQuality: 80,
  airplaneMode: false,
  simKind,
})

const esimResponse = (id: string): EsimProfilesResponse => ({
  ses: [
    {
      id: `se-${id}`,
      label: `SE ${id}`,
      profiles: [
        {
          seId: `se-${id}`,
          seLabel: `SE ${id}`,
          name: `profile-${id}`,
          serviceProviderName: 'Carrier',
          iccid: `iccid-${id}`,
          icon: '',
          profileName: `Profile ${id}`,
          profileState: 1,
          profileStateName: 'enabled',
          profileClass: 'operational',
          profileOwner: { mcc: '310', mnc: '260' },
        },
      ],
    },
  ],
})

const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('useModemDetail', () => {
  it('shares overlapping explicit refreshes and keeps the previous snapshot while refreshing', async () => {
    let detail: ReturnType<typeof useModemDetail> | undefined
    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })
    if (!detail || !modemResource.modem) throw new Error('missing modem resource')
    esimApi.getEsims.mockResolvedValue({ data: ref(esimResponse('modem-1')) })
    modemResource.modem.value = modem('euicc')
    await flushPromises()
    esimApi.getEsims.mockClear()
    const overview = deferred<void>()
    modemResource.refresh.mockReturnValueOnce(overview.promise)
    const first = detail.fetchModemDetail('modem-1')
    await flushPromises()
    const second = detail.fetchModemDetail('modem-1')
    expect(first).toBe(second)
    expect(detail.esimProfiles.value[0]?.name).toBe('profile-modem-1')
    overview.resolve()
    await Promise.all([first, second])
    expect(modemResource.refresh).toHaveBeenCalledTimes(1)
    expect(esimApi.getEsims).toHaveBeenCalledTimes(1)
  })

  it.each(['session', 'SIM', 'unmount'])(
    'discards a pending inventory read after %s changes',
    async (change) => {
      const pending = deferred<{ data: Ref<EsimProfilesResponse> }>()
      esimApi.getEsims.mockReturnValueOnce(pending.promise)
      let detail: ReturnType<typeof useModemDetail> | undefined
      const wrapper = mount({
        template: '<div />',
        setup() {
          detail = useModemDetail()
        },
      })
      if (!detail || !modemResource.modem) throw new Error('missing modem resource')
      modemResource.modem.value = modem('euicc')
      await flushPromises()
      if (change === 'session') invalidateSession()
      else if (change === 'SIM') modemResource.modem.value.sim.identifier = 'replacement'
      else wrapper.unmount()
      pending.resolve({ data: ref(esimResponse('old')) })
      await flushPromises()
      expect(detail.esimProfiles.value).toEqual([])
      expect(detail.isEsimProfilesLoading.value).toBe(false)
      if (change !== 'unmount') {
        esimApi.getEsims.mockResolvedValueOnce({ data: ref(esimResponse('new')) })
        await detail.fetchModemDetail('modem-1')
        expect(detail.esimProfiles.value[0]?.name).toBe('profile-new')
      }
    },
  )

  it('does not start inventory reads after a stale overview finishes', async () => {
    let detail: ReturnType<typeof useModemDetail> | undefined
    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })
    if (!detail || !modemResource.modem) throw new Error('missing modem resource')
    modemResource.modem.value = modem('euicc')
    await flushPromises()
    esimApi.getEsims.mockClear()
    const overview = deferred<void>()
    modemResource.refresh.mockReturnValueOnce(overview.promise)
    const refresh = detail.fetchModemDetail('modem-1')
    await flushPromises()
    invalidateSession()
    overview.resolve()
    await refresh
    expect(esimApi.getEsims).not.toHaveBeenCalled()
  })

  it('reloads the replacement SIM when it changes during an explicit inventory refresh', async () => {
    let detail: ReturnType<typeof useModemDetail> | undefined
    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })
    if (!detail || !modemResource.modem) throw new Error('missing modem resource')
    modemResource.modem.value = modem('euicc')
    await flushPromises()
    const old = deferred<{ data: Ref<EsimProfilesResponse> }>()
    esimApi.getEsims.mockReturnValueOnce(old.promise)
    const refresh = detail.fetchModemDetail('modem-1')
    await flushPromises()
    esimApi.getEsims.mockResolvedValue({ data: ref(esimResponse('replacement')) })
    modemResource.modem.value.sim.identifier = 'replacement'
    await flushPromises()
    expect(detail.esimProfiles.value[0]?.name).toBe('profile-replacement')
    old.resolve({ data: ref(esimResponse('old')) })
    await refresh
    expect(detail.esimProfiles.value[0]?.name).toBe('profile-replacement')
  })

  it('does not enumerate the card for status-only updates and refreshes after an explicit reload', async () => {
    let detail: ReturnType<typeof useModemDetail> | undefined
    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })
    const resource = modemResource.modem
    if (!resource || !detail) throw new Error('missing modem resource')
    resource.value = modem('euicc')
    await flushPromises()
    resource.value = { ...modem('euicc'), signalQuality: 45 }
    await flushPromises()
    expect(esimApi.getEsims).toHaveBeenCalledTimes(1)
    expect(seApi.getSEs).toHaveBeenCalledTimes(1)
    await detail.fetchModemDetail('modem-1')
    expect(esimApi.getEsims).toHaveBeenCalledTimes(2)
    expect(seApi.getSEs).toHaveBeenCalledTimes(2)
  })

  it('coalesces concurrent inventory reads', async () => {
    let detail: ReturnType<typeof useModemDetail> | undefined
    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })
    const resource = modemResource.modem
    if (!resource || !detail) throw new Error('missing modem resource')
    resource.value = modem('euicc')
    await flushPromises()
    esimApi.getEsims.mockClear()
    await Promise.all([detail.fetchEsimProfiles('modem-1'), detail.fetchEsimProfiles('modem-1')])
    expect(esimApi.getEsims).toHaveBeenCalledTimes(1)
  })

  it.each([undefined, '', '+15551234567'])(
    'maps a profile number from the API: %s',
    async (number) => {
      const response = esimResponse('modem-1')
      for (const group of response.ses) {
        for (const profile of group.profiles) profile.number = number
      }
      esimApi.getEsims.mockResolvedValue({ data: ref(response) })
      let detail: ReturnType<typeof useModemDetail> | undefined
      mount({
        template: '<div />',
        setup() {
          detail = useModemDetail()
        },
      })
      if (!modemResource.modem) throw new Error('modem resource is unavailable')
      modemResource.modem.value = modem('euicc')
      await flushPromises()
      expect(detail?.esimProfiles.value[0]?.number).toBe(number ?? '')
    },
  )

  beforeEach(() => {
    vi.clearAllMocks()
    modemResource.modem!.value = null
    esimApi.getEsims.mockResolvedValue({ data: ref({ ses: [] }) })
    seApi.getSEs.mockResolvedValue({ data: ref({ ses: [] }) })
  })

  it('loads eSIM data when a pending SIM is classified as an eUICC', async () => {
    mount({
      template: '<div />',
      setup() {
        useModemDetail()
      },
    })

    modemResource.modem!.value = modem('unknown')
    await flushPromises()

    expect(seApi.getSEs).not.toHaveBeenCalled()
    expect(esimApi.getEsims).not.toHaveBeenCalled()

    modemResource.modem!.value = modem('euicc')
    await flushPromises()

    expect(seApi.getSEs).toHaveBeenCalledExactlyOnceWith('modem-1')
    expect(esimApi.getEsims).toHaveBeenCalledExactlyOnceWith('modem-1')
  })

  it('ignores eSIM data returned for a previously selected modem', async () => {
    const staleSE = deferred<{ data: Ref<SEsResponse> }>()
    const staleProfiles = deferred<{ data: Ref<EsimProfilesResponse> }>()
    seApi.getSEs.mockImplementation((id: string) => {
      if (id === 'modem-1') return staleSE.promise
      return Promise.resolve({ data: ref({ ses: [{ id: 'se-modem-2', label: 'SE modem-2' }] }) })
    })
    esimApi.getEsims.mockImplementation((id: string) => {
      if (id === 'modem-1') return staleProfiles.promise
      return Promise.resolve({ data: ref(esimResponse(id)) })
    })
    let detail!: ReturnType<typeof useModemDetail>

    mount({
      template: '<div />',
      setup() {
        detail = useModemDetail()
      },
    })

    modemResource.modem!.value = modem('euicc', 'modem-1')
    await flushPromises()
    modemResource.modem!.value = modem('euicc', 'modem-2')
    await flushPromises()

    expect(detail.seInfo.value?.ses[0]?.id).toBe('se-modem-2')
    expect(detail.esimProfiles.value[0]?.name).toBe('profile-modem-2')

    staleSE.resolve({ data: ref({ ses: [{ id: 'se-modem-1', label: 'SE modem-1' }] }) })
    staleProfiles.resolve({ data: ref(esimResponse('modem-1')) })
    await flushPromises()

    expect(detail.seInfo.value?.ses[0]?.id).toBe('se-modem-2')
    expect(detail.esimProfiles.value[0]?.name).toBe('profile-modem-2')
  })
})

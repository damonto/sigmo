import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref, type Ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemDetail } from '@/composables/useModemDetail'
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

    expect(seApi.getSEs).toHaveBeenCalledOnce()
    expect(seApi.getSEs).toHaveBeenCalledWith('modem-1')
    expect(esimApi.getEsims).toHaveBeenCalledOnce()
    expect(esimApi.getEsims).toHaveBeenCalledWith('modem-1')
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

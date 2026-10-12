import { flushPromises } from '@vue/test-utils'
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemIcons } from '@/composables/useModemIcons'
import type { EsimProfileApiResponse, EsimProfilesResponse } from '@/types/esim'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  getEsimsQuietly: vi.fn(),
  token: null as string | null,
  replace: vi.fn(),
}))
vi.mock('@/apis/esim', () => ({ useEsimApi: () => api }))
vi.mock('@/lib/authStorage', () => ({
  getStoredToken: () => api.token,
  clearStoredToken: () => {
    api.token = null
  },
}))
vi.mock('@/router', () => ({ default: { replace: api.replace } }))

const modem = (iccid = 'one'): Modem => ({
  manufacturer: '',
  id: 'device',
  primaryPort: '',
  firmwareRevision: '',
  hardwareRevision: '',
  name: '',
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '310070',
    regionCode: 'US',
    identifier: iccid,
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: { name: 'Roaming', code: '23415' },
  signalQuality: 80,
  airplaneMode: false,
  simKind: 'euicc',
})

const profile = (iccid: string, icon: string, state = 1): EsimProfileApiResponse => ({
  seId: 'se-a',
  seLabel: '',
  name: '',
  serviceProviderName: '',
  iccid,
  icon,
  profileName: '',
  profileState: state,
  profileStateName: '',
  profileClass: '',
  profileOwner: { mcc: '310', mnc: '070' },
})
const profiles = (...items: EsimProfileApiResponse[]): EsimProfilesResponse => ({
  ses: items.map((item, index) => ({ id: `se-${index}`, label: '', profiles: [item] })),
})

const scopes: ReturnType<typeof effectScope>[] = []
const start = (devices = [modem()]) => {
  const scope = effectScope()
  scopes.push(scope)
  const modems = ref(devices)
  const state = scope.run(() => useModemIcons(modems))
  if (!state) throw new Error('scope did not start')
  return { ...state, modems, scope }
}

beforeEach(() => {
  vi.clearAllMocks()
  api.token = null
  api.getEsimsQuietly.mockResolvedValue(profiles(profile('one', 'embedded')))
})
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop()
})

describe('useModemIcons', () => {
  it('matches the active ICCID across SEs and reuses the result on ordinary refresh', async () => {
    api.getEsimsQuietly.mockResolvedValue(
      profiles(profile('other', 'wrong'), profile('one', 'right'), profile('one', 'disabled', 0)),
    )
    const state = start()
    expect(state.icons.value.device?.pending).toBe(true)
    await flushPromises()
    expect(state.icons.value.device?.icon).toBe('right')
    state.modems.value = [modem()]
    await flushPromises()
    expect(api.getEsimsQuietly).toHaveBeenCalledTimes(1)
    state.refreshIcons()
    await flushPromises()
    expect(api.getEsimsQuietly).toHaveBeenCalledTimes(2)
  })

  it('deduplicates reads and discards a late result after switching SIMs', async () => {
    let resolveOld: (result: EsimProfilesResponse) => void = () => {
      throw new Error('request not started')
    }
    api.getEsimsQuietly.mockReturnValueOnce(
      new Promise<EsimProfilesResponse>((resolve) => {
        resolveOld = resolve
      }),
    )
    const state = start()
    state.modems.value = [modem()]
    await nextTick()
    expect(api.getEsimsQuietly).toHaveBeenCalledTimes(1)
    api.getEsimsQuietly.mockResolvedValue(profiles(profile('two', 'new')))
    state.modems.value = [modem('two')]
    await flushPromises()
    resolveOld(profiles(profile('one', 'old')))
    await flushPromises()
    expect(state.icons.value.device?.icon).toBe('new')
  })

  it('cancels removed devices and scope disposal', async () => {
    api.getEsimsQuietly.mockImplementation(
      (_id: string, signal: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal.addEventListener('abort', () =>
            reject(new DOMException('cancelled', 'AbortError')),
          )
        }),
    )
    const state = start()
    const firstSignal: AbortSignal = api.getEsimsQuietly.mock.calls[0]?.[1]
    state.modems.value = []
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    expect(state.icons.value).toEqual({})
    state.modems.value = [modem()]
    await nextTick()
    const secondSignal: AbortSignal = api.getEsimsQuietly.mock.calls[1]?.[1]
    state.scope.stop()
    expect(secondSignal.aborted).toBe(true)
  })

  it('falls back on errors, retries manually, and excludes locked and physical SIMs', async () => {
    api.getEsimsQuietly.mockRejectedValueOnce(new Error('unavailable'))
    const state = start()
    await flushPromises()
    expect(state.icons.value.device?.pending).toBe(false)
    expect(state.icons.value.device?.icon).toBeUndefined()
    state.refreshIcons()
    await flushPromises()
    expect(state.icons.value.device?.icon).toBe('embedded')
    state.modems.value = [
      { ...modem(), state: 'locked' },
      { ...modem(), id: 'physical', simKind: 'physical' },
    ]
    await flushPromises()
    expect(state.icons.value).toEqual({})
    expect(api.getEsimsQuietly).toHaveBeenCalledTimes(2)
  })

  it('rejects ambiguous matches and results from a previous login', async () => {
    api.getEsimsQuietly.mockResolvedValue(profiles(profile('one', 'a'), profile('one', 'b')))
    const state = start()
    await flushPromises()
    expect(state.icons.value.device?.icon).toBeUndefined()
    api.token = 'next-session'
    api.getEsimsQuietly.mockResolvedValue(profiles(profile('one', 'new-session')))
    state.modems.value = [modem()]
    await flushPromises()
    expect(state.icons.value.device?.icon).toBe('new-session')
  })
})

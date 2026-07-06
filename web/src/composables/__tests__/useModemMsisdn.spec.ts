import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemMsisdn } from '@/composables/useModemMsisdn'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  updateMsisdn: vi.fn(),
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const modem = (number: string): Modem => ({
  manufacturer: 'Quectel',
  id: 'modem-1',
  firmwareRevision: '1.0',
  hardwareRevision: 'A',
  name: 'RM520N',
  number,
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    active: true,
    operatorName: 'T-Mobile',
    operatorIdentifier: '310260',
    regionCode: 'US',
    identifier: '8901',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: {
    name: 'T-Mobile',
    code: '310260',
  },
  signalQuality: 76,
  airplaneMode: false,
  supportsEsim: false,
})

const mountComposable = (initialModem = modem('+12242255559')) => {
  const currentModem = ref<Modem | null>(initialModem)
  const refreshModem = vi.fn()
  let state!: ReturnType<typeof useModemMsisdn>

  mount({
    template: '<div />',
    setup() {
      state = useModemMsisdn({
        modemId: computed(() => 'modem-1'),
        modem: currentModem,
        refreshModem,
      })
      return {}
    },
  })

  return { currentModem, refreshModem, state }
}

describe('useModemMsisdn', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.updateMsisdn.mockResolvedValue(undefined)
  })

  it('formats the modem number for the edit field', () => {
    const { state } = mountComposable()

    expect(state.msisdnInput.value).toBe('(224) 225-5559')
  })

  it('formats user input while keeping the submitted national value raw', async () => {
    const { refreshModem, state } = mountComposable()

    state.msisdnInput.value = '(224) 225-5558'
    await state.handleMsisdnUpdate()
    await flushPromises()

    expect(state.msisdnInput.value).toBe('(224) 225-5558')
    expect(api.updateMsisdn).toHaveBeenCalledWith('modem-1', '2242255558')
    expect(refreshModem).toHaveBeenCalledWith('modem-1')
  })

  it('submits international numbers with plus', async () => {
    const { state } = mountComposable()

    state.msisdnInput.value = '86 133 4444 5555'
    await state.handleMsisdnUpdate()
    await flushPromises()

    expect(api.updateMsisdn).toHaveBeenCalledWith('modem-1', '+8613344445555')
  })

  it('keeps plus when a default-country number includes the country code', async () => {
    const { state } = mountComposable()

    state.msisdnInput.value = '1 224 225 5559'
    await state.handleMsisdnUpdate()
    await flushPromises()

    expect(api.updateMsisdn).toHaveBeenCalledWith('modem-1', '+12242255559')
  })
})

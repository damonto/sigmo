import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CommunicationDialpad from '@/components/communication/CommunicationDialpad.vue'
import CommunicationModemSelect from '@/components/communication/CommunicationModemSelect.vue'
import ModemDialpad from '@/components/modem/ModemDialpad.vue'
import type { Modem } from '@/types/modem'
const api = vi.hoisted(() => ({ getModems: vi.fn(), getModem: vi.fn() }))
const dial = vi.hoisted(() => vi.fn())
vi.mock('@/apis/modem', () => ({ useModemApi: () => api }))
vi.mock('@/composables/useCommunicationDial', () => ({ useCommunicationDial: () => dial }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const modem = (id: string, profile = `sim-${id}`): Modem => ({
  id,
  name: `Device ${id}`,
  manufacturer: '',
  primaryPort: '',
  firmwareRevision: '',
  hardwareRevision: '',
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '',
    regionCode: '',
    identifier: profile,
    label: 'Work',
  },
  slots: [],
  accessTechnology: '',
  registrationState: '',
  registeredOperator: { name: '', code: '' },
  signalQuality: 0,
  airplaneMode: false,
  simKind: 'euicc',
})

const mountDialpad = () =>
  mount(CommunicationDialpad, { global: { stubs: { Dialog: true, DraggableFab: true } } })
let wrapper: ReturnType<typeof mountDialpad>
beforeEach(() => {
  vi.clearAllMocks()
  api.getModems.mockResolvedValue({ data: ref([modem('a'), modem('b')]) })
  api.getModem.mockResolvedValue({ data: ref(modem('b', 'replacement')) })
  dial.mockResolvedValue(true)
})
afterEach(() => wrapper?.unmount())
describe('backend call dialpad', () => {
  it('requires a modem for a new call and dials with its current SIM', async () => {
    wrapper = mountDialpad()
    await wrapper.get('input[type="tel"]').setValue('10086')
    await flushPromises()
    const keypad = wrapper.getComponent(ModemDialpad)
    expect(keypad.props('canDial')).toBe(false)
    const picker = wrapper.getComponent(CommunicationModemSelect)
    picker.vm.$emit('update:modelValue', 'b')
    await flushPromises()
    keypad.vm.$emit('dial')
    await flushPromises()
    expect(dial).toHaveBeenCalledExactlyOnceWith('10086', {
      modemId: 'b',
      profileId: 'replacement',
    })
    expect(keypad.props('digits')).toBe('')
  })
  it('uses the sole modem without a switching control and retains failed numbers', async () => {
    api.getModems.mockResolvedValue({ data: ref([modem('b')]) })
    dial.mockRejectedValue(new Error('SIM changed'))
    wrapper = mountDialpad()
    await wrapper.get('input[type="tel"]').setValue('10086')
    await flushPromises()
    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
    wrapper.getComponent(ModemDialpad).vm.$emit('dial')
    await flushPromises()
    expect(wrapper.getComponent(ModemDialpad).props('digits')).toBe('10086')
    expect(wrapper.get('[role="alert"]').text()).toBe('SIM changed')
  })
})

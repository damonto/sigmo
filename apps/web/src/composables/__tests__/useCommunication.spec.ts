import { effectScope, reactive, ref } from 'vue'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  chooseCommunicationModem,
  useCommunicationComposer,
} from '@/composables/useCommunicationComposer'
import { clearContacts, useContacts } from '@/composables/useContacts'
import { useContactEditor } from '@/composables/useContactEditor'
import type { Contact } from '@/types/communication'
import type { Modem } from '@/types/modem'

const api = vi.hoisted(() => ({
  contacts: vi.fn(),
  getModems: vi.fn(),
  getModem: vi.fn(),
  lines: vi.fn(),
  preference: vi.fn(),
  send: vi.fn(),
  saveContact: vi.fn(),
  saveAvatar: vi.fn(),
  removeAvatar: vi.fn(),
}))
const dial = vi.hoisted(() => vi.fn())
vi.mock('@/apis/modem', () => ({ useModemApi: () => api }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))
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
const contact = (id: string): Contact => ({
  id,
  name: id,
  notes: '',
  numbers: [{ number: '+14155552671', key: '+14155552671', label: 'mobile', region: 'US' }],
  hasAvatar: false,
  avatarColor: '',
  updatedAt: '',
})
beforeEach(() => {
  vi.clearAllMocks()
  clearContacts()
})

describe('communication modem choice', () => {
  it.each([
    { devices: [modem('a')], original: '', expected: 'a' },
    { devices: [modem('a'), modem('b')], original: '', expected: '' },
    { devices: [modem('a'), modem('b')], original: 'b', expected: 'b' },
    { devices: [modem('a')], original: 'removed', expected: 'a' },
    { devices: [modem('a'), modem('b')], original: 'removed', expected: '' },
  ])('chooses the original or sole modem: $original', ({ devices, original, expected }) => {
    expect(chooseCommunicationModem(devices, original)).toBe(expected)
  })
  it.each(['message', 'call'] as const)(
    'submits %s using the selected modem’s active SIM after a swap',
    async (mode) => {
      api.getModems.mockResolvedValue({ data: ref([modem('a'), modem('b')]) })
      api.getModem.mockResolvedValue({ data: ref(modem('b', 'replacement')) })
      api.send.mockResolvedValue({})
      dial.mockResolvedValue(true)
      const scope = effectScope(),
        participant = ref('10086')
      const state = scope.run(() =>
        useCommunicationComposer(participant, ref(mode), ref('removed')),
      )
      if (!state) throw new Error('missing composer')
      await flushPromises()
      expect(state.selectedID.value).toBe('')
      state.selectedID.value = 'b'
      participant.value = '10010'
      state.message.value = 'Hello'
      expect(await state.submit()).toBe(true)
      const target = { modemId: 'b', profileId: 'replacement' }
      if (mode === 'message') expect(api.send).toHaveBeenCalledWith(target, '10010', 'Hello')
      else expect(dial).toHaveBeenCalledWith('10010', target)
      expect(api.lines).not.toHaveBeenCalled()
      expect(api.preference).not.toHaveBeenCalled()
      scope.stop()
    },
  )
  it('does not send after leaving the composer while refreshing the modem', async () => {
    api.getModems.mockResolvedValue({ data: ref([modem('a')]) })
    let finish: ((response: { data: ReturnType<typeof ref<Modem>> }) => void) | undefined
    api.getModem.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const scope = effectScope()
    const state = scope.run(() => useCommunicationComposer(ref('10086'), ref('message')))
    if (!state) throw new Error('missing composer')
    await flushPromises()
    state.message.value = 'Keep me'
    const pending = state.submit()
    scope.stop()
    finish?.({ data: ref(modem('a')) })
    expect(await pending).toBe(false)
    expect(api.send).not.toHaveBeenCalled()
  })
  it('keeps a failed draft and never retries a submission', async () => {
    api.getModems.mockResolvedValue({ data: ref([modem('a')]) })
    api.getModem.mockResolvedValue({ data: ref(modem('a')) })
    api.send.mockRejectedValue(new Error('SIM changed'))
    const scope = effectScope()
    const state = scope.run(() => useCommunicationComposer(ref('10086'), ref('message')))
    if (!state) throw new Error('missing composer')
    await flushPromises()
    state.message.value = 'Keep me'
    expect(await state.submit()).toBe(false)
    expect(state.message.value).toBe('Keep me')
    expect(api.send).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it.each(['message', 'call'] as const)(
    'uses only the current modem for a device contact %s',
    async (mode) => {
      api.getModem
        .mockResolvedValueOnce({ data: ref(modem('b')) })
        .mockResolvedValueOnce({ data: ref(modem('b', 'replacement')) })
      api.send.mockResolvedValue({})
      dial.mockResolvedValue(true)
      const scope = effectScope()
      const state = scope.run(() =>
        useCommunicationComposer(ref('10086'), ref(mode), ref('a'), ref('b')),
      )
      if (!state) throw new Error('missing composer')
      await flushPromises()
      expect(state.modems.value.map((entry) => entry.id)).toEqual(['b'])
      expect(state.selectedID.value).toBe('b')
      state.message.value = 'Hello'
      expect(await state.submit()).toBe(true)
      expect(api.getModem).toHaveBeenNthCalledWith(1, 'b')
      expect(api.getModem).toHaveBeenNthCalledWith(2, 'b')
      const target = { modemId: 'b', profileId: 'replacement' }
      if (mode === 'message') expect(api.send).toHaveBeenCalledWith(target, '10086', 'Hello')
      else expect(dial).toHaveBeenCalledWith('10086', target)
      expect(api.getModems).not.toHaveBeenCalled()
      expect(api.lines).not.toHaveBeenCalled()
      scope.stop()
    },
  )

  it.each(['missing', 'offline'])(
    'does not fall back to another modem when the current modem is %s',
    async (failure) => {
      api.getModems.mockResolvedValue({ data: ref([modem('a')]) })
      if (failure === 'missing') api.getModem.mockResolvedValue({ data: ref(null) })
      else api.getModem.mockRejectedValue(new Error('Offline'))
      const scope = effectScope()
      const state = scope.run(() =>
        useCommunicationComposer(ref('10086'), ref('message'), ref(''), ref('removed')),
      )
      if (!state) throw new Error('missing composer')
      await flushPromises()
      state.message.value = 'Keep me'
      state.resetSelection()
      expect(state.selectedID.value).toBe('')
      expect(state.canSubmit.value).toBe(false)
      expect(await state.submit()).toBe(false)
      expect(state.error.value).not.toBe('')
      expect(api.getModems).not.toHaveBeenCalled()
      expect(api.send).not.toHaveBeenCalled()
      expect(dial).not.toHaveBeenCalled()
      scope.stop()
    },
  )

  it('discards a pending submission when the modem context changes', async () => {
    api.getModem.mockResolvedValue({ data: ref(modem('a')) })
    const fixedID = ref('a')
    const scope = effectScope()
    const state = scope.run(() =>
      useCommunicationComposer(ref('10086'), ref('message'), ref(''), fixedID),
    )
    if (!state) throw new Error('missing composer')
    await flushPromises()
    let finish: ((response: { data: ReturnType<typeof ref<Modem>> }) => void) | undefined
    api.getModem.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    state.message.value = 'Do not send'
    const pending = state.submit()
    api.getModem.mockResolvedValue({ data: ref(modem('b')) })
    fixedID.value = 'b'
    await flushPromises()
    finish?.({ data: ref(modem('a')) })
    expect(await pending).toBe(false)
    expect(state.selectedID.value).toBe('b')
    expect(state.message.value).toBe('')
    expect(api.send).not.toHaveBeenCalled()
    scope.stop()
  })
})

describe('backend contact cache', () => {
  it('loads an empty cache even when the clock starts at the Unix epoch', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(0)
    try {
      api.contacts.mockResolvedValue({ items: [contact('a')] })
      const state = useContacts()
      await state.load()
      await state.load()
      expect(state.contacts.value[0]?.id).toBe('a')
      expect(api.contacts).toHaveBeenCalledTimes(1)
    } finally {
      now.mockRestore()
    }
  })

  it('coalesces readers and refreshes explicitly after a write', async () => {
    api.contacts.mockResolvedValue({ items: [contact('a')] })
    const first = useContacts(),
      second = useContacts()
    await Promise.all([first.load(), second.load()])
    await first.load()
    expect(api.contacts).toHaveBeenCalledTimes(1)
    api.contacts.mockResolvedValue({ items: [contact('b')] })
    await second.load(true)
    expect(first.contacts.value[0]?.id).toBe('b')
    expect(api.contacts).toHaveBeenCalledTimes(2)
  })
  it('refuses ambiguous matching and never suffix-matches numbers', async () => {
    api.contacts.mockResolvedValue({ items: [contact('a'), contact('b')] })
    const state = useContacts()
    await state.load()
    expect(state.match('+14155552671')).toBeUndefined()
    expect(state.match('5552671')).toBeUndefined()
  })
  it('discards late responses after logout', async () => {
    let finish: ((value: { items: Contact[] }) => void) | undefined
    api.contacts.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const state = useContacts()
    const pending = state.load()
    clearContacts()
    finish?.({ items: [contact('old')] })
    await pending
    expect(state.contacts.value).toEqual([])
  })
})

describe('contact drafts', () => {
  it('copies a reactive entry so cancel never changes the saved contact', () => {
    const original = reactive(contact('a'))
    const editor = useContactEditor(original)
    editor.contact.value.name = 'Changed'
    editor.contact.value.numbers.splice(0, 1)
    expect(original.name).toBe('a')
    expect(original.numbers).toHaveLength(1)
    expect(api.saveContact).not.toHaveBeenCalled()
  })
  it('retains the saved ID when an avatar upload fails, making retry an update', async () => {
    api.saveContact.mockResolvedValue(contact('saved'))
    api.saveAvatar
      .mockRejectedValueOnce(new Error('Upload interrupted'))
      .mockResolvedValueOnce(contact('saved'))
    const editor = useContactEditor({ ...contact(''), name: 'Alice' })
    editor.avatar.value = new Blob(['image'], { type: 'image/png' })
    expect(await editor.save()).toBeUndefined()
    expect(editor.contact.value.id).toBe('saved')
    expect(await editor.save()).toEqual(contact('saved'))
    expect(api.saveContact.mock.calls[1]?.[0].id).toBe('saved')
  })
})

import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemCallsView from '@/views/ModemCallsView.vue'
import type { CallRecord } from '@/types/call'

vi.mock('@/apis/communication', () => ({
  communicationApi: { contacts: vi.fn().mockResolvedValue({ items: [] }) },
}))

const phoneHarness = vi.hoisted(() => ({
  recentCalls: [] as CallRecord[],
  activeCall: null as CallRecord | null,
  incomingCall: null as CallRecord | null,
  isLoading: false,
  isDialing: false,
  isDeletingCallID: '',
  errorMessage: '',
  dial: vi.fn(),
  answer: vi.fn(),
  reject: vi.fn(),
  hangup: vi.fn(),
  deleteRecord: vi.fn(),
  loadCalls: vi.fn(),
  setSearchQuery: vi.fn(),
  sessionSearchQuery: null as { value: string } | null,
}))

vi.mock('@/composables/useDeviceCallHistory', () => ({
  useDeviceCallHistory: () => ({
    recentCalls: computed(() => phoneHarness.recentCalls),
    hasRecentCalls: computed(() => phoneHarness.recentCalls.length > 0),
    isLoading: computed(() => phoneHarness.isLoading),
    isDeletingCallID: computed(() => phoneHarness.isDeletingCallID),
    errorMessage: computed(() => phoneHarness.errorMessage),
    deleteRecord: phoneHarness.deleteRecord,
    loadCalls: phoneHarness.loadCalls,
    setSearchQuery: phoneHarness.setSearchQuery,
  }),
}))

const ussdHarness = vi.hoisted(() => ({
  executeUssd: vi.fn(),
}))

const modemApiHarness = vi.hoisted(() => ({
  getWiFiCallingSettings: vi.fn(),
  getModem: vi.fn(),
}))

const volteApiHarness = vi.hoisted(() => ({
  settings: vi.fn(),
}))

const capabilityHarness = vi.hoisted(() => ({
  volte: true,
}))

const callAudioHarness = vi.hoisted(() => ({
  errorMessage: { value: '' },
  deviceNotice: { value: '' },
  prepare: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  setInputEnabled: vi.fn(),
}))

const datetimeHarness = vi.hoisted(() => ({
  formatListTimestamp: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'modem-1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

const labels: Record<string, string> = {
  'modemDetail.calls.openDialpad': 'Open dialpad',
  'modemDetail.calls.dialpad': 'Dialpad',
  'modemDetail.calls.dialpadDescription': 'Enter a phone number or USSD code.',
  'modemDetail.calls.numberPlaceholder': 'Number',
  'modemDetail.calls.call': 'Call',
  'modemDetail.calls.callBack': 'Call back',
  'modemDetail.calls.backspace': 'Delete digit',
  'modemDetail.calls.incoming': 'Incoming',
  'modemDetail.calls.outgoing': 'Outgoing',
  'modemDetail.calls.duration': 'Duration',
  'modemDetail.calls.durationEmpty': '0:00',
  'modemDetail.calls.deleteRecord': 'Delete record',
  'modemDetail.calls.deleteTitle': 'Delete this call record?',
  'modemDetail.calls.deleteDescription':
    'This only removes the local call record. It does not affect carrier billing or an active call.',
  'modemDetail.calls.details.direction': 'Direction',
  'modemDetail.calls.details.state': 'State',
  'modemDetail.calls.details.route': 'Route',
  'modemDetail.calls.details.duration': 'Duration',
  'modemDetail.calls.details.startedAt': 'Started',
  'modemDetail.calls.details.answeredAt': 'Answered',
  'modemDetail.calls.details.endedAt': 'Ended',
  'modemDetail.calls.details.reason': 'Reason',
  'modemDetail.calls.details.notAnswered': 'Not answered',
  'modemDetail.calls.details.inProgress': 'In progress',
  'modemDetail.calls.ussdTitle': 'USSD',
  'modemDetail.calls.ussdDescription': 'Continue the USSD session in this dialog.',
  'modemDetail.calls.ussdPlaceholder': 'Reply',
  'modemDetail.calls.states.dialing': 'Dialing',
  'modemDetail.calls.states.ringing': 'Ringing',
  'modemDetail.calls.states.answering': 'Answering',
  'modemDetail.calls.states.earlyMedia': 'Early media',
  'modemDetail.calls.states.active': 'In call',
  'modemDetail.calls.states.confirmed': 'Connected',
  'modemDetail.calls.states.ended': 'Ended',
  'modemDetail.calls.states.failed': 'Failed',
  'modemDetail.calls.holdStates.local': 'On hold',
  'modemDetail.calls.holdStates.remote': 'Remote hold',
  'modemDetail.calls.holdStates.localRemote': 'Both on hold',
  'modemDetail.calls.title': 'Phone',
  'modemDetail.calls.subtitle': 'Recent calls for this modem.',
  'modemDetail.calls.searchPlaceholder': 'Search calls',
  'modemDetail.calls.clearSearch': 'Clear search',
  'modemDetail.calls.empty': 'No recent calls.',
  'modemDetail.calls.noSearchResults': 'No calls match your search.',
  'modemDetail.calls.answer': 'Answer',
  'modemDetail.calls.reject': 'Reject',
  'modemDetail.calls.hold': 'Hold',
  'modemDetail.calls.resume': 'Resume',
  'modemDetail.back': 'Back',
  'modemDetail.actions.cancel': 'Cancel',
  'modemDetail.actions.delete': 'Delete',
  'modemDetail.ussd.send': 'Send',
  'home.refresh': 'Refresh',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => labels[key] ?? key,
  }),
}))

vi.mock('@/apis/ussd', () => ({
  useUssdApi: () => ussdHarness,
}))

vi.mock('@/apis/modem', () => ({
  useModemApi: () => modemApiHarness,
}))

vi.mock('@/apis/volte', () => ({
  useVoLTEApi: () => volteApiHarness,
}))

vi.mock('@/composables/useCapabilities', () => ({
  FEATURE: { volte: 'volte' },
  useCapabilities: () => ({
    hasFeature: (feature: string) => feature === 'volte' && capabilityHarness.volte,
  }),
}))

vi.mock('@/composables/useCalls', () => ({
  useCalls: (_modemId: unknown, _country: unknown, searchQuery: { value: string }) => {
    phoneHarness.sessionSearchQuery = searchQuery
    return {
      recentCalls: computed(() => phoneHarness.recentCalls),
      hasRecentCalls: computed(() => phoneHarness.recentCalls.length > 0),
      activeCall: computed(() => phoneHarness.activeCall),
      incomingCall: computed(() => phoneHarness.incomingCall),
      isLoading: computed(() => phoneHarness.isLoading),
      isDialing: computed(() => phoneHarness.isDialing),
      isDeletingCallID: computed(() => phoneHarness.isDeletingCallID),
      errorMessage: computed(() => phoneHarness.errorMessage),
      terminalStates: new Set(['ended', 'failed']),
      dial: phoneHarness.dial,
      answer: phoneHarness.answer,
      reject: phoneHarness.reject,
      hangup: phoneHarness.hangup,
      hold: vi.fn(),
      resume: vi.fn(),
      toggleHold: vi.fn(),
      deleteRecord: phoneHarness.deleteRecord,
      loadCalls: phoneHarness.loadCalls,
    }
  },
}))

vi.mock('@/composables/useCallAudioSession', () => ({
  useCallAudioSession: () => callAudioHarness,
}))

vi.mock('@/lib/datetime', () => ({
  formatListTimestamp: datetimeHarness.formatListTimestamp,
}))

vi.mock('lucide-vue-next', () => ({
  Delete: { template: '<span />' },
  Mic: { template: '<span />' },
  MessageSquare: { template: '<span />' },
  Phone: { template: '<span />' },
  PhoneCall: { template: '<span />' },
  PhoneIncoming: { template: '<span />' },
  PhoneOff: { template: '<span />' },
  PhoneOutgoing: { template: '<span />' },
  RefreshCw: { template: '<span />' },
  Search: { template: '<span />' },
  Trash2: { template: '<span />' },
  X: { template: '<span />' },
}))

const passthrough = { template: '<div><slot /></div>' }

const mountView = () =>
  mount(ModemCallsView, {
    global: {
      stubs: {
        AlertDialog: {
          props: ['open'],
          template: '<div v-if="open"><slot /></div>',
        },
        AlertDialogCancel: {
          props: ['disabled'],
          emits: ['click'],
          template:
            '<button type="button" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
        },
        AlertDialogContent: passthrough,
        AlertDialogDescription: { template: '<p><slot /></p>' },
        AlertDialogFooter: passthrough,
        AlertDialogHeader: passthrough,
        AlertDialogTitle: { template: '<h2><slot /></h2>' },
        DraggableFab: {
          emits: ['click'],
          template:
            '<button type="button" aria-label="Open dialpad" @click="$emit(\'click\')"><slot /></button>',
        },
        BackButton: {
          props: ['label'],
          template: '<a><slot />{{ label }}</a>',
        },
        ModemStickyTopBar: {
          props: ['title', 'show'],
          template:
            '<div data-testid="sticky-top-bar" :data-show="show"><span>{{ title }}</span><slot name="right" /></div>',
        },
        Button: {
          props: ['disabled'],
          emits: ['click'],
          template:
            '<button type="button" v-bind="$attrs" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
        },
        Dialog: {
          props: ['open'],
          template: '<div v-if="open"><slot /></div>',
        },
        DialogContent: passthrough,
        DialogDescription: { template: '<p><slot /></p>' },
        DialogHeader: passthrough,
        DialogTitle: { template: '<h2><slot /></h2>' },
        Spinner: { template: '<span />' },
      },
    },
  })

const clickKey = async (wrapper: ReturnType<typeof mountView>, key: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().trim().startsWith(key))
  expect(button, `dial key ${key}`).toBeDefined()
  await button?.trigger('click')
}

const callButton = (wrapper: ReturnType<typeof mountView>) =>
  wrapper.findAll('button').find((item) => item.attributes('aria-label') === 'Call')

const deferredCall = () => {
  let resolve!: (call: CallRecord | null) => void
  const promise = new Promise<CallRecord | null>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

describe('ModemCallsView phone interactions', () => {
  beforeEach(() => {
    capabilityHarness.volte = true
    phoneHarness.recentCalls = []
    phoneHarness.activeCall = null
    phoneHarness.incomingCall = null
    phoneHarness.isLoading = false
    phoneHarness.isDialing = false
    phoneHarness.isDeletingCallID = ''
    phoneHarness.errorMessage = ''
    phoneHarness.dial.mockReset()
    phoneHarness.dial.mockResolvedValue(null)
    phoneHarness.answer.mockReset()
    phoneHarness.reject.mockReset()
    phoneHarness.hangup.mockReset()
    phoneHarness.deleteRecord.mockReset()
    phoneHarness.deleteRecord.mockResolvedValue(true)
    phoneHarness.loadCalls.mockReset()
    phoneHarness.loadCalls.mockResolvedValue(undefined)
    phoneHarness.setSearchQuery.mockReset()
    phoneHarness.sessionSearchQuery = null
    callAudioHarness.errorMessage.value = ''
    callAudioHarness.deviceNotice.value = ''
    callAudioHarness.prepare.mockReset()
    callAudioHarness.prepare.mockResolvedValue(true)
    callAudioHarness.start.mockReset()
    callAudioHarness.stop.mockReset()
    datetimeHarness.formatListTimestamp.mockReset()
    datetimeHarness.formatListTimestamp.mockImplementation((value: string) => `short ${value}`)
    modemApiHarness.getWiFiCallingSettings.mockReset()
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({
        enabled: true,
        connected: false,
        state: 'disconnected',
      }),
    })
    modemApiHarness.getModem.mockReset()
    modemApiHarness.getModem.mockResolvedValue({
      data: ref({ sim: { regionCode: 'US' } }),
    })
    volteApiHarness.settings.mockReset()
    volteApiHarness.settings.mockResolvedValue({
      data: ref({
        enabled: false,
      }),
    })
    ussdHarness.executeUssd.mockReset()
    ussdHarness.executeUssd.mockResolvedValue({
      data: ref({ reply: 'Balance: 1' }),
    })
  })

  it('routes star-prefixed input to the USSD dialog and sends it immediately', async () => {
    const wrapper = mountView()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '*')
    await clickKey(wrapper, '1')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(phoneHarness.dial).not.toHaveBeenCalled()
    expect(ussdHarness.executeUssd).toHaveBeenCalledWith('modem-1', 'initialize', '*1')
    expect(wrapper.text()).toContain('USSD')
    expect(wrapper.text()).toContain('Balance: 1')
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('')

    await wrapper
      .findAll('button')
      .find((item) => item.text() === 'Cancel')
      ?.trigger('click')
    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')

    expect(wrapper.get<HTMLInputElement>('input[type="tel"]').element.value).toBe('')
  })

  it('dials a number entered directly in the phone input', async () => {
    const wrapper = mountView()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await wrapper.get('input[type="tel"]').setValue('+12242255559')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(phoneHarness.dial).toHaveBeenCalledWith('+12242255559', 'auto', expect.any(String))
  })

  it('renders search and uses a search empty state', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountView()

      const input = wrapper.get('input[role="searchbox"]')
      expect(input.attributes('aria-label')).toBe('Search calls')
      expect(wrapper.text()).toContain('No recent calls.')

      await input.setValue('224')
      await vi.advanceTimersByTimeAsync(250)
      await flushPromises()

      expect(phoneHarness.setSearchQuery).toHaveBeenLastCalledWith('224')
      expect(wrapper.text()).toContain('No calls match your search.')
    } finally {
      vi.useRealTimers()
    }
  })

  it('shrinks the phone input text for long numbers', async () => {
    const wrapper = mountView()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    const input = wrapper.get('input[type="tel"]')

    expect(input.classes()).toContain('text-3xl')

    await input.setValue('+12242255559')
    expect(input.classes()).toContain('text-2xl')

    await input.setValue('+122422555591234')
    expect(input.classes()).toContain('text-xl')

    await input.setValue('+122422555591234567890')
    expect(input.classes()).toContain('text-lg')
  })

  it('keeps the USSD dialog dismissible after a request error', async () => {
    ussdHarness.executeUssd.mockRejectedValueOnce(new Error('network timeout'))
    const wrapper = mountView()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '*')
    await clickKey(wrapper, '1')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('USSD')
    expect(wrapper.text()).not.toContain('Balance: 1')

    await wrapper
      .findAll('button')
      .find((item) => item.text() === 'Cancel')
      ?.trigger('click')

    expect(wrapper.find('input[placeholder="Reply"]').exists()).toBe(false)
  })

  it('renders call details without direction text and calls back from recent records', async () => {
    phoneHarness.recentCalls = [
      {
        callID: 'call-out',
        route: 'wifi_calling',
        direction: 'outgoing',
        number: '+12242255559',
        state: 'ended',
        hold: 'none',
        reason: '',
        startedAt: '2026-05-27T00:00:00Z',
        answeredAt: '2026-05-27T00:00:10Z',
        endedAt: '2026-05-27T00:01:15Z',
        updatedAt: '2026-05-27T00:01:15Z',
      },
      {
        callID: 'call-in',
        route: 'wifi_calling',
        direction: 'incoming',
        number: '+12242255558',
        state: 'ended',
        hold: 'none',
        reason: '',
        startedAt: '2026-05-27T00:02:00Z',
        answeredAt: '',
        endedAt: '2026-05-27T00:02:09Z',
        updatedAt: '2026-05-27T00:02:09Z',
      },
    ]
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Outgoing')
    expect(wrapper.text()).not.toContain('Incoming')
    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('short 2026-05-27T00:01:15Z')
    expect(datetimeHarness.formatListTimestamp).toHaveBeenCalledWith('2026-05-27T00:01:15Z')
    expect(wrapper.text()).toContain('1:05')
    expect(wrapper.text()).not.toContain('0:09')
    expect(wrapper.text()).not.toContain('0:00')

    await wrapper
      .findAll('button')
      .find((item) => item.attributes('aria-label') === 'Call back')
      ?.trigger('click')
    await flushPromises()

    expect(phoneHarness.dial).toHaveBeenCalledWith('+12242255559', 'auto', expect.any(String))
  })

  it('shows immediate loading feedback when calling back from recent records', async () => {
    phoneHarness.recentCalls = [
      {
        callID: 'call-out',
        route: 'wifi_calling',
        direction: 'outgoing',
        number: '+12242255559',
        state: 'ended',
        hold: 'none',
        reason: '',
        startedAt: '2026-05-27T00:00:00Z',
        answeredAt: '2026-05-27T00:00:10Z',
        endedAt: '2026-05-27T00:01:15Z',
        updatedAt: '2026-05-27T00:01:15Z',
      },
    ]
    const pendingDial = deferredCall()
    phoneHarness.dial.mockReturnValueOnce(pendingDial.promise)
    const wrapper = mountView()
    await flushPromises()
    const button = wrapper
      .findAll('button')
      .find((item) => item.attributes('aria-label') === 'Call back')

    await button?.trigger('click')

    expect(phoneHarness.dial).toHaveBeenCalledWith('+12242255559', 'auto', expect.any(String))
    expect(button?.attributes('aria-busy')).toBe('true')
    expect(button?.attributes('disabled')).toBeDefined()

    pendingDial.resolve(null)
    await flushPromises()

    expect(button?.attributes('aria-busy')).toBe('false')
    expect(button?.attributes('disabled')).toBeUndefined()
  })

  it('does not copy recent record callbacks into the dialpad input', async () => {
    phoneHarness.recentCalls = [
      {
        callID: 'call-out',
        route: 'wifi_calling',
        direction: 'outgoing',
        number: '+12242255559',
        state: 'ended',
        hold: 'none',
        reason: '',
        startedAt: '2026-05-27T00:00:00Z',
        answeredAt: '2026-05-27T00:00:10Z',
        endedAt: '2026-05-27T00:01:15Z',
        updatedAt: '2026-05-27T00:01:15Z',
      },
    ]
    phoneHarness.dial.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((item) => item.attributes('aria-label') === 'Call back')
      ?.trigger('click')
    await flushPromises()
    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')

    expect((wrapper.get('input[aria-label="Number"]').element as HTMLInputElement).value).toBe('')
  })

  it('expands a terminal call record and confirms deletion', async () => {
    phoneHarness.recentCalls = [
      {
        callID: 'call-out',
        route: 'wifi_calling',
        direction: 'outgoing',
        number: '+12242255559',
        state: 'ended',
        hold: 'none',
        reason: 'remote bye',
        startedAt: '2026-05-27T00:00:00Z',
        answeredAt: '2026-05-27T00:00:10Z',
        endedAt: '2026-05-27T00:01:15Z',
        updatedAt: '2026-05-27T00:01:15Z',
      },
    ]
    const wrapper = mountView()

    await wrapper.get('[role="button"][aria-expanded="false"]').trigger('click')

    expect(wrapper.text()).toContain('Direction')
    expect(wrapper.text()).toContain('Outgoing')
    expect(wrapper.text()).toContain('remote bye')
    expect(wrapper.text()).not.toContain('call-out')

    await wrapper
      .findAll('button')
      .find((item) => item.text() === 'Delete record')
      ?.trigger('click')

    expect(wrapper.text()).toContain('Delete this call record?')

    await wrapper
      .findAll('button')
      .find((item) => item.text() === 'Delete')
      ?.trigger('click')
    await flushPromises()

    expect(phoneHarness.deleteRecord).toHaveBeenCalledWith(phoneHarness.recentCalls[0])
  })

  it('prepares WebRTC audio from the outgoing dial user gesture when Wi-Fi Calling is connected', async () => {
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({
        enabled: true,
        connected: true,
        state: 'connected',
      }),
    })
    phoneHarness.dial.mockResolvedValue({
      callID: 'call-1',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '12',
      state: 'dialing',
      hold: 'none',
      reason: '',
      startedAt: '2026-05-27T00:00:00Z',
      answeredAt: '',
      endedAt: '',
      updatedAt: '2026-05-27T00:00:00Z',
    } satisfies CallRecord)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12', 'auto', expect.any(String))
  })

  it('starts dialing even when browser audio preparation fails', async () => {
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({
        enabled: true,
        connected: true,
        state: 'connected',
      }),
    })
    callAudioHarness.prepare.mockResolvedValue(false)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12', 'auto', expect.any(String))
  })

  it('does not prepare outgoing audio when Wi-Fi Calling is disconnected', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).not.toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12', 'auto', expect.any(String))
  })

  it('prepares outgoing audio when VoLTE is enabled', async () => {
    volteApiHarness.settings.mockResolvedValue({
      data: ref({
        enabled: true,
      }),
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12', 'auto', expect.any(String))
  })

  it('does not request VoLTE without the Pro capability', async () => {
    capabilityHarness.volte = false

    mountView()
    await flushPromises()

    expect(volteApiHarness.settings).not.toHaveBeenCalled()
  })

  it('hides the dialpad as soon as dialing starts', async () => {
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({
        enabled: true,
        connected: true,
        state: 'connected',
      }),
    })
    let resolvePrepare!: (ready: boolean) => void
    const pendingDial = deferredCall()
    callAudioHarness.prepare.mockReturnValueOnce(
      new Promise<boolean>((resolve) => {
        resolvePrepare = resolve
      }),
    )
    phoneHarness.dial.mockReturnValueOnce(pendingDial.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    expect(wrapper.findAll('input[type="tel"]')).toHaveLength(2)

    await callButton(wrapper)?.trigger('click')

    expect(callAudioHarness.prepare).toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12', 'auto', expect.any(String))
    expect(wrapper.findAll('input[type="tel"]')).toHaveLength(1)
    resolvePrepare(true)
    pendingDial.resolve(null)
    await flushPromises()
  })

  it('releases prepared outgoing audio when dialing does not create a call', async () => {
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({
        enabled: true,
        connected: true,
        state: 'connected',
      }),
    })
    phoneHarness.dial.mockResolvedValue(null)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).toHaveBeenCalled()
    expect(callAudioHarness.stop).toHaveBeenCalled()
  })

  it('starts WebRTC audio for a confirmed Wi-Fi Calling session', () => {
    phoneHarness.activeCall = {
      callID: 'call-confirmed',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '+12242255559',
      state: 'confirmed',
      hold: 'none',
      reason: '',
      startedAt: '2026-05-27T00:00:00Z',
      answeredAt: '',
      endedAt: '',
      updatedAt: '2026-05-27T00:00:05Z',
    }

    mountView()

    expect(callAudioHarness.start).toHaveBeenCalledWith('call-confirmed')
  })

  it('starts WebRTC audio for an early media Wi-Fi Calling session', () => {
    phoneHarness.activeCall = {
      callID: 'call-early',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '+12242255559',
      state: 'early_media',
      hold: 'none',
      reason: '',
      startedAt: '2026-05-27T00:00:00Z',
      answeredAt: '',
      endedAt: '',
      updatedAt: '2026-05-27T00:00:05Z',
    }

    mountView()

    expect(callAudioHarness.start).toHaveBeenCalledWith('call-early')
  })
})

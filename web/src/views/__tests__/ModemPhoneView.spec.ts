import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemPhoneView from '@/views/ModemPhoneView.vue'
import type { CallRecord } from '@/types/call'

const phoneHarness = vi.hoisted(() => ({
  recentCalls: [] as CallRecord[],
  activeCall: null as CallRecord | null,
  incomingCall: null as CallRecord | null,
  isLoading: false,
  isDialing: false,
  errorMessage: '',
  dial: vi.fn(),
  answer: vi.fn(),
  reject: vi.fn(),
  hangup: vi.fn(),
  loadCalls: vi.fn(),
}))

const ussdHarness = vi.hoisted(() => ({
  executeUssd: vi.fn(),
}))

const modemApiHarness = vi.hoisted(() => ({
  getWiFiCallingSettings: vi.fn(),
}))

const callAudioHarness = vi.hoisted(() => ({
  errorMessage: { value: '' },
  prepare: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
}))

const browserCodecHarness = vi.hoisted(() => ({
  hasCodec: false,
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'modem-1' } }),
}))

const labels: Record<string, string> = {
  'modemDetail.phone.openDialpad': 'Open dialpad',
  'modemDetail.phone.dialpad': 'Dialpad',
  'modemDetail.phone.dialpadDescription': 'Enter a phone number or USSD code.',
  'modemDetail.phone.numberPlaceholder': 'Number',
  'modemDetail.phone.call': 'Call',
  'modemDetail.phone.callBack': 'Call back',
  'modemDetail.phone.backspace': 'Delete digit',
  'modemDetail.phone.incoming': 'Incoming',
  'modemDetail.phone.outgoing': 'Outgoing',
  'modemDetail.phone.duration': 'Duration',
  'modemDetail.phone.durationEmpty': '0:00',
  'modemDetail.phone.ussdTitle': 'USSD',
  'modemDetail.phone.ussdDescription': 'Continue the USSD session in this dialog.',
  'modemDetail.phone.ussdPlaceholder': 'Reply',
  'modemDetail.phone.audioCodecUnavailable': 'Call audio requires an AMR/AMR-WB codec module.',
  'modemDetail.phone.states.dialing': 'Dialing',
  'modemDetail.phone.states.ringing': 'Ringing',
  'modemDetail.phone.states.answering': 'Answering',
  'modemDetail.phone.states.earlyMedia': 'Early media',
  'modemDetail.phone.states.active': 'In call',
  'modemDetail.phone.states.confirmed': 'Connected',
  'modemDetail.phone.states.ended': 'Ended',
  'modemDetail.phone.states.failed': 'Failed',
  'modemDetail.phone.title': 'Phone',
  'modemDetail.phone.subtitle': 'Recent calls for this modem.',
  'modemDetail.phone.empty': 'No recent calls.',
  'modemDetail.phone.answer': 'Answer',
  'modemDetail.phone.reject': 'Reject',
  'modemDetail.back': 'Back',
  'modemDetail.actions.cancel': 'Cancel',
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

vi.mock('@/composables/usePhoneCalls', () => ({
  usePhoneCalls: () => ({
    recentCalls: computed(() => phoneHarness.recentCalls),
    hasRecentCalls: computed(() => phoneHarness.recentCalls.length > 0),
    activeCall: computed(() => phoneHarness.activeCall),
    incomingCall: computed(() => phoneHarness.incomingCall),
    isLoading: computed(() => phoneHarness.isLoading),
    isDialing: computed(() => phoneHarness.isDialing),
    errorMessage: computed(() => phoneHarness.errorMessage),
    dial: phoneHarness.dial,
    answer: phoneHarness.answer,
    reject: phoneHarness.reject,
    hangup: phoneHarness.hangup,
    loadCalls: phoneHarness.loadCalls,
  }),
}))

vi.mock('@/composables/useCallAudioSession', () => ({
  useCallAudioSession: () => callAudioHarness,
}))

vi.mock('@/lib/browserAmrCodec', () => ({
  createBrowserAmrCodec: vi.fn(),
  hasBrowserAmrCodec: () => browserCodecHarness.hasCodec,
}))

vi.mock('lucide-vue-next', () => ({
  Delete: { template: '<span />' },
  Mic: { template: '<span />' },
  Phone: { template: '<span />' },
  PhoneCall: { template: '<span />' },
  PhoneIncoming: { template: '<span />' },
  PhoneOff: { template: '<span />' },
  PhoneOutgoing: { template: '<span />' },
  RefreshCw: { template: '<span />' },
}))

const passthrough = { template: '<div><slot /></div>' }

const mountView = () =>
  mount(ModemPhoneView, {
    global: {
      stubs: {
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

describe('ModemPhoneView phone interactions', () => {
  beforeEach(() => {
    phoneHarness.recentCalls = []
    phoneHarness.activeCall = null
    phoneHarness.incomingCall = null
    phoneHarness.isLoading = false
    phoneHarness.isDialing = false
    phoneHarness.errorMessage = ''
    phoneHarness.dial.mockReset()
    phoneHarness.dial.mockResolvedValue(null)
    phoneHarness.answer.mockReset()
    phoneHarness.reject.mockReset()
    phoneHarness.hangup.mockReset()
    phoneHarness.loadCalls.mockReset()
    phoneHarness.loadCalls.mockResolvedValue(undefined)
    callAudioHarness.errorMessage.value = ''
    callAudioHarness.prepare.mockReset()
    callAudioHarness.prepare.mockResolvedValue(true)
    callAudioHarness.start.mockReset()
    callAudioHarness.stop.mockReset()
    browserCodecHarness.hasCodec = false
    modemApiHarness.getWiFiCallingSettings.mockReset()
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({ enabled: true, preferred: true, connected: false, state: 'disconnected' }),
    })
    ussdHarness.executeUssd.mockReset()
    ussdHarness.executeUssd.mockResolvedValue({ data: ref({ reply: 'Balance: 1' }) })
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
    expect(wrapper.find('input').element.value).toBe('')

    await wrapper
      .findAll('button')
      .find((item) => item.text() === 'Cancel')
      ?.trigger('click')
    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')

    expect(wrapper.text()).toContain('Number')
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

    expect(wrapper.text()).not.toContain('USSD')
  })

  it('renders call direction, duration, and calls back from recent records', async () => {
    phoneHarness.recentCalls = [
      {
        callID: 'call-out',
        route: 'wifi_calling',
        direction: 'outgoing',
        number: '+12242255559',
        state: 'ended',
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
        reason: '',
        startedAt: '2026-05-27T00:02:00Z',
        answeredAt: '',
        endedAt: '2026-05-27T00:02:09Z',
        updatedAt: '2026-05-27T00:02:09Z',
      },
    ]
    const wrapper = mountView()

    expect(wrapper.text()).toContain('Outgoing')
    expect(wrapper.text()).toContain('Incoming')
    expect(wrapper.text()).toContain('1:05')
    expect(wrapper.text()).not.toContain('0:09')
    expect(wrapper.text()).not.toContain('0:00')

    await wrapper
      .findAll('button')
      .find((item) => item.attributes('aria-label') === 'Call back')
      ?.trigger('click')
    await flushPromises()

    expect(phoneHarness.dial).toHaveBeenCalledWith('+12242255559')
  })

  it('prepares browser audio from the outgoing dial user gesture when a codec is available', async () => {
    browserCodecHarness.hasCodec = true
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({ enabled: true, preferred: true, connected: true, state: 'connected' }),
    })
    phoneHarness.dial.mockResolvedValue({
      callID: 'call-1',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '12',
      state: 'dialing',
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
    expect(phoneHarness.dial).toHaveBeenCalledWith('12')
  })

  it('does not dial when browser audio preparation fails', async () => {
    browserCodecHarness.hasCodec = true
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({ enabled: true, preferred: true, connected: true, state: 'connected' }),
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
    expect(phoneHarness.dial).not.toHaveBeenCalled()
  })

  it('does not prepare outgoing audio when Wi-Fi Calling is disconnected', async () => {
    browserCodecHarness.hasCodec = true
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    await callButton(wrapper)?.trigger('click')
    await flushPromises()

    expect(callAudioHarness.prepare).not.toHaveBeenCalled()
    expect(phoneHarness.dial).toHaveBeenCalledWith('12')
  })

  it('hides the dialpad as soon as dialing starts', async () => {
    const pendingDial = deferredCall()
    phoneHarness.dial.mockReturnValueOnce(pendingDial.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[aria-label="Open dialpad"]').trigger('click')
    await clickKey(wrapper, '1')
    await clickKey(wrapper, '2')
    expect(wrapper.text()).toContain('Dialpad')

    await callButton(wrapper)?.trigger('click')

    expect(phoneHarness.dial).toHaveBeenCalledWith('12')
    expect(wrapper.text()).not.toContain('Dialpad')
    pendingDial.resolve(null)
    await flushPromises()
  })

  it('releases prepared outgoing audio when dialing does not create a call', async () => {
    browserCodecHarness.hasCodec = true
    modemApiHarness.getWiFiCallingSettings.mockResolvedValue({
      data: ref({ enabled: true, preferred: true, connected: true, state: 'connected' }),
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

  it('starts browser audio for a confirmed Wi-Fi Calling session', () => {
    browserCodecHarness.hasCodec = true
    phoneHarness.activeCall = {
      callID: 'call-confirmed',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '+12242255559',
      state: 'confirmed',
      reason: '',
      startedAt: '2026-05-27T00:00:00Z',
      answeredAt: '',
      endedAt: '',
      updatedAt: '2026-05-27T00:00:05Z',
    }

    mountView()

    expect(callAudioHarness.start).toHaveBeenCalledWith('call-confirmed')
  })

  it('starts browser audio for an early media Wi-Fi Calling session', () => {
    browserCodecHarness.hasCodec = true
    phoneHarness.activeCall = {
      callID: 'call-early',
      route: 'wifi_calling',
      direction: 'outgoing',
      number: '+12242255559',
      state: 'early_media',
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

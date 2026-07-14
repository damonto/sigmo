import { mount } from '@vue/test-utils'
import { computed, ref, type Ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemCallBanner from '@/components/modem/ModemCallBanner.vue'
import type { ModemCallSession } from '@/composables/useModemCallSession'
import type { CallRecord } from '@/types/call'

const call = (patch: Partial<CallRecord> = {}): CallRecord => ({
  callID: 'call-1',
  route: 'wifi_calling',
  direction: 'incoming',
  number: '+12242255559',
  state: 'ringing',
  hold: 'none',
  reason: '',
  startedAt: '2026-05-27T00:00:00Z',
  answeredAt: '',
  endedAt: '',
  updatedAt: '2026-05-27T00:00:00Z',
  ...patch,
})

const makeSession = (state: {
  incomingCall?: CallRecord | null
  activeCall?: CallRecord | null
  duration?: string
  audioMessage?: string
}) =>
  ({
    incomingCall: ref(state.incomingCall ?? null) as Ref<CallRecord | null>,
    activeCall: ref(state.activeCall ?? null) as Ref<CallRecord | null>,
    activeCallDurationLabel: computed(() => state.duration ?? ''),
    audioMessage: computed(() => state.audioMessage ?? ''),
    terminalStates: new Set<CallRecord['state']>(['ended', 'failed']),
    primaryLine: (item: CallRecord) => (item.number ? '(224) 225-5559' : 'Unknown number'),
    routeLabel: () => 'Wi-Fi Calling',
    stateLabel: (value: string) => (value === 'answering' ? 'Answering' : 'In call'),
    holdLabel: (value: string) => (value === 'local' ? 'On hold' : ''),
    isLocallyHeld: (item: CallRecord | null) =>
      item?.hold === 'local' || item?.hold === 'local_remote',
    isRemotelyHeld: (item: CallRecord | null) =>
      item?.hold === 'remote' || item?.hold === 'local_remote',
    usesBrowserAudio: (item: CallRecord | null) =>
      item?.route === 'wifi_calling' || item?.route === 'volte',
    answerIncoming: vi.fn(),
    reject: vi.fn(),
    hangup: vi.fn(),
    toggleHold: vi.fn(),
    sendDTMF: vi.fn(),
  }) as unknown as ModemCallSession

const mountBanner = (session: ModemCallSession) =>
  mount(ModemCallBanner, {
    props: { session },
    global: {
      mocks: {
        $t: (key: string, params?: Record<string, string>) =>
          ({
            'modemDetail.phone.answer': 'Answer',
            'modemDetail.phone.reject': 'Reject',
            'modemDetail.phone.hangup': 'Hang up',
            'modemDetail.phone.hold': 'Hold',
            'modemDetail.phone.resume': 'Resume',
            'modemDetail.phone.duration': 'Duration',
            'modemDetail.phone.openInCallDialpad': 'Open in-call dialpad',
            'modemDetail.phone.closeInCallDialpad': 'Close in-call dialpad',
            'modemDetail.phone.sendDtmf': `Send ${params?.digit ?? ''}`,
          })[key] ?? key,
      },
      stubs: {
        ModemCallAudioDevices: {
          props: ['call', 'session'],
          template:
            '<span data-testid="audio-devices"><button aria-label="Select microphone"></button><button aria-label="Select audio output"></button></span>',
        },
        Button: {
          props: ['disabled'],
          emits: ['click'],
          template:
            '<button type="button" v-bind="$attrs" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
        },
      },
    },
  })

vi.mock('lucide-vue-next', () => ({
  Keyboard: { template: '<span />' },
  Mic: { template: '<span />' },
  PhoneCall: { template: '<span />' },
  PhoneIncoming: { template: '<span />' },
  PhoneOff: { template: '<span />' },
  Pause: { template: '<span />' },
  Play: { template: '<span />' },
  Volume2: { template: '<span />' },
}))

describe('ModemCallBanner', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows incoming call actions in the global banner', async () => {
    const incoming = call()
    const session = makeSession({ incomingCall: incoming })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('Wi-Fi Calling')
    expect(wrapper.get('[data-testid="audio-devices"]')).toBeTruthy()

    await wrapper.get('button[aria-label="Answer"]').trigger('click')
    await wrapper.get('button[aria-label="Reject"]').trigger('click')

    expect(session.answerIncoming).toHaveBeenCalledWith(incoming)
    expect(session.reject).toHaveBeenCalledWith(incoming)
  })

  it('places incoming and active call actions on their own mobile row', () => {
    const incomingWrapper = mountBanner(makeSession({ incomingCall: call() }))
    const incomingActions = incomingWrapper.get('button[aria-label="Answer"]').element.parentElement

    expect(incomingActions?.classList.contains('w-full')).toBe(true)
    expect(incomingActions?.classList.contains('border-t')).toBe(true)
    expect(incomingActions?.classList.contains('sm:w-auto')).toBe(true)

    const activeWrapper = mountBanner(
      makeSession({ activeCall: call({ direction: 'outgoing', state: 'active' }) }),
    )
    const activeActions = activeWrapper.get('button[aria-label="Hang up"]').element.parentElement

    expect(activeActions?.classList.contains('w-full')).toBe(true)
    expect(activeActions?.classList.contains('border-t')).toBe(true)
    expect(activeActions?.classList.contains('sm:w-auto')).toBe(true)
  })

  it('does not show browser audio devices for modem calls', () => {
    const session = makeSession({
      activeCall: call({ route: 'modem', direction: 'outgoing', state: 'active' }),
    })
    const wrapper = mountBanner(session)

    expect(wrapper.find('[data-testid="audio-devices"]').exists()).toBe(false)
  })

  it('shows active call state, duration, audio status, and hangup action', async () => {
    const active = call({
      direction: 'outgoing',
      state: 'active',
      answeredAt: '2026-05-27T00:00:10Z',
    })
    const session = makeSession({
      activeCall: active,
      duration: '1:05',
      audioMessage: 'Call audio connection failed',
    })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('In call')
    expect(wrapper.text()).toContain('Duration')
    expect(wrapper.text()).toContain('1:05')
    expect(wrapper.text()).toContain('Call audio connection failed')

    await wrapper.get('button[aria-label="Hold"]').trigger('click')
    await wrapper.get('button[aria-label="Hang up"]').trigger('click')

    expect(session.toggleHold).toHaveBeenCalledWith(active)
    expect(session.hangup).toHaveBeenCalledWith(active)
  })

  it('opens an in-call dialpad and sends DTMF digits', async () => {
    const active = call({
      direction: 'outgoing',
      state: 'active',
      answeredAt: '2026-05-27T00:00:10Z',
    })
    const session = makeSession({ activeCall: active })
    const wrapper = mountBanner(session)

    await wrapper.get('button[aria-label="Open in-call dialpad"]').trigger('click')
    expect(wrapper.text()).toContain('*')
    expect(wrapper.text()).toContain('#')

    const one = wrapper.findAll('button').find((button) => button.text() === '1')
    expect(one).toBeTruthy()
    await one?.trigger('click')

    expect(session.sendDTMF).toHaveBeenCalledWith(active, '1')
  })

  it('shows local hold state and resume action', async () => {
    const active = call({
      direction: 'outgoing',
      state: 'active',
      hold: 'local',
      answeredAt: '2026-05-27T00:00:10Z',
    })
    const session = makeSession({ activeCall: active })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('On hold')
    expect(wrapper.find('button[aria-label="Open in-call dialpad"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="Resume"]').trigger('click')

    expect(session.toggleHold).toHaveBeenCalledWith(active)
  })

  it('keeps answering calls visible and hides terminal calls', async () => {
    const session = makeSession({
      activeCall: call({
        state: 'answering',
        answeredAt: '2026-05-27T00:00:05Z',
      }),
    })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('Answering')
    ;(session.activeCall as Ref<CallRecord | null>).value = call({ state: 'ended' })
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toBe('')
  })
})

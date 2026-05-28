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
    primaryLine: (item: CallRecord) => item.number || 'Unknown number',
    routeLabel: () => 'Wi-Fi Calling',
    stateLabel: (value: string) => (value === 'answering' ? 'Answering' : 'In call'),
    answerIncoming: vi.fn(),
    reject: vi.fn(),
    hangup: vi.fn(),
  }) as unknown as ModemCallSession

const mountBanner = (session: ModemCallSession) =>
  mount(ModemCallBanner, {
    props: { session },
    global: {
      mocks: {
        $t: (key: string) =>
          ({
            'modemDetail.phone.answer': 'Answer',
            'modemDetail.phone.reject': 'Reject',
            'modemDetail.phone.hangup': 'Hang up',
            'modemDetail.phone.duration': 'Duration',
          })[key] ?? key,
      },
      stubs: {
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
  Mic: { template: '<span />' },
  PhoneCall: { template: '<span />' },
  PhoneIncoming: { template: '<span />' },
  PhoneOff: { template: '<span />' },
}))

describe('ModemCallBanner', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows incoming call actions in the global banner', async () => {
    const incoming = call()
    const session = makeSession({ incomingCall: incoming })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('+12242255559')
    expect(wrapper.text()).toContain('Wi-Fi Calling')

    await wrapper.get('button[aria-label="Answer"]').trigger('click')
    await wrapper.get('button[aria-label="Reject"]').trigger('click')

    expect(session.answerIncoming).toHaveBeenCalledWith(incoming)
    expect(session.reject).toHaveBeenCalledWith(incoming)
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
      audioMessage: 'Call audio requires an AMR/AMR-WB codec module.',
    })
    const wrapper = mountBanner(session)

    expect(wrapper.text()).toContain('+12242255559')
    expect(wrapper.text()).toContain('In call')
    expect(wrapper.text()).toContain('Duration')
    expect(wrapper.text()).toContain('1:05')
    expect(wrapper.text()).toContain('Call audio requires an AMR/AMR-WB codec module.')

    await wrapper.get('button[aria-label="Hang up"]').trigger('click')

    expect(session.hangup).toHaveBeenCalledWith(active)
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

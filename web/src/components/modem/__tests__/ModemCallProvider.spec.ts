import { mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemCallProvider from '@/components/modem/ModemCallProvider.vue'
import { useModemCallSession } from '@/composables/useModemCallSession'
import type { CallRecord } from '@/types/call'

const route = vi.hoisted(() => ({
  params: {
    id: 'modem-1' as string | undefined,
  },
}))

const callsHarness = vi.hoisted(() => ({
  activeCall: null as unknown as ReturnType<typeof ref<CallRecord | null>>,
  incomingCall: null as unknown as ReturnType<typeof ref<CallRecord | null>>,
  usePhoneCalls: vi.fn(),
  useCallAudioSession: vi.fn(),
}))

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

vi.mock('vue-router', () => ({
  useRoute: () => route,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) =>
      ({
        'modemDetail.phone.routes.wifiCalling': 'Wi-Fi Calling',
        'modemDetail.phone.routes.modem': 'Modem',
        'modemDetail.phone.routes.auto': 'Auto',
        'modemDetail.phone.states.dialing': 'Dialing',
        'modemDetail.phone.states.ringing': 'Ringing',
        'modemDetail.phone.states.answering': 'Answering',
        'modemDetail.phone.states.earlyMedia': 'Early media',
        'modemDetail.phone.states.active': 'In call',
        'modemDetail.phone.states.confirmed': 'Connected',
        'modemDetail.phone.states.ending': 'Ending',
        'modemDetail.phone.states.failed': 'Failed',
        'modemDetail.phone.states.ended': 'Ended',
        'modemDetail.phone.unknownNumber': 'Unknown number',
        'modemDetail.phone.durationEmpty': '0:00',
        'modemDetail.phone.audioCodecUnavailable':
          'Call audio requires an AMR/AMR-WB codec module.',
      })[key] ?? key,
  }),
}))

vi.mock('@/composables/usePhoneCalls', () => ({
  usePhoneCalls: callsHarness.usePhoneCalls,
}))

vi.mock('@/composables/useCallAudioSession', () => ({
  useCallAudioSession: callsHarness.useCallAudioSession,
}))

vi.mock('@/composables/useModemPhoneCountry', () => ({
  useModemPhoneCountry: () => ({ phoneCountry: computed(() => 'US') }),
}))

vi.mock('@/lib/browserAmrCodec', () => ({
  createBrowserAmrCodec: vi.fn(),
  hasBrowserAmrCodec: () => false,
}))

const Consumer = defineComponent({
  setup() {
    const session = useModemCallSession(computed(() => 'fallback-modem'))
    return { session }
  },
  template:
    '<section data-testid="consumer">{{ session.primaryLine(session.incomingCall.value) }}</section>',
})

const mountProvider = () =>
  mount(ModemCallProvider, {
    slots: {
      default: Consumer,
    },
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

describe('ModemCallProvider', () => {
  beforeEach(() => {
    route.params.id = 'modem-1'
    callsHarness.activeCall = ref<CallRecord | null>(null)
    callsHarness.incomingCall = ref<CallRecord | null>(call())
    callsHarness.usePhoneCalls.mockReset()
    callsHarness.usePhoneCalls.mockReturnValue({
      recentCalls: computed(() => []),
      hasRecentCalls: computed(() => false),
      activeCall: callsHarness.activeCall,
      incomingCall: callsHarness.incomingCall,
      isLoading: ref(false),
      isDialing: ref(false),
      errorMessage: ref(''),
      dial: vi.fn(),
      answer: vi.fn(),
      reject: vi.fn(),
      hangup: vi.fn(),
      loadCalls: vi.fn(),
    })
    callsHarness.useCallAudioSession.mockReset()
    callsHarness.useCallAudioSession.mockReturnValue({
      errorMessage: ref(''),
      prepare: vi.fn(),
      start: vi.fn(),
      stop: vi.fn(),
    })
  })

  it('provides the same call session to routed children and the global banner', () => {
    const wrapper = mountProvider()

    expect(callsHarness.usePhoneCalls).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="consumer"]').text()).toBe('(224) 225-5559')
    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('Wi-Fi Calling')
  })
})

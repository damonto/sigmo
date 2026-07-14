import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, nextTick, ref } from 'vue'
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
  remoteStream: null as unknown as ReturnType<typeof ref<MediaStream | null>>,
  bindOutputElement: vi.fn(),
  prepareAudio: vi.fn(),
  stopAudio: vi.fn(),
  usePhoneCalls: vi.fn(),
  useCallAudioSession: vi.fn(),
}))

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
        'modemDetail.phone.holdStates.local': 'On hold',
        'modemDetail.phone.holdStates.remote': 'Remote hold',
        'modemDetail.phone.holdStates.localRemote': 'Both on hold',
        'modemDetail.phone.unknownNumber': 'Unknown number',
        'modemDetail.phone.durationEmpty': '0:00',
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

const Consumer = defineComponent({
  setup() {
    const session = useModemCallSession(computed(() => 'fallback-modem'))
    return { session }
  },
  template:
    '<section data-testid="consumer">{{ session.incomingCall.value ? session.primaryLine(session.incomingCall.value) : "" }}</section>',
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
            'modemDetail.phone.hold': 'Hold',
            'modemDetail.phone.resume': 'Resume',
            'modemDetail.phone.duration': 'Duration',
          })[key] ?? key,
      },
      stubs: {
        ModemCallAudioDevices: { template: '<span />' },
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
  let srcObject: MediaProvider | null

  beforeEach(() => {
    srcObject = null
    Object.defineProperty(HTMLMediaElement.prototype, 'srcObject', {
      configurable: true,
      get: () => srcObject,
      set: (value: MediaProvider | null) => {
        srcObject = value
      },
    })
    route.params.id = 'modem-1'
    callsHarness.activeCall = ref<CallRecord | null>(null)
    callsHarness.incomingCall = ref<CallRecord | null>(call())
    callsHarness.remoteStream = ref<MediaStream | null>(null)
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
      hold: vi.fn(),
      resume: vi.fn(),
      toggleHold: vi.fn(),
      loadCalls: vi.fn(),
    })
    callsHarness.useCallAudioSession.mockReset()
    callsHarness.bindOutputElement.mockReset()
    callsHarness.bindOutputElement.mockResolvedValue(true)
    callsHarness.prepareAudio.mockReset()
    callsHarness.prepareAudio.mockResolvedValue(true)
    callsHarness.stopAudio.mockReset()
    callsHarness.useCallAudioSession.mockReturnValue({
      errorMessage: ref(''),
      deviceNotice: ref(''),
      remoteStream: callsHarness.remoteStream,
      inputDevices: ref([]),
      outputDevices: ref([]),
      selectedInputDeviceID: ref(''),
      selectedOutputDeviceID: ref(''),
      isRefreshingDevices: ref(false),
      isSwitchingInput: ref(false),
      isSwitchingOutput: ref(false),
      outputSelectionSupported: ref(true),
      mediaStatus: ref('idle'),
      prepare: callsHarness.prepareAudio,
      start: vi.fn(),
      stop: callsHarness.stopAudio,
      setInputEnabled: vi.fn(),
      refreshDevices: vi.fn(),
      selectInputDevice: vi.fn(),
      selectOutputDevice: vi.fn(),
      bindOutputElement: callsHarness.bindOutputElement,
    })
  })

  it('provides the same call session to routed children and the global banner', async () => {
    const wrapper = mountProvider()
    await flushPromises()

    expect(callsHarness.usePhoneCalls).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="consumer"]').text()).toBe('(224) 225-5559')
    expect(wrapper.text()).toContain('(224) 225-5559')
    expect(wrapper.text()).toContain('Wi-Fi Calling')
  })

  it('plays the remote call audio stream when it is attached', async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    const wrapper = mountProvider()
    const stream = {} as MediaStream

    callsHarness.remoteStream.value = stream
    await nextTick()
    await flushPromises()

    const audio = wrapper.get('audio').element as HTMLAudioElement
    expect(audio.srcObject).toBe(callsHarness.remoteStream.value)
    expect(callsHarness.bindOutputElement).toHaveBeenCalledWith(audio)
    expect(callsHarness.bindOutputElement.mock.invocationCallOrder[0]).toBeLessThan(
      play.mock.invocationCallOrder[0],
    )
    expect(play).toHaveBeenCalled()

    callsHarness.remoteStream.value = null
    await nextTick()
    await flushPromises()

    expect(audio.srcObject).toBeNull()
    expect(pause).toHaveBeenCalled()
    play.mockRestore()
    pause.mockRestore()
  })

  it('waits for output binding before playing remote audio', async () => {
    let resolveBinding!: (bound: boolean) => void
    callsHarness.bindOutputElement.mockReturnValueOnce(
      new Promise<boolean>((resolve) => {
        resolveBinding = resolve
      }),
    )
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    mountProvider()

    callsHarness.remoteStream.value = {} as MediaStream
    await nextTick()

    expect(play).not.toHaveBeenCalled()

    resolveBinding(true)
    await flushPromises()

    expect(callsHarness.bindOutputElement).toHaveBeenCalledOnce()
    expect(play).toHaveBeenCalledOnce()
    play.mockRestore()
  })

  it('releases microphone preparation when an unanswered incoming call disappears', async () => {
    const wrapper = mountProvider()
    const incoming = callsHarness.incomingCall.value
    const banner = wrapper.findComponent({ name: 'ModemCallBanner' })
    const session = banner.props('session') as ReturnType<typeof useModemCallSession>

    expect(incoming).toBeTruthy()
    await session.prepareAudioDevices(incoming!)
    callsHarness.incomingCall.value = null
    await nextTick()
    await flushPromises()

    expect(callsHarness.prepareAudio).toHaveBeenCalled()
    expect(callsHarness.stopAudio).toHaveBeenCalled()
  })

  it('releases a pending microphone capture when the incoming call disappears', async () => {
    let resolvePrepare!: (ready: boolean) => void
    callsHarness.prepareAudio.mockReturnValueOnce(
      new Promise<boolean>((resolve) => {
        resolvePrepare = resolve
      }),
    )
    const wrapper = mountProvider()
    const incoming = callsHarness.incomingCall.value
    const banner = wrapper.findComponent({ name: 'ModemCallBanner' })
    const session = banner.props('session') as ReturnType<typeof useModemCallSession>

    const prepared = session.prepareAudioDevices(incoming!)
    callsHarness.incomingCall.value = null
    await nextTick()
    resolvePrepare(true)

    await expect(prepared).resolves.toBe(false)
    expect(callsHarness.stopAudio).toHaveBeenCalled()
  })
})

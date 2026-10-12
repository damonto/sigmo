import { mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import ModemCallAudioDevices from '@/components/modem/ModemCallAudioDevices.vue'
import type { ModemCallSession } from '@/composables/useModemCallSession'
import type { CallRecord } from '@/types/call'

const labels: Record<string, string> = {
  'modemDetail.phone.audioDevices.microphone': 'Microphone',
  'modemDetail.phone.audioDevices.output': 'Audio output',
  'modemDetail.phone.audioDevices.systemDefault': 'System default',
  'modemDetail.phone.audioDevices.outputUnsupported': 'Output is controlled by the system',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string | number>) => {
      if (key === 'modemDetail.phone.audioDevices.microphoneFallback') {
        return `Microphone ${params?.index}`
      }
      if (key === 'modemDetail.phone.audioDevices.outputFallback') {
        return `Audio output ${params?.index}`
      }
      if (key === 'modemDetail.phone.audioDevices.selectMicrophone') {
        return `Select microphone, current: ${params?.device}`
      }
      if (key === 'modemDetail.phone.audioDevices.selectOutput') {
        return `Select audio output, current: ${params?.device}`
      }
      return labels[key] ?? key
    },
  }),
}))

vi.mock('lucide-vue-next', () => ({
  Mic: { template: '<span />' },
  Volume2: { template: '<span />' },
}))

const call: CallRecord = {
  callID: 'call-1',
  route: 'wifi_calling',
  direction: 'incoming',
  number: '+12242255559',
  state: 'ringing',
  hold: 'none',
  reason: '',
  startedAt: '2026-07-14T00:00:00Z',
  answeredAt: '',
  endedAt: '',
  updatedAt: '2026-07-14T00:00:00Z',
}

const makeSession = (outputSupported = true) =>
  ({
    callAudio: {
      inputDevices: ref([
        { deviceId: 'mic-1', label: 'Desk microphone' },
        { deviceId: 'mic-2', label: '' },
      ]),
      outputDevices: ref([{ deviceId: 'speaker-1', label: 'USB headset' }]),
      selectedInputDeviceID: ref('mic-1'),
      selectedOutputDeviceID: ref(''),
      isSwitchingInput: ref(false),
      isSwitchingOutput: ref(false),
      isRefreshingDevices: ref(false),
      outputSelectionSupported: ref(outputSupported),
      mediaStatus: ref('idle'),
      refreshDevices: vi.fn(),
      selectInputDevice: vi.fn(),
      selectOutputDevice: vi.fn(),
    },
    prepareAudioDevices: vi.fn(),
    answerIncoming: vi.fn(),
    audioMessage: computed(() => ''),
  }) as unknown as ModemCallSession

const DropdownMenu = defineComponent({
  name: 'DropdownMenu',
  emits: ['update:open'],
  template: '<div @click="$emit(\'update:open\', true)"><slot /></div>',
})

const DropdownMenuRadioGroup = defineComponent({
  name: 'DropdownMenuRadioGroup',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<div><slot /></div>',
})

const mountDevices = (session: ModemCallSession) =>
  mount(ModemCallAudioDevices, {
    props: { call, session },
    global: {
      stubs: {
        Button: {
          props: ['disabled'],
          template: '<button v-bind="$attrs" :disabled="disabled"><slot /></button>',
        },
        DropdownMenu,
        DropdownMenuContent: { template: '<div><slot /></div>' },
        DropdownMenuLabel: { template: '<strong><slot /></strong>' },
        DropdownMenuRadioGroup,
        DropdownMenuRadioItem: {
          props: ['value', 'disabled'],
          template: '<div :data-value="value" :data-disabled="disabled"><slot /></div>',
        },
        DropdownMenuSeparator: { template: '<hr />' },
        DropdownMenuTrigger: { template: '<div><slot /></div>' },
      },
    },
  })

describe('ModemCallAudioDevices', () => {
  it('shows separate microphone and output controls with current device labels', () => {
    const wrapper = mountDevices(makeSession())

    expect(
      wrapper.get('button[aria-label="Select microphone, current: Desk microphone"]'),
    ).toBeTruthy()
    expect(
      wrapper.get('button[aria-label="Select audio output, current: System default"]'),
    ).toBeTruthy()
    expect(wrapper.text()).toContain('Microphone 2')
    expect(wrapper.text()).toContain('USB headset')
  })

  it('prepares incoming audio without answering when the microphone menu opens', async () => {
    const session = makeSession()
    const wrapper = mountDevices(session)

    await wrapper.get('button[aria-label^="Select microphone"]').trigger('click')

    expect(session.prepareAudioDevices).toHaveBeenCalledWith(call)
    expect(session.answerIncoming).not.toHaveBeenCalled()
  })

  it('selects microphone and output radio values', () => {
    const session = makeSession()
    const wrapper = mountDevices(session)
    const groups = wrapper.findAllComponents({ name: 'DropdownMenuRadioGroup' })

    groups[0]?.vm.$emit('update:modelValue', 'mic-2')
    groups[1]?.vm.$emit('update:modelValue', 'speaker-1')

    expect(session.callAudio.selectInputDevice).toHaveBeenCalledWith('mic-2')
    expect(session.callAudio.selectOutputDevice).toHaveBeenCalledWith('speaker-1')
  })

  it('disables output selection when the browser does not support setSinkId', () => {
    const wrapper = mountDevices(makeSession(false))
    const button = wrapper.get('button[aria-label="Output is controlled by the system"]')

    expect(button.attributes('disabled')).toBeDefined()
  })
})

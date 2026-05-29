import { flushPromises, mount } from '@vue/test-utils'
import { computed, nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { usePhoneCalls } from '@/composables/usePhoneCalls'
import type { CallRecord } from '@/types/call'

const api = vi.hoisted(() => ({
  listCalls: vi.fn(),
  dialCall: vi.fn(),
  answerCall: vi.fn(),
  rejectCall: vi.fn(),
  hangupCall: vi.fn(),
  deleteCall: vi.fn(),
}))

vi.mock('@/apis/call', () => ({
  buildCallEventsUrl: (id: string) => `ws://localhost/api/v1/modems/${id}/calls/events`,
  useCallApi: () => api,
}))

class FakeWebSocket {
  static instances: FakeWebSocket[] = []

  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  closed = false

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this)
  }

  close() {
    this.closed = true
  }

  open() {
    this.onopen?.()
  }

  closeFromServer() {
    this.closed = true
    this.onclose?.()
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>)
  }
}

class FakeAudioContext {
  currentTime = 0
  destination = {}
  closed = false

  createOscillator() {
    return {
      type: '',
      frequency: { value: 0 },
      connect: vi.fn(),
      start: vi.fn(),
      stop: vi.fn(),
    }
  }

  createGain() {
    return {
      gain: { value: 0 },
      connect: vi.fn(),
    }
  }

  close() {
    this.closed = true
    return Promise.resolve()
  }
}

const notifications: FakeNotification[] = []

class FakeNotification {
  static permission: NotificationPermission = 'default'
  static requestPermission = vi.fn()
  onclick: ((event: Event) => void) | null = null
  onclose: ((event: Event) => void) | null = null
  closed = false

  constructor(readonly title: string, readonly options?: NotificationOptions) {
    notifications.push(this)
  }

  close() {
    this.closed = true
    this.onclose?.(new Event('close'))
  }

  click() {
    this.onclick?.(new Event('click'))
  }
}

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

const mountComposable = () => {
  const modemId = ref('modem-1')
  const phoneCountry = ref('US')
  let phone!: ReturnType<typeof usePhoneCalls>
  const wrapper = mount({
    template: '<div />',
    setup() {
      phone = usePhoneCalls(
        computed(() => modemId.value),
        computed(() => phoneCountry.value),
      )
      return {}
    },
  })
  return { wrapper, modemId, phone, phoneCountry }
}

describe('usePhoneCalls', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useRealTimers()
    FakeWebSocket.instances = []
    notifications.length = 0
    FakeNotification.permission = 'default'
    FakeNotification.requestPermission.mockResolvedValue('granted')
    vi.stubGlobal('WebSocket', FakeWebSocket)
    vi.stubGlobal('AudioContext', FakeAudioContext)
    vi.stubGlobal('Notification', FakeNotification)
    api.listCalls.mockResolvedValue({ data: ref([]) })
    api.dialCall.mockResolvedValue({ data: ref(call({ direction: 'outgoing', state: 'dialing' })) })
    api.answerCall.mockResolvedValue({ data: ref(call({ state: 'active', answeredAt: '2026-05-27T00:00:10Z' })) })
    api.rejectCall.mockResolvedValue({ data: ref(call({ state: 'ended', endedAt: '2026-05-27T00:00:10Z' })) })
    api.hangupCall.mockResolvedValue({ data: ref(call({ state: 'ended', endedAt: '2026-05-27T00:00:10Z' })) })
    api.deleteCall.mockResolvedValue({ data: ref(undefined) })
  })

  it('opens call events and surfaces incoming calls with a foreground notification', async () => {
    FakeNotification.permission = 'granted'
    const { phone } = mountComposable()
    await flushPromises()

    const ws = FakeWebSocket.instances[0]
    expect(ws?.url).toBe('ws://localhost/api/v1/modems/modem-1/calls/events')
    ws?.message({ type: 'call', call: call() })
    await nextTick()

    expect(phone.incomingCall.value?.callID).toBe('call-1')
    expect(phone.activeCall.value).toBeNull()
    expect(notifications).toHaveLength(1)
    expect(notifications[0]?.title).toBe('Incoming call')
    expect(notifications[0]?.options).toEqual({ body: '(224) 225-5559', tag: 'call-1' })
  })

  it('reconnects call events after an unexpected socket close', async () => {
    vi.useFakeTimers()
    mountComposable()
    await flushPromises()

    const first = FakeWebSocket.instances[0]
    expect(first?.url).toBe('ws://localhost/api/v1/modems/modem-1/calls/events')
    first?.closeFromServer()

    await vi.advanceTimersByTimeAsync(999)
    expect(FakeWebSocket.instances).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(FakeWebSocket.instances).toHaveLength(2)
    expect(FakeWebSocket.instances[1]?.url).toBe('ws://localhost/api/v1/modems/modem-1/calls/events')
  })

  it('does not reconnect call events after the component is unmounted', async () => {
    vi.useFakeTimers()
    const { wrapper } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.closeFromServer()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(1000)

    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('surfaces an already-ringing incoming call from the initial call list', async () => {
    FakeNotification.permission = 'granted'
    api.listCalls.mockResolvedValue({ data: ref([call()]) })

    const { phone } = mountComposable()
    await flushPromises()

    expect(phone.incomingCall.value?.callID).toBe('call-1')
    expect(phone.activeCall.value).toBeNull()
    expect(notifications).toHaveLength(1)
    expect(notifications[0]?.options?.tag).toBe('call-1')
  })

  it('restores an answering call from the initial call list', async () => {
    api.listCalls.mockResolvedValue({
      data: ref([call({ state: 'answering', answeredAt: '2026-05-27T00:00:10Z' })]),
    })

    const { phone } = mountComposable()
    await flushPromises()

    expect(phone.incomingCall.value).toBeNull()
    expect(phone.activeCall.value?.state).toBe('answering')
  })

  it('restores early media and confirmed calls from the initial call list', async () => {
    api.listCalls.mockResolvedValue({
      data: ref([
        call({
          callID: 'call-confirmed',
          direction: 'outgoing',
          state: 'confirmed',
          updatedAt: '2026-05-27T00:00:20Z',
        }),
        call({
          callID: 'call-early',
          direction: 'outgoing',
          state: 'early_media',
          updatedAt: '2026-05-27T00:00:10Z',
        }),
      ]),
    })

    const { phone } = mountComposable()
    await flushPromises()

    expect(phone.incomingCall.value).toBeNull()
    expect(phone.activeCall.value?.callID).toBe('call-confirmed')
  })

  it('closes stale incoming notifications when refreshed calls are no longer ringing', async () => {
    FakeNotification.permission = 'granted'
    api.listCalls.mockResolvedValueOnce({ data: ref([call()]) })
    const { phone } = mountComposable()
    await flushPromises()
    const notification = notifications[0]
    expect(notification?.closed).toBe(false)

    api.listCalls.mockResolvedValueOnce({
      data: ref([call({ state: 'active', answeredAt: '2026-05-27T00:00:10Z' })]),
    })
    await phone.loadCalls()

    expect(phone.incomingCall.value).toBeNull()
    expect(phone.activeCall.value?.state).toBe('active')
    expect(notification?.closed).toBe(true)
  })

  it('focuses the page when the foreground incoming notification is clicked', async () => {
    FakeNotification.permission = 'granted'
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {})
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({ type: 'call', call: call() })
    await nextTick()
    notifications[0]?.click()

    expect(focus).toHaveBeenCalled()
    expect(phone.incomingCall.value?.callID).toBe('call-1')
    expect(phone.activeCall.value).toBeNull()
  })

  it('keeps incoming state when ringtone creation is blocked', async () => {
    vi.stubGlobal('AudioContext', class {
      constructor() {
        throw new Error('blocked')
      }
    })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({ type: 'call', call: call() })
    await nextTick()

    expect(phone.incomingCall.value?.callID).toBe('call-1')
    expect(warn).toHaveBeenCalledWith('[usePhoneCalls] play ringtone:', expect.any(Error))
  })

  it('dials and requests notification permission from a user action path', async () => {
    const { phone } = mountComposable()
    await flushPromises()

    const result = await phone.dial('+12242255559', 'auto')

    expect(api.dialCall).toHaveBeenCalledWith('modem-1', { to: '+12242255559', route: 'auto' })
    expect(result?.number).toBe('+12242255559')
    expect(FakeNotification.requestPermission).toHaveBeenCalled()
  })

  it('keeps dial API failures handled inside the composable', async () => {
    api.dialCall.mockRejectedValueOnce(new Error('wifi calling is not connected'))
    const { phone } = mountComposable()
    await flushPromises()

    const result = await phone.dial('+12242255559', 'wifi_calling')

    expect(result).toBeNull()
    expect(phone.errorMessage.value).toBe('wifi calling is not connected')
  })

  it('answers, rejects, and hangs up through the route-neutral call API', async () => {
    const { phone } = mountComposable()
    await flushPromises()
    const incoming = call()

    await phone.answer(incoming)
    await phone.reject(incoming)
    await phone.hangup(incoming)

    expect(api.answerCall).toHaveBeenCalledWith('modem-1', 'call-1')
    expect(api.rejectCall).toHaveBeenCalledWith('modem-1', 'call-1')
    expect(api.hangupCall).toHaveBeenCalledWith('modem-1', 'call-1')
  })

  it('deletes terminal records from the local list', async () => {
    api.listCalls.mockResolvedValueOnce({
      data: ref([call({ state: 'ended', endedAt: '2026-05-27T00:00:10Z' })]),
    })
    const { phone } = mountComposable()
    await flushPromises()

    const deleted = await phone.deleteRecord(phone.recentCalls.value[0]!)

    expect(deleted).toBe(true)
    expect(api.deleteCall).toHaveBeenCalledWith('modem-1', 'call-1')
    expect(phone.recentCalls.value).toHaveLength(0)
  })

  it('clears the incoming banner when an incoming call becomes active', async () => {
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({ type: 'call', call: call() })
    await nextTick()
    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({ state: 'active', answeredAt: '2026-05-27T00:00:10Z' }),
    })
    await nextTick()

    expect(phone.incomingCall.value).toBeNull()
    expect(phone.activeCall.value?.state).toBe('active')
  })

  it('keeps the current call visible while it is answering', async () => {
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({ type: 'call', call: call() })
    await nextTick()
    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({ state: 'answering', answeredAt: '2026-05-27T00:00:10Z' }),
    })
    await nextTick()

    expect(phone.incomingCall.value).toBeNull()
    expect(phone.activeCall.value?.state).toBe('answering')
  })

  it('keeps the current call visible during early media and confirmed states', async () => {
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({ state: 'early_media', direction: 'outgoing' }),
    })
    await nextTick()
    expect(phone.activeCall.value?.state).toBe('early_media')

    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({
        state: 'confirmed',
        direction: 'outgoing',
        updatedAt: '2026-05-27T00:00:20Z',
      }),
    })
    await nextTick()

    expect(phone.activeCall.value?.state).toBe('confirmed')
  })

  it('closes incoming notifications when the call leaves ringing state', async () => {
    FakeNotification.permission = 'granted'
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({ type: 'call', call: call() })
    await nextTick()
    const notification = notifications[0]
    expect(notification?.closed).toBe(false)

    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({ state: 'ended', endedAt: '2026-05-27T00:00:10Z' }),
    })
    await nextTick()

    expect(notification?.closed).toBe(true)
    expect(phone.incomingCall.value).toBeNull()
  })

  it('clears the active call when the remote side ends it', async () => {
    const { phone } = mountComposable()
    await flushPromises()

    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({ state: 'active', direction: 'outgoing', answeredAt: '2026-05-27T00:00:10Z' }),
    })
    await nextTick()
    expect(phone.activeCall.value?.state).toBe('active')

    FakeWebSocket.instances[0]?.message({
      type: 'call',
      call: call({
        state: 'ended',
        direction: 'outgoing',
        reason: 'remote bye',
        answeredAt: '2026-05-27T00:00:10Z',
        endedAt: '2026-05-27T00:00:20Z',
        updatedAt: '2026-05-27T00:00:20Z',
      }),
    })
    await nextTick()

    expect(phone.activeCall.value).toBeNull()
    expect(phone.recentCalls.value[0]?.state).toBe('ended')
  })
})

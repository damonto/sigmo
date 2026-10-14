import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useCallMonitor } from '@/composables/useCallMonitor'
import type { CallRecord } from '@/types/call'

vi.mock('@/apis/call', () => ({ buildCallEventsUrl: (id: string) => `ws://fixture/${id}` }))
vi.mock('@/apis/modem', () => ({
  useModemApi: () => ({ getModems: async () => ({ data: { value: [{ id: 'a' }, { id: 'b' }] } }) }),
}))
vi.mock('@/composables/useCapabilities', () => ({
  useCapabilities: () => ({ hasFeature: () => true, fetchCapabilities: async () => undefined }),
}))
class Socket {
  static instances: Socket[] = []
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onclose: (() => void) | null = null
  closed = false
  constructor(readonly url: string) {
    Socket.instances.push(this)
  }
  close() {
    this.closed = true
    this.onclose?.()
  }
  receive(call: CallRecord) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify({ type: 'call', call }) }))
  }
}
const call: CallRecord = {
  callID: 'incoming',
  number: '10086',
  direction: 'incoming',
  state: 'ringing',
  route: 'volte',
  hold: 'none',
  reason: '',
  startedAt: '2026-10-13T00:00:00Z',
  updatedAt: '2026-10-13T00:00:00Z',
  answeredAt: '',
  endedAt: '',
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  Socket.instances = []
  vi.stubGlobal('WebSocket', Socket)
})
afterEach(() => {
  vi.unstubAllGlobals()
})
describe('backend-wide call observation', () => {
  it('keeps the active audio owner until idle, then offers another device', async () => {
    const busy = ref(true),
      receive = vi.fn().mockResolvedValue(undefined)
    const wrapper = mount(
      defineComponent({
        setup() {
          useCallMonitor(busy, receive)
          return () => null
        },
      }),
    )
    await flushPromises()
    expect(Socket.instances.map((item) => item.url)).toEqual(['ws://fixture/a', 'ws://fixture/b'])
    Socket.instances[1]?.receive(call)
    expect(receive).not.toHaveBeenCalled()
    busy.value = false
    await flushPromises()
    expect(receive).toHaveBeenCalledWith('b', call)
    wrapper.unmount()
    expect(Socket.instances.every((item) => item.closed)).toBe(true)
  })
  it('does not resurrect ended or disconnected invitations', async () => {
    const busy = ref(true),
      receive = vi.fn().mockResolvedValue(undefined)
    mount(
      defineComponent({
        setup() {
          useCallMonitor(busy, receive)
          return () => null
        },
      }),
    )
    await flushPromises()
    const socket = Socket.instances[0]
    socket?.receive(call)
    socket?.receive({ ...call, state: 'ended', endedAt: '2026-10-13T00:01:00Z' })
    socket?.receive(call)
    Socket.instances[1]?.receive({ ...call, callID: 'other' })
    Socket.instances[1]?.close()
    busy.value = false
    await flushPromises()
    expect(receive).not.toHaveBeenCalled()
  })
})

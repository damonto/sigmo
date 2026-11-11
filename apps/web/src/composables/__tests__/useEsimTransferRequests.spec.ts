import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useEsimTransferRequests } from '@/composables/useEsimTransferRequests'
import { invalidateSession } from '@/lib/authSession'

const api = vi.hoisted(() => ({
  decide: vi.fn().mockResolvedValue(undefined),
  capabilities: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@/lib/fetch', () => ({ fetchJson: api.decide }))
vi.mock('@/composables/useCapabilities', () => ({
  useCapabilities: () => ({
    features: ref(['esimTransfer']),
    hasFeature: () => true,
    fetchCapabilities: api.capabilities,
  }),
}))

class RequestSocket {
  static instances: RequestSocket[] = []
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(readonly url: string) {
    RequestSocket.instances.push(this)
  }
  close() {
    this.closed = true
  }
  snapshot(requests: unknown[]) {
    this.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify({ type: 'requests', requests }) }),
    )
  }
}

const request = {
  id: 'request-1',
  revision: 1,
  phase: 'authorization',
  remoteAddress: '192.168.1.2',
  target: { name: 'Target', imei: '123456789012345', eid: '11111111111111111111111111111111' },
  expiresAt: '2026-11-10T12:00:00Z',
}

const selection = { sourceType: 'modem' as const, sourceId: 'source-1', profileId: 'profile' }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('global eSIM approvals', () => {
  it('submits the source owner’s selection independently of the page and clears on logout', async () => {
    RequestSocket.instances = []
    vi.stubGlobal('WebSocket', RequestSocket)
    let approvals: ReturnType<typeof useEsimTransferRequests> | undefined
    const wrapper = mount(
      defineComponent({
        setup() {
          approvals = useEsimTransferRequests()
          return () => h('main', 'Settings page')
        },
      }),
    )
    await flushPromises()
    const socket = RequestSocket.instances[0]
    expect(socket?.url).toContain('esim-transfer-requests/events')
    socket?.snapshot([request])
    expect(approvals?.current.value?.phase).toBe('authorization')
    if (approvals?.current.value) await approvals.decide(approvals.current.value, true, selection)
    expect(api.decide).toHaveBeenLastCalledWith(
      expect.stringContaining('/decisions'),
      expect.objectContaining({
        body: JSON.stringify({ phase: 'authorization', revision: 1, accept: true, selection }),
      }),
    )
    socket?.snapshot([
      {
        ...request,
        phase: 'running',
        revision: 3,
        profile: {
          id: 'profile',
          type: 'esim',
          name: 'Line',
          iccid: '8901',
          enabled: true,
          supported: true,
        },
      },
    ])
    expect(approvals?.current.value?.phase).toBe('running')
    expect(api.decide).toHaveBeenCalledTimes(1)
    invalidateSession()
    await flushPromises()
    expect(socket?.closed).toBe(true)
    expect(approvals?.current.value).toBeUndefined()
    wrapper.unmount()
  })
  it('does not open a subscription after unmount while capabilities are pending', async () => {
    RequestSocket.instances = []
    vi.stubGlobal('WebSocket', RequestSocket)
    let finish: (() => void) | undefined
    api.capabilities.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finish = resolve
      }),
    )
    const wrapper = mount(
      defineComponent({
        setup() {
          useEsimTransferRequests()
          return () => h('main')
        },
      }),
    )
    wrapper.unmount()
    finish?.()
    await flushPromises()
    expect(RequestSocket.instances).toHaveLength(0)
  })

  it('does not apply an old decision failure after a login change', async () => {
    RequestSocket.instances = []
    vi.stubGlobal('WebSocket', RequestSocket)
    let fail: ((error: Error) => void) | undefined
    api.decide.mockReturnValueOnce(
      new Promise((_, reject) => {
        fail = reject
      }),
    )
    let approvals: ReturnType<typeof useEsimTransferRequests> | undefined
    const wrapper = mount(
      defineComponent({
        setup() {
          approvals = useEsimTransferRequests()
          return () => h('main')
        },
      }),
    )
    await flushPromises()
    RequestSocket.instances[0]?.snapshot([request])
    const first = approvals?.current.value
    if (!approvals || !first) throw new Error('missing approval')
    const pending = approvals.decide(first, true, selection)
    invalidateSession()
    await flushPromises()
    expect(approvals.busy.value).toBe(false)
    fail?.(new Error('old request failed'))
    await pending
    expect(approvals.error.value).toBe('')
    wrapper.unmount()
  })

  it('does not submit a stale displayed revision', async () => {
    RequestSocket.instances = []
    vi.stubGlobal('WebSocket', RequestSocket)
    let approvals: ReturnType<typeof useEsimTransferRequests> | undefined
    const wrapper = mount(
      defineComponent({
        setup() {
          approvals = useEsimTransferRequests()
          return () => h('main')
        },
      }),
    )
    await flushPromises()
    RequestSocket.instances[0]?.snapshot([request])
    const displayed = approvals?.current.value
    RequestSocket.instances[0]?.snapshot([{ ...request, revision: 3, phase: 'ended' }])
    if (approvals && displayed) await approvals.decide(displayed, true, selection)
    expect(api.decide).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

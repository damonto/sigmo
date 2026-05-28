import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { buildCallMediaUrl } from '@/apis/call'
import { useCallMediaSession } from '@/composables/useCallMediaSession'
import { clearStoredToken, setStoredToken } from '@/lib/auth-storage'

class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []

  binaryType = ''
  readyState = FakeWebSocket.OPEN
  sent: unknown[] = []
  onmessage: ((event: MessageEvent<unknown>) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this)
  }

  send(message: unknown) {
    this.sent.push(message)
  }

  close() {
    this.readyState = 3
  }

  message(data: unknown) {
    this.onmessage?.({ data } as MessageEvent<unknown>)
  }

  error() {
    this.onerror?.()
  }

  closeFromServer() {
    this.readyState = 3
    this.onclose?.()
  }
}

describe('call media session', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    clearStoredToken()
  })

  it('builds a tokenized media websocket URL', () => {
    setStoredToken('token-1')

    const url = new URL(buildCallMediaUrl('modem-1', 'call/id'))

    expect(url.protocol).toBe('ws:')
    expect(url.pathname).toBe('/api/v1/modems/modem-1/calls/call%2Fid/media')
    expect(url.searchParams.get('token')).toBe('token-1')
  })

  it('marks the session ready after the media handshake', () => {
    const session = useCallMediaSession(ref('modem-1'))

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    const url = new URL(ws.url)
    expect(url.protocol).toBe('ws:')
    expect(url.pathname).toBe('/api/v1/modems/modem-1/calls/call-1/media')
    expect(ws.binaryType).toBe('arraybuffer')

    ws.message(
      JSON.stringify({
        type: 'ready',
        media: {
          codec: 'AMR-WB',
          payloadType: 104,
          clockRate: 16000,
          channels: 0,
          dtmfPayloadType: 101,
          dtmfClockRate: 8000,
          ptimeMillis: 20,
        },
      }),
    )

    expect(session.status.value).toBe('ready')
    expect(session.isReady.value).toBe(true)
    expect(session.mediaInfo.value?.codec).toBe('AMR-WB')
  })

  it('sends RTP packets only after the media handshake', () => {
    const session = useCallMediaSession(ref('modem-1'))
    const packet = new Uint8Array([1, 2, 3])

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    expect(session.sendRtpPacket(packet)).toBe(false)
    ws.message(
      JSON.stringify({
        type: 'ready',
        media: {
          codec: 'AMR',
          payloadType: 102,
          clockRate: 8000,
          channels: 0,
          dtmfPayloadType: 0,
          dtmfClockRate: 0,
          ptimeMillis: 20,
        },
      }),
    )

    expect(session.sendRtpPacket(packet)).toBe(true)
    expect(Array.from(new Uint8Array(ws.sent[0] as ArrayBuffer))).toEqual([1, 2, 3])
  })

  it('passes binary RTP packets to the caller callback', async () => {
    const onRtpPacket = vi.fn()
    const session = useCallMediaSession(ref('modem-1'), { onRtpPacket })

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    const packet = new Uint8Array([4, 5, 6]).buffer
    ws.message(packet)
    await Promise.resolve()

    expect(onRtpPacket).toHaveBeenCalledWith(packet)
  })

  it('enters error state for malformed handshake messages', () => {
    const session = useCallMediaSession(ref('modem-1'))

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    ws.message('{')

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Invalid media response')
  })

  it('enters error state when the media socket fails', () => {
    const session = useCallMediaSession(ref('modem-1'))

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    ws.error()

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call media connection failed')
  })

  it('enters closed state when the media socket closes before ready', () => {
    const session = useCallMediaSession(ref('modem-1'))

    session.connect('call-1')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return

    ws.closeFromServer()

    expect(session.status.value).toBe('closed')
    expect(session.errorMessage.value).toBe('')
  })
})

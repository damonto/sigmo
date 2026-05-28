import { ref } from 'vue'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useCallAudioSession } from '@/composables/useCallAudioSession'
import type { AmrCodecAdapter } from '@/lib/callMediaPipeline'

const codecFactory = vi.fn((): AmrCodecAdapter => ({
  decode: vi.fn(),
  encode: vi.fn(() => []),
}))

class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []

  binaryType = ''
  readyState = FakeWebSocket.OPEN
  onmessage: ((event: MessageEvent<unknown>) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this)
  }

  send() {}

  close() {
    this.readyState = 3
  }

  error() {
    this.onerror?.()
  }

  closeFromServer() {
    this.readyState = 3
    this.onclose?.()
  }
}

const audioContext = () =>
  ({
    state: 'running',
    currentTime: 0,
    close: vi.fn(),
  }) as unknown as AudioContext

describe('call audio session', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  it('does not open media when no AMR codec factory is available', async () => {
    const session = useCallAudioSession(ref('modem-1'))

    await expect(session.start('call-1')).resolves.toBe(false)

    expect(session.status.value).toBe('unsupported')
    expect(session.errorMessage.value).toBe('AMR codec module is not available')
    expect(session.mediaStatus.value).toBe('closed')
  })

  it('prepares microphone input without opening call media', async () => {
    const stop = vi.fn()
    const getUserMedia = vi.fn(async () => ({
      getTracks: () => [{ stop }],
    } as unknown as MediaStream))
    let audioState: AudioContextState = 'suspended'
    const audioContext = {
      get state() {
        return audioState
      },
      currentTime: 0,
      resume: vi.fn(async () => {
        audioState = 'running'
      }),
      close: vi.fn(),
    } as unknown as AudioContext
    const session = useCallAudioSession(ref('modem-1'), {
      codecFactory,
      deps: {
        createAudioContext: () => audioContext,
        getUserMedia,
      },
    })

    await expect(session.prepare()).resolves.toBe(true)

    expect(getUserMedia).toHaveBeenCalledWith({ audio: true })
    expect(session.status.value).toBe('idle')
    expect(session.mediaStatus.value).toBe('idle')
  })

  it('blocks call audio preparation when microphone capture is blocked', async () => {
    const audioContext = {
      state: 'running',
      currentTime: 0,
      close: vi.fn(),
    } as unknown as AudioContext
    const session = useCallAudioSession(ref('modem-1'), {
      codecFactory,
      deps: {
        createAudioContext: () => audioContext,
        getUserMedia: vi.fn(async () => {
          throw new Error('Audio Capture is not available')
        }),
      },
    })

    await expect(session.prepare()).resolves.toBe(false)

    expect(session.status.value).toBe('error')
    expect(session.mediaStatus.value).toBe('closed')
    expect(session.errorMessage.value).toBe('Audio Capture is not available')
  })

  it('does not open call media when microphone capture is blocked', async () => {
    const session = useCallAudioSession(ref('modem-1'), {
      codecFactory,
      deps: {
        createAudioContext: audioContext,
        getUserMedia: vi.fn(async () => {
          throw new Error('Audio Capture is not available')
        }),
      },
    })

    await expect(session.start('call-1')).resolves.toBe(false)

    expect(session.status.value).toBe('error')
    expect(session.mediaStatus.value).toBe('closed')
    expect(FakeWebSocket.instances).toHaveLength(0)
  })

  it('surfaces media websocket failures while starting call audio', async () => {
    const stop = vi.fn()
    const session = useCallAudioSession(ref('modem-1'), {
      codecFactory,
      deps: {
        createAudioContext: audioContext,
        getUserMedia: vi.fn(async () => ({
          getTracks: () => [{ stop }],
        } as unknown as MediaStream)),
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    expect(session.status.value).toBe('connecting')

    FakeWebSocket.instances[0]?.error()
    await nextTick()

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call media connection failed')
    expect(stop).toHaveBeenCalled()
  })

  it('closes call audio when the media websocket closes before the handshake', async () => {
    const stop = vi.fn()
    const session = useCallAudioSession(ref('modem-1'), {
      codecFactory,
      deps: {
        createAudioContext: audioContext,
        getUserMedia: vi.fn(async () => ({
          getTracks: () => [{ stop }],
        } as unknown as MediaStream)),
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)

    FakeWebSocket.instances[0]?.closeFromServer()
    await nextTick()

    expect(session.status.value).toBe('closed')
    expect(session.errorMessage.value).toBe('')
    expect(stop).toHaveBeenCalled()
  })
})

import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { reduceAudioStatus, useCallAudioSession } from '@/composables/useCallAudioSession'

const createWebRTCAnswer = vi.hoisted(() => vi.fn())

vi.mock('@/apis/call', () => ({
  useCallApi: () => ({
    createWebRTCAnswer,
  }),
}))

class FakePeerConnection {
  iceGatheringState: RTCIceGatheringState = 'complete'
  connectionState: RTCPeerConnectionState = 'new'
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  private listeners = new Map<string, Set<EventListener>>()

  addTrack = vi.fn()
  createOffer = vi.fn(async () => ({
    type: 'offer' as const,
    sdp: 'offer-sdp',
  }))
  setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
    this.localDescription = description
  })
  setRemoteDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
    this.remoteDescription = description
    this.connectionState = 'connected'
    this.onconnectionstatechange?.()
  })
  addEventListener = vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
    if (typeof listener !== 'function') return
    const listeners = this.listeners.get(type) ?? new Set<EventListener>()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  })
  removeEventListener = vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
    if (typeof listener !== 'function') return
    this.listeners.get(type)?.delete(listener)
  })
  close = vi.fn(() => {
    this.connectionState = 'closed'
  })

  setIceGatheringState(state: RTCIceGatheringState) {
    this.iceGatheringState = state
    this.dispatch('icegatheringstatechange')
  }

  setConnectionState(state: RTCPeerConnectionState) {
    this.connectionState = state
    this.onconnectionstatechange?.()
  }

  private dispatch(type: string) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(new Event(type))
    }
  }
}

const fakeTrack = (stop = vi.fn()) =>
  ({ enabled: true, stop }) as unknown as MediaStreamTrack & { enabled: boolean }

const fakeStream = (tracks: MediaStreamTrack[]) =>
  ({
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
  }) as unknown as MediaStream

const deferredStream = () => {
  let resolve!: (stream: MediaStream) => void
  const promise = new Promise<MediaStream>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

describe('call audio session', () => {
  beforeEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
    createWebRTCAnswer.mockResolvedValue({
      data: ref({ type: 'answer', sdp: 'answer-sdp' }),
    })
  })

  it('prepares microphone input without opening WebRTC media', async () => {
    const track = fakeTrack()
    const getUserMedia = vi.fn(async () => fakeStream([track]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        createPeerConnection: () => new FakePeerConnection() as unknown as RTCPeerConnection,
      },
    })

    await expect(session.prepare()).resolves.toBe(true)

    expect(getUserMedia).toHaveBeenCalledWith({
      audio: {
        autoGainControl: false,
        channelCount: 1,
        echoCancellation: true,
        noiseSuppression: true,
        sampleSize: 16,
      },
    })
    expect(createWebRTCAnswer).not.toHaveBeenCalled()
    expect(session.status.value).toBe('idle')
  })

  it('starts a WebRTC call audio session from a browser offer', async () => {
    const pc = new FakePeerConnection()
    const track = fakeTrack()
    const stream = fakeStream([track])
    const createPeerConnection = vi.fn(() => pc as unknown as RTCPeerConnection)
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => stream),
        createPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)

    expect(createPeerConnection).toHaveBeenCalledWith({
      iceServers: [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: 'stun:stun.cloudflare.com:3478' },
      ],
    })
    expect(pc.addTrack).toHaveBeenCalledWith(track, stream)
    expect(createWebRTCAnswer).toHaveBeenCalledWith('modem-1', 'call-1', {
      type: 'offer',
      sdp: 'offer-sdp',
    })
    expect(pc.setRemoteDescription).toHaveBeenCalledWith({
      type: 'answer',
      sdp: 'answer-sdp',
    })
    expect(session.status.value).toBe('ready')
  })

  it('toggles captured microphone tracks for call hold', async () => {
    const track = fakeTrack()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([track])),
        createPeerConnection: () => new FakePeerConnection() as unknown as RTCPeerConnection,
      },
    })

    await expect(session.prepare()).resolves.toBe(true)
    session.setInputEnabled(false)
    expect(track.enabled).toBe(false)

    session.setInputEnabled(true)
    expect(track.enabled).toBe(true)
  })

  it('reuses pending microphone preparation when a call starts', async () => {
    const capture = deferredStream()
    const pc = new FakePeerConnection()
    const getUserMedia = vi.fn(() => capture.promise)
    const createPeerConnection = vi.fn(() => pc as unknown as RTCPeerConnection)
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        createPeerConnection,
      },
    })

    const prepared = session.prepare()
    const started = session.start('call-1')
    const track = fakeTrack()
    const stream = fakeStream([track])
    capture.resolve(stream)

    await expect(prepared).resolves.toBe(true)
    await expect(started).resolves.toBe(true)

    expect(getUserMedia).toHaveBeenCalledOnce()
    expect(pc.addTrack).toHaveBeenCalledWith(track, stream)
    expect(session.status.value).toBe('ready')
  })

  it('stops captured microphone tracks when start is cancelled while capture is pending', async () => {
    const capture = deferredStream()
    const stop = vi.fn()
    const createPeerConnection = vi.fn(
      () => new FakePeerConnection() as unknown as RTCPeerConnection,
    )
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(() => capture.promise),
        createPeerConnection,
      },
    })

    const started = session.start('call-1')
    session.stop()
    capture.resolve(fakeStream([fakeTrack(stop)]))

    await expect(started).resolves.toBe(false)
    expect(stop).toHaveBeenCalled()
    expect(createPeerConnection).not.toHaveBeenCalled()
    expect(session.status.value).toBe('closed')
  })

  it('keeps a restarted session alive when earlier microphone capture was cancelled', async () => {
    const capture = deferredStream()
    const stop = vi.fn()
    const pc = new FakePeerConnection()
    const createPeerConnection = vi.fn(() => pc as unknown as RTCPeerConnection)
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(() => capture.promise),
        createPeerConnection,
      },
    })

    const firstStart = session.start('call-1')
    session.stop()
    const secondStart = session.start('call-1')
    capture.resolve(fakeStream([fakeTrack(stop)]))

    await expect(firstStart).resolves.toBe(false)
    await expect(secondStart).resolves.toBe(true)
    expect(stop).not.toHaveBeenCalled()
    expect(createPeerConnection).toHaveBeenCalledOnce()
    expect(session.status.value).toBe('ready')
  })

  it('times out when ICE gathering does not complete', async () => {
    vi.useFakeTimers()
    const pc = new FakePeerConnection()
    pc.iceGatheringState = 'gathering'
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    const started = session.start('call-1')
    await vi.advanceTimersByTimeAsync(20000)

    await expect(started).resolves.toBe(false)
    expect(createWebRTCAnswer).not.toHaveBeenCalled()
    expect(pc.close).toHaveBeenCalled()
    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('WebRTC ICE candidates are missing')
  })

  it('starts with gathered candidates when ICE gathering does not complete before the timeout', async () => {
    vi.useFakeTimers()
    const pc = new FakePeerConnection()
    pc.iceGatheringState = 'gathering'
    pc.setLocalDescription.mockImplementation(async (description: RTCSessionDescriptionInit) => {
      pc.localDescription = {
        ...description,
        sdp: `${description.sdp}\r\na=candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host\r\n`,
      }
    })
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    const started = session.start('call-1')
    await vi.advanceTimersByTimeAsync(20000)

    await expect(started).resolves.toBe(true)
    expect(createWebRTCAnswer).toHaveBeenCalledWith('modem-1', 'call-1', {
      type: 'offer',
      sdp: 'offer-sdp\r\na=candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host\r\n',
    })
    expect(session.status.value).toBe('ready')
  })

  it('keeps audio alive when a disconnected peer reconnects during the grace period', async () => {
    vi.useFakeTimers()
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    pc.setConnectionState('disconnected')
    await vi.advanceTimersByTimeAsync(4000)
    expect(session.status.value).toBe('ready')

    pc.setConnectionState('connected')
    await vi.advanceTimersByTimeAsync(1000)

    expect(session.status.value).toBe('ready')
    expect(session.errorMessage.value).toBe('')
  })

  it('fails audio when a disconnected peer stays down past the grace period', async () => {
    vi.useFakeTimers()
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    pc.setConnectionState('disconnected')
    await vi.advanceTimersByTimeAsync(5000)

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call audio connection failed')
    expect(pc.close).toHaveBeenCalled()
  })

  it('publishes the remote stream from WebRTC ontrack', async () => {
    const pc = new FakePeerConnection()
    const remote = fakeStream([])
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    pc.ontrack?.({ streams: [remote] } as unknown as RTCTrackEvent)

    expect(session.remoteStream.value).toBe(remote)
  })

  it('stops local tracks and closes the peer connection', async () => {
    const pc = new FakePeerConnection()
    const stop = vi.fn()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack(stop)])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    session.stop()

    expect(pc.close).toHaveBeenCalled()
    expect(stop).toHaveBeenCalled()
    expect(session.status.value).toBe('closed')
    expect(session.remoteStream.value).toBeNull()
  })

  it('surfaces microphone capture failures', async () => {
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => {
          throw new Error('Audio capture is blocked')
        }),
        createPeerConnection: () => new FakePeerConnection() as unknown as RTCPeerConnection,
      },
    })

    await expect(session.prepare()).resolves.toBe(false)

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Audio capture is blocked')
  })
})

describe('reduceAudioStatus', () => {
  it('maps session events to audio states', () => {
    expect(reduceAudioStatus('idle', { type: 'prepare' })).toBe('preparing')
    expect(reduceAudioStatus('preparing', { type: 'idle_after_prepare' })).toBe('idle')
    expect(reduceAudioStatus('preparing', { type: 'connect' })).toBe('connecting')
    expect(reduceAudioStatus('connecting', { type: 'ready' })).toBe('ready')
    expect(reduceAudioStatus('ready', { type: 'closed' })).toBe('closed')
    expect(reduceAudioStatus('ready', { type: 'error' })).toBe('error')
  })

  it('keeps an error state when the peer closes during cleanup', () => {
    expect(reduceAudioStatus('error', { type: 'peer_closed' })).toBe('error')
    expect(reduceAudioStatus('ready', { type: 'peer_closed' })).toBe('closed')
  })
})

import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { reduceAudioStatus, useCallAudioSession } from '@/composables/useCallAudioSession'

const getWebRTCICEServers = vi.hoisted(() => vi.fn())

vi.mock('@/apis/call', () => ({
  buildWebRTCSessionUrl: (id: string, callID: string) =>
    `ws://localhost/api/v1/modems/${id}/calls/${encodeURIComponent(callID)}/webrtc/sessions`,
  useCallApi: () => ({
    getWebRTCICEServers,
  }),
}))

class FakePeerConnection {
  iceGatheringState: RTCIceGatheringState = 'complete'
  connectionState: RTCPeerConnectionState = 'new'
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  onicecandidate: ((event: RTCPeerConnectionIceEvent) => void) | null = null
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  private listeners = new Map<string, Set<EventListener>>()

  sender = {
    replaceTrack: vi.fn(async () => {}),
  }
  addTrack = vi.fn(() => this.sender as unknown as RTCRtpSender)
  createOffer = vi.fn(async () => ({
    type: 'offer' as const,
    sdp: 'offer-sdp',
  }))
  setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
    this.localDescription = description
  })
  setRemoteDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
    this.remoteDescription = description
  })
  addIceCandidate = vi.fn(async () => {})
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

  emitIceCandidate(candidate: RTCIceCandidateInit) {
    this.onicecandidate?.({
      candidate: { toJSON: () => candidate },
    } as unknown as RTCPeerConnectionIceEvent)
  }

  private dispatch(type: string) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(new Event(type))
    }
  }
}

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static throwOnCandidate = false
  static onOffer: (socket: FakeWebSocket) => void = (socket) => {
    queueMicrotask(() => {
      socket.message({ type: 'answer', answer: { type: 'answer', sdp: 'answer-sdp' } })
    })
  }
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3

  readyState = FakeWebSocket.CONNECTING
  sent: unknown[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null

  constructor(public url: string) {
    FakeWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.readyState = FakeWebSocket.OPEN
      this.onopen?.()
    })
  }

  send(data: string) {
    const message = JSON.parse(data) as { type?: string }
    if (message.type === 'candidate' && FakeWebSocket.throwOnCandidate) {
      throw new Error('socket send failed')
    }
    this.sent.push(message)
    if (message.type === 'offer') {
      FakeWebSocket.onOffer(this)
    }
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const fakeTrack = (stop = vi.fn()) =>
  ({ enabled: true, stop }) as unknown as MediaStreamTrack & { enabled: boolean }

const fakeStream = (tracks: MediaStreamTrack[]) =>
  ({
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
  }) as unknown as MediaStream

const fakeDevice = (deviceId: string, kind: MediaDeviceKind, label = '') =>
  ({
    deviceId,
    groupId: '',
    kind,
    label,
    toJSON: () => ({}),
  }) as MediaDeviceInfo

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
    localStorage.clear()
    FakeWebSocket.instances = []
    FakeWebSocket.throwOnCandidate = false
    FakeWebSocket.onOffer = (socket) => {
      queueMicrotask(() => {
        socket.message({ type: 'answer', answer: { type: 'answer', sdp: 'answer-sdp' } })
      })
    }
    vi.stubGlobal('WebSocket', FakeWebSocket)
    getWebRTCICEServers.mockResolvedValue({
      data: ref({
        iceServers: [
          { urls: ['stun:stun.l.google.com:19302'] },
          { urls: ['stun:stun.cloudflare.com:3478'] },
        ],
      }),
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
    expect(FakeWebSocket.instances).toHaveLength(0)
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
        { urls: ['stun:stun.l.google.com:19302'] },
        { urls: ['stun:stun.cloudflare.com:3478'] },
      ],
    })
    expect(pc.addTrack).toHaveBeenCalledWith(track, stream)
    expect(FakeWebSocket.instances[0]?.url).toBe(
      'ws://localhost/api/v1/modems/modem-1/calls/call-1/webrtc/sessions',
    )
    expect(FakeWebSocket.instances[0]?.sent).toContainEqual({
      type: 'offer',
      offer: {
        type: 'offer',
        sdp: 'offer-sdp',
      },
    })
    expect(pc.setRemoteDescription).toHaveBeenCalledWith({
      type: 'answer',
      sdp: 'answer-sdp',
    })
    expect(session.status.value).toBe('connecting')
    pc.setConnectionState('connected')
    expect(session.status.value).toBe('ready')
  })

  it('sends local ICE candidates on the WebRTC signaling socket', async () => {
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    pc.emitIceCandidate({
      candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
      sdpMid: '0',
      sdpMLineIndex: 0,
    })

    expect(FakeWebSocket.instances[0]?.sent).toContainEqual({
      type: 'candidate',
      candidate: {
        candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
        sdpMid: '0',
        sdpMLineIndex: 0,
      },
    })
  })

  it('sends the offer before local ICE candidates emitted during local description setup', async () => {
    const pc = new FakePeerConnection()
    pc.setLocalDescription.mockImplementation(async (description: RTCSessionDescriptionInit) => {
      pc.localDescription = description
      pc.emitIceCandidate({
        candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
        sdpMid: '0',
        sdpMLineIndex: 0,
      })
    })
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)

    expect(FakeWebSocket.instances[0]?.sent).toEqual([
      {
        type: 'offer',
        offer: {
          type: 'offer',
          sdp: 'offer-sdp',
        },
      },
      {
        type: 'candidate',
        candidate: {
          candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
          sdpMid: '0',
          sdpMLineIndex: 0,
        },
      },
    ])
  })

  it('adds remote ICE candidates from the WebRTC signaling socket', async () => {
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    expect(FakeWebSocket.instances[0]?.url).toBe(
      'ws://localhost/api/v1/modems/modem-1/calls/call-1/webrtc/sessions',
    )
    FakeWebSocket.instances[0]?.message({
      type: 'candidate',
      candidate: {
        candidate: 'candidate:2 1 udp 2130706431 10.10.10.101 40040 typ host',
        sdpMid: '0',
        sdpMLineIndex: 0,
      },
    })

    expect(pc.addIceCandidate).toHaveBeenCalledWith({
      candidate: 'candidate:2 1 udp 2130706431 10.10.10.101 40040 typ host',
      sdpMid: '0',
      sdpMLineIndex: 0,
    })
  })

  it('buffers remote ICE candidates that arrive before the answer is applied', async () => {
    FakeWebSocket.onOffer = (socket) => {
      socket.message({
        type: 'candidate',
        candidate: {
          candidate: 'candidate:2 1 udp 2130706431 10.10.10.101 40040 typ host',
          sdpMid: '0',
          sdpMLineIndex: 0,
        },
      })
      socket.message({ type: 'answer', answer: { type: 'answer', sdp: 'answer-sdp' } })
    }
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)

    expect(pc.setRemoteDescription).toHaveBeenCalledWith({
      type: 'answer',
      sdp: 'answer-sdp',
    })
    expect(pc.addIceCandidate).toHaveBeenCalledWith({
      candidate: 'candidate:2 1 udp 2130706431 10.10.10.101 40040 typ host',
      sdpMid: '0',
      sdpMLineIndex: 0,
    })
    expect(pc.setRemoteDescription.mock.invocationCallOrder[0]).toBeLessThan(
      pc.addIceCandidate.mock.invocationCallOrder[0],
    )
  })

  it('fails setup when sending a trickled local ICE candidate fails before connection', async () => {
    FakeWebSocket.throwOnCandidate = true
    const pc = new FakePeerConnection()
    pc.setLocalDescription.mockImplementation(async (description: RTCSessionDescriptionInit) => {
      pc.localDescription = description
      pc.emitIceCandidate({
        candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
        sdpMid: '0',
        sdpMLineIndex: 0,
      })
    })
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(false)

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call audio signaling failed')
    expect(pc.close).toHaveBeenCalled()
  })

  it('fails a connecting session when sending a local ICE candidate fails after answer', async () => {
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    expect(session.status.value).toBe('connecting')
    FakeWebSocket.throwOnCandidate = true

    pc.emitIceCandidate({
      candidate: 'candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host',
      sdpMid: '0',
      sdpMLineIndex: 0,
    })

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call audio signaling failed')
    expect(pc.close).toHaveBeenCalled()
  })

  it('fails a connecting session when WebRTC signaling closes before connection', async () => {
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    expect(session.status.value).toBe('connecting')
    FakeWebSocket.instances[0]?.close()

    expect(session.status.value).toBe('error')
    expect(session.errorMessage.value).toBe('Call audio signaling closed')
    expect(pc.close).toHaveBeenCalled()
  })

  it('keeps ready audio alive when WebRTC signaling closes after connection', async () => {
    const consoleWarn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const pc = new FakePeerConnection()
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    pc.setConnectionState('connected')
    FakeWebSocket.instances[0]?.close()

    expect(session.status.value).toBe('ready')
    expect(session.errorMessage.value).toBe('')
    expect(consoleWarn).toHaveBeenCalledWith('[useCallAudioSession] WebRTC signaling closed')
    consoleWarn.mockRestore()
  })

  it('uses backend TURN servers when creating the WebRTC peer', async () => {
    getWebRTCICEServers.mockResolvedValue({
      data: ref({
        iceServers: [
          {
            urls: ['turn:turn.cloudflare.com:3478?transport=udp'],
            username: 'sigmo',
            credential: 'secret',
          },
        ],
      }),
    })
    const pc = new FakePeerConnection()
    const createPeerConnection = vi.fn(() => pc as unknown as RTCPeerConnection)
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)

    expect(getWebRTCICEServers).toHaveBeenCalledOnce()
    expect(createPeerConnection).toHaveBeenCalledWith({
      iceServers: [
        {
          urls: ['turn:turn.cloudflare.com:3478?transport=udp'],
          username: 'sigmo',
          credential: 'secret',
        },
      ],
    })
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

  it('uses the saved microphone when preparing audio', async () => {
    localStorage.setItem('sigmo:call-audio-input-device', 'mic-saved')
    const getUserMedia = vi.fn(async () => fakeStream([fakeTrack()]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        enumerateDevices: vi.fn(async () => [
          fakeDevice('mic-saved', 'audioinput', 'Saved microphone'),
        ]),
      },
    })

    await expect(session.prepare()).resolves.toBe(true)

    expect(getUserMedia).toHaveBeenCalledWith({
      audio: {
        autoGainControl: false,
        channelCount: 1,
        deviceId: { exact: 'mic-saved' },
        echoCancellation: true,
        noiseSuppression: true,
        sampleSize: 16,
      },
    })
    expect(session.selectedInputDeviceID.value).toBe('mic-saved')
  })

  it('keeps a saved microphone when enumeration runs before capture permission', async () => {
    localStorage.setItem('sigmo:call-audio-input-device', 'mic-saved')
    const getUserMedia = vi.fn(async () => fakeStream([fakeTrack()]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        enumerateDevices: vi.fn(async () => [fakeDevice('default', 'audioinput')]),
      },
    })

    await session.refreshDevices()

    expect(getUserMedia).not.toHaveBeenCalled()
    expect(session.selectedInputDeviceID.value).toBe('mic-saved')
    expect(localStorage.getItem('sigmo:call-audio-input-device')).toBe('mic-saved')
  })

  it('falls back to the system microphone when the saved device is missing', async () => {
    localStorage.setItem('sigmo:call-audio-input-device', 'mic-missing')
    const getUserMedia = vi
      .fn()
      .mockRejectedValueOnce(new DOMException('missing', 'NotFoundError'))
      .mockResolvedValueOnce(fakeStream([fakeTrack()]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: { getUserMedia },
    })

    await expect(session.prepare()).resolves.toBe(true)

    expect(getUserMedia).toHaveBeenCalledTimes(2)
    expect(getUserMedia.mock.calls[1]?.[0]).toEqual({
      audio: {
        autoGainControl: false,
        channelCount: 1,
        echoCancellation: true,
        noiseSuppression: true,
        sampleSize: 16,
      },
    })
    expect(session.selectedInputDeviceID.value).toBe('')
    expect(session.deviceNotice.value).toBe('device_fallback')
    expect(localStorage.getItem('sigmo:call-audio-input-device')).toBeNull()
  })

  it('reacquires the system microphone when its current track has ended', async () => {
    let readyState: MediaStreamTrackState = 'live'
    const firstStop = vi.fn()
    const firstTrack = fakeTrack(firstStop)
    Object.defineProperty(firstTrack, 'readyState', {
      configurable: true,
      get: () => readyState,
    })
    const secondTrack = fakeTrack()
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream([firstTrack]))
      .mockResolvedValueOnce(fakeStream([secondTrack]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        enumerateDevices: vi.fn(async () => []),
      },
    })

    await session.prepare()
    readyState = 'ended'
    await session.refreshDevices()

    expect(getUserMedia).toHaveBeenCalledTimes(2)
    expect(firstStop).toHaveBeenCalled()
    expect(session.selectedInputDeviceID.value).toBe('')
    expect(session.deviceNotice.value).toBe('device_fallback')
  })

  it('reports an automatic microphone fallback failure', async () => {
    localStorage.setItem('sigmo:call-audio-input-device', 'mic-saved')
    const firstStop = vi.fn()
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream([fakeTrack(firstStop)]))
      .mockRejectedValueOnce(new Error('default microphone unavailable'))
    const enumerateDevices = vi
      .fn()
      .mockResolvedValueOnce([fakeDevice('mic-saved', 'audioinput')])
      .mockResolvedValueOnce([])
    const session = useCallAudioSession(ref('modem-1'), {
      deps: { getUserMedia, enumerateDevices },
    })

    await session.prepare()
    await session.refreshDevices()

    expect(firstStop).not.toHaveBeenCalled()
    expect(session.selectedInputDeviceID.value).toBe('mic-saved')
    expect(session.deviceNotice.value).toBe('input_switch_failed')
  })

  it('replaces the active microphone track before stopping the previous stream', async () => {
    const pc = new FakePeerConnection()
    const firstStop = vi.fn()
    const firstTrack = fakeTrack(firstStop)
    const secondTrack = fakeTrack()
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream([firstTrack]))
      .mockResolvedValueOnce(fakeStream([secondTrack]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    session.setInputEnabled(false)
    await expect(session.selectInputDevice('mic-2')).resolves.toBe(true)

    expect(pc.sender.replaceTrack).toHaveBeenCalledWith(secondTrack)
    expect(secondTrack.enabled).toBe(false)
    expect(firstStop).toHaveBeenCalled()
    expect(firstStop.mock.invocationCallOrder[0]).toBeGreaterThan(
      pc.sender.replaceTrack.mock.invocationCallOrder[0] ?? 0,
    )
    expect(session.selectedInputDeviceID.value).toBe('mic-2')
    expect(localStorage.getItem('sigmo:call-audio-input-device')).toBe('mic-2')
  })

  it('keeps the previous microphone when replaceTrack fails', async () => {
    const pc = new FakePeerConnection()
    const firstStop = vi.fn()
    const secondStop = vi.fn()
    const firstTrack = fakeTrack(firstStop)
    pc.sender.replaceTrack.mockRejectedValueOnce(new Error('replace failed'))
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream([firstTrack]))
      .mockResolvedValueOnce(fakeStream([fakeTrack(secondStop)]))
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia,
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    await expect(session.selectInputDevice('mic-2')).resolves.toBe(false)
    session.setInputEnabled(false)

    expect(firstStop).not.toHaveBeenCalled()
    expect(secondStop).toHaveBeenCalled()
    expect(firstTrack.enabled).toBe(false)
    expect(session.selectedInputDeviceID.value).toBe('')
    expect(session.deviceNotice.value).toBe('input_switch_failed')
  })

  it('stops a microphone switch that finishes after the audio session closes', async () => {
    const capture = deferredStream()
    const nextStop = vi.fn()
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream([fakeTrack()]))
      .mockReturnValueOnce(capture.promise)
    const session = useCallAudioSession(ref('modem-1'), {
      deps: { getUserMedia },
    })

    await session.prepare()
    const switched = session.selectInputDevice('mic-2')
    session.stop()
    capture.resolve(fakeStream([fakeTrack(nextStop)]))

    await expect(switched).resolves.toBe(false)
    expect(nextStop).toHaveBeenCalled()
    expect(session.selectedInputDeviceID.value).toBe('')
  })

  it('applies and persists the selected audio output', async () => {
    const setSinkId = vi.fn(async () => {})
    const session = useCallAudioSession(ref('modem-1'))
    const audio = { setSinkId } as unknown as HTMLAudioElement

    await expect(session.bindOutputElement(audio)).resolves.toBe(true)
    await expect(session.selectOutputDevice('speaker-1')).resolves.toBe(true)

    expect(session.outputSelectionSupported.value).toBe(true)
    expect(setSinkId).toHaveBeenCalledWith('speaker-1')
    expect(session.selectedOutputDeviceID.value).toBe('speaker-1')
    expect(localStorage.getItem('sigmo:call-audio-output-device')).toBe('speaker-1')
  })

  it('keeps the previous output when setSinkId fails', async () => {
    const setSinkId = vi
      .fn()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new DOMException('blocked', 'NotAllowedError'))
    const session = useCallAudioSession(ref('modem-1'))

    await session.bindOutputElement({ setSinkId } as unknown as HTMLAudioElement)
    await expect(session.selectOutputDevice('speaker-1')).resolves.toBe(true)
    await expect(session.selectOutputDevice('speaker-2')).resolves.toBe(false)

    expect(session.selectedOutputDeviceID.value).toBe('speaker-1')
    expect(session.deviceNotice.value).toBe('output_switch_failed')
    expect(localStorage.getItem('sigmo:call-audio-output-device')).toBe('speaker-1')
  })

  it('falls back to the system output when a saved device disappears', async () => {
    localStorage.setItem('sigmo:call-audio-output-device', 'speaker-missing')
    const setSinkId = vi.fn(async () => {})
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        enumerateDevices: vi.fn(async () => [
          fakeDevice('speaker-current', 'audiooutput', 'Current speaker'),
        ]),
      },
    })

    await session.bindOutputElement({ setSinkId } as unknown as HTMLAudioElement)
    await session.refreshDevices()

    expect(setSinkId).toHaveBeenNthCalledWith(1, 'speaker-missing')
    expect(setSinkId).toHaveBeenNthCalledWith(2, '')
    expect(session.selectedOutputDeviceID.value).toBe('')
    expect(session.deviceNotice.value).toBe('device_fallback')
    expect(localStorage.getItem('sigmo:call-audio-output-device')).toBeNull()
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
    expect(session.status.value).toBe('connecting')
    pc.setConnectionState('connected')
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
    expect(session.status.value).toBe('connecting')
    pc.setConnectionState('connected')
    expect(session.status.value).toBe('ready')
  })

  it('sends the offer over signaling without waiting for ICE gathering to complete', async () => {
    const pc = new FakePeerConnection()
    pc.iceGatheringState = 'gathering'
    const session = useCallAudioSession(ref('modem-1'), {
      deps: {
        getUserMedia: vi.fn(async () => fakeStream([fakeTrack()])),
        createPeerConnection: () => pc as unknown as RTCPeerConnection,
      },
    })

    await expect(session.start('call-1')).resolves.toBe(true)
    expect(FakeWebSocket.instances[0]?.sent).toContainEqual({
      type: 'offer',
      offer: {
        type: 'offer',
        sdp: 'offer-sdp',
      },
    })
    expect(session.status.value).toBe('connecting')
    pc.setConnectionState('connected')
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
    pc.setConnectionState('connected')
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
    pc.setConnectionState('connected')
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

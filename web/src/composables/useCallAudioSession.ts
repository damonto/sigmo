import { computed, getCurrentInstance, onBeforeUnmount, ref, shallowRef, type Ref } from 'vue'

import { buildWebRTCSessionUrl, useCallApi } from '@/apis/call'
import type { WebRTCSessionDescriptionPayload, WebRTCSignalMessage } from '@/types/call'

export type AudioStatus = 'idle' | 'preparing' | 'connecting' | 'ready' | 'closed' | 'error'

export type AudioStatusEvent =
  | { type: 'prepare' }
  | { type: 'idle_after_prepare' }
  | { type: 'connect' }
  | { type: 'ready' }
  | { type: 'closed' }
  | { type: 'error' }
  | { type: 'peer_closed' }

type AudioDeps = {
  createPeerConnection?: (configuration: RTCConfiguration) => RTCPeerConnection
  getIceServers?: () => Promise<RTCIceServer[]>
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
}

type Options = {
  deps?: AudioDeps
}

const microphoneConstraints: MediaTrackConstraints = {
  autoGainControl: false,
  channelCount: 1,
  echoCancellation: true,
  noiseSuppression: true,
  sampleSize: 16,
}

const disconnectedGraceMs = 5000

export const reduceAudioStatus = (current: AudioStatus, event: AudioStatusEvent): AudioStatus => {
  switch (event.type) {
    case 'prepare':
      return 'preparing'
    case 'idle_after_prepare':
      return current === 'preparing' ? 'idle' : current
    case 'connect':
      return 'connecting'
    case 'ready':
      return 'ready'
    case 'closed':
      return 'closed'
    case 'error':
      return 'error'
    case 'peer_closed':
      return current === 'error' ? 'error' : 'closed'
  }
}

export const useCallAudioSession = (modemId: Ref<string>, options: Options = {}) => {
  const status = ref<AudioStatus>('idle')
  const mediaStatus = computed(() => status.value)
  const errorMessage = ref('')
  const remoteStream = shallowRef<MediaStream | null>(null)

  const calls = useCallApi()

  let pc: RTCPeerConnection | null = null
  let signalWS: WebSocket | null = null
  let stream: MediaStream | null = null
  let inputPromise: Promise<MediaStream> | null = null
  let preparePromise: Promise<boolean> | null = null
  let sessionAbort: AbortController | null = null
  let connectionLossTimer: ReturnType<typeof setTimeout> | null = null
  let activeCallID = ''
  let activeModemID = ''
  let offerSent = false
  let pendingLocalSignals: WebRTCSignalMessage[] = []
  let pendingRemoteICECandidates: RTCIceCandidateInit[] = []
  let answerResolve: ((answer: RTCSessionDescriptionInit) => void) | null = null
  let answerReject: ((err: Error) => void) | null = null

  const isReady = computed(() => status.value === 'ready')

  const applyStatus = (event: AudioStatusEvent) => {
    status.value = reduceAudioStatus(status.value, event)
  }

  const fail = (err: unknown) => {
    errorMessage.value = errorText(err)
    applyStatus({ type: 'error' })
    cleanup()
  }

  const isCurrentSession = (controller: AbortController) =>
    sessionAbort === controller && !controller.signal.aborted

  const openAudioInput = async () => {
    if (stream) return stream
    if (inputPromise) return await inputPromise
    inputPromise = (async () => {
      const getUserMedia =
        options.deps?.getUserMedia ??
        navigator.mediaDevices?.getUserMedia.bind(navigator.mediaDevices)
      if (!getUserMedia) {
        throw new Error('Microphone capture is not available')
      }
      const nextStream = await getUserMedia({ audio: microphoneConstraints })
      stream = nextStream
      return nextStream
    })()
    try {
      return await inputPromise
    } finally {
      inputPromise = null
    }
  }

  const ensureAudioInput = async (controller: AbortController) => {
    const signal = controller.signal
    if (signal.aborted) throw newAbortError()
    const currentStream = await openAudioInput()
    if (signal.aborted) {
      if (stream === currentStream && (sessionAbort === controller || sessionAbort === null)) {
        stopStream(currentStream)
        stream = null
      }
      throw newAbortError()
    }
    return currentStream
  }

  const createPeerConnection = (configuration: RTCConfiguration) =>
    options.deps?.createPeerConnection?.(configuration) ?? new RTCPeerConnection(configuration)

  const getIceServers = async () => {
    if (options.deps?.getIceServers) {
      const servers = await options.deps.getIceServers()
      return servers.length > 0 ? servers : defaultIceServers
    }
    const { data } = await calls.getWebRTCICEServers()
    const servers = data.value?.iceServers ?? []
    return servers.length > 0 ? servers : defaultIceServers
  }

  const clearConnectionLossTimer = () => {
    if (!connectionLossTimer) return
    clearTimeout(connectionLossTimer)
    connectionLossTimer = null
  }

  const prepare = async () => {
    if (preparePromise) return await preparePromise
    errorMessage.value = ''
    applyStatus({ type: 'prepare' })
    preparePromise = (async () => {
      try {
        await openAudioInput()
        if (!activeCallID && status.value === 'preparing') {
          applyStatus({ type: 'idle_after_prepare' })
        }
        return true
      } catch (err) {
        fail(err)
        return false
      } finally {
        preparePromise = null
      }
    })()
    return await preparePromise
  }

  const start = async (callID: string) => {
    if (!callID) return false
    cleanup(true)
    const nextAbort = new AbortController()
    sessionAbort = nextAbort
    activeModemID = modemId.value
    activeCallID = callID
    errorMessage.value = ''
    applyStatus({ type: 'prepare' })

    try {
      const localStream = await ensureAudioInput(nextAbort)
      if (!isCurrentSession(nextAbort)) return false
      applyStatus({ type: 'connect' })
      const iceServers = await getIceServers()
      if (!isCurrentSession(nextAbort)) return false
      const nextPC = createPeerConnection({ iceServers })
      pc = nextPC
      const signal = await openWebRTCSignaling(nextPC, activeModemID, callID, nextAbort)
      if (!isCurrentSession(nextAbort)) return false
      nextPC.onicecandidate = (event) => {
        if (pc !== nextPC || !event.candidate) return
        queueOrSendWebRTCSignal({ type: 'candidate', candidate: event.candidate.toJSON() })
      }
      nextPC.ontrack = (event) => {
        remoteStream.value = event.streams[0] ?? new MediaStream([event.track])
      }
      nextPC.onconnectionstatechange = () => {
        if (pc !== nextPC) return
        switch (nextPC.connectionState) {
          case 'connected':
            clearConnectionLossTimer()
            applyStatus({ type: 'ready' })
            break
          case 'failed':
            clearConnectionLossTimer()
            fail(new Error('Call audio connection failed'))
            break
          case 'disconnected':
            if (connectionLossTimer) return
            connectionLossTimer = setTimeout(() => {
              connectionLossTimer = null
              if (pc === nextPC && nextPC.connectionState === 'disconnected') {
                fail(new Error('Call audio connection failed'))
              }
            }, disconnectedGraceMs)
            break
          case 'closed':
            clearConnectionLossTimer()
            applyStatus({ type: 'peer_closed' })
            break
        }
      }
      for (const track of localStream.getAudioTracks()) {
        nextPC.addTrack(track, localStream)
      }
      const offer = await nextPC.createOffer()
      if (!isCurrentSession(nextAbort)) return false
      await nextPC.setLocalDescription(offer)
      if (pc !== nextPC || !isCurrentSession(nextAbort)) return false
      const localDescription = nextPC.localDescription
      if (!localDescription) {
        throw new Error('WebRTC offer is missing a local description')
      }
      const answerPromise = waitForWebRTCAnswer(signal, nextAbort)
      sendWebRTCSignal({
        type: 'offer',
        offer: {
          type: 'offer',
          sdp: localDescription.sdp,
        },
      })
      offerSent = true
      flushLocalSignals()
      const answer = await answerPromise
      if (!isCurrentSession(nextAbort)) return false
      await nextPC.setRemoteDescription(answer)
      if (!isCurrentSession(nextAbort)) return false
      await flushRemoteICECandidates(nextPC)
      return true
    } catch (err) {
      if (isAbortError(err)) return false
      fail(err)
      return false
    }
  }

  const cleanup = (keepInput = false) => {
    sessionAbort?.abort()
    sessionAbort = null
    clearConnectionLossTimer()
    if (pc) {
      pc.onicecandidate = null
      pc.ontrack = null
      pc.onconnectionstatechange = null
      pc.close()
      pc = null
    }
    closeWebRTCSignaling()
    activeModemID = ''
    offerSent = false
    pendingLocalSignals = []
    pendingRemoteICECandidates = []
    answerResolve = null
    answerReject = null
    remoteStream.value = null
    if (!keepInput && stream) {
      stopStream(stream)
      stream = null
    }
  }

  const openWebRTCSignaling = (
    targetPC: RTCPeerConnection,
    id: string,
    callID: string,
    controller: AbortController,
  ) =>
    new Promise<WebSocket>((resolve, reject) => {
      closeWebRTCSignaling()
      const conn = new WebSocket(buildWebRTCSessionUrl(id, callID))
      signalWS = conn
      let settled = false
      const settle = (fn: () => void) => {
        if (settled) return
        settled = true
        controller.signal.removeEventListener('abort', abort)
        fn()
      }
      const abort = () => {
        closeWebRTCSignaling()
        settle(() => reject(newAbortError()))
      }
      conn.onopen = () => {
        settle(() => {
          flushLocalSignals()
          resolve(conn)
        })
      }
      conn.onerror = () => {
        if (!settled) {
          settle(() => reject(new Error('Call audio signaling failed')))
        }
      }
      conn.onclose = () => {
        if (!settled) {
          if (signalWS === conn) {
            signalWS = null
          }
          settle(() => reject(new Error('Call audio signaling closed')))
          return
        }
        if (signalWS !== conn || pc !== targetPC) return
        if (status.value !== 'ready') {
          fail(new Error('Call audio signaling closed'))
          return
        }
        signalWS = null
        console.warn('[useCallAudioSession] WebRTC signaling closed')
      }
      conn.onmessage = (event) => {
        if (signalWS !== conn || pc !== targetPC) return
        handleWebRTCSignalMessage(targetPC, event.data)
      }
      controller.signal.addEventListener('abort', abort, { once: true })
    })

  const queueOrSendWebRTCSignal = (message: WebRTCSignalMessage) => {
    if (message.type === 'candidate' && !offerSent) {
      pendingLocalSignals.push(message)
      return
    }
    try {
      sendWebRTCSignal(message)
    } catch (err) {
      handleWebRTCSignalSendError(err)
    }
  }

  const sendWebRTCSignal = (message: WebRTCSignalMessage) => {
    if (!signalWS || signalWS.readyState !== WebSocket.OPEN) {
      pendingLocalSignals.push(message)
      return
    }
    signalWS.send(JSON.stringify(message))
  }

  const flushLocalSignals = () => {
    const messages = pendingLocalSignals
    pendingLocalSignals = []
    for (const message of messages) {
      try {
        sendWebRTCSignal(message)
      } catch (err) {
        handleWebRTCSignalSendError(err)
        return
      }
    }
  }

  const waitForWebRTCAnswer = (conn: WebSocket, controller: AbortController) =>
    new Promise<RTCSessionDescriptionInit>((resolve, reject) => {
      const abort = () => {
        cleanup()
        reject(newAbortError())
      }
      const cleanup = () => {
        controller.signal.removeEventListener('abort', abort)
        if (answerResolve === done) answerResolve = null
        if (answerReject === failAnswer) answerReject = null
      }
      const done = (answer: RTCSessionDescriptionInit) => {
        cleanup()
        resolve(answer)
      }
      const failAnswer = (err: Error) => {
        cleanup()
        reject(err)
      }
      if (conn.readyState !== WebSocket.OPEN) {
        reject(new Error('Call audio signaling closed'))
        return
      }
      answerResolve = done
      answerReject = failAnswer
      controller.signal.addEventListener('abort', abort, { once: true })
    })

  const handleWebRTCSignalMessage = (targetPC: RTCPeerConnection, data: unknown) => {
    const message = parseWebRTCSignalMessage(data)
    if (!message) return
    switch (message.type) {
      case 'answer':
        answerResolve?.(message.answer)
        break
      case 'candidate':
        if (!targetPC.remoteDescription) {
          pendingRemoteICECandidates.push(message.candidate)
          return
        }
        void addRemoteICECandidate(targetPC, message.candidate)
        break
      case 'error':
        answerReject?.(new Error(message.message || 'Call audio signaling failed'))
        break
      case 'offer':
        break
    }
  }

  const flushRemoteICECandidates = async (targetPC: RTCPeerConnection) => {
    const candidates = pendingRemoteICECandidates
    pendingRemoteICECandidates = []
    for (const candidate of candidates) {
      await addRemoteICECandidate(targetPC, candidate)
    }
  }

  const addRemoteICECandidate = async (
    targetPC: RTCPeerConnection,
    candidate: RTCIceCandidateInit,
  ) => {
    try {
      await targetPC.addIceCandidate(candidate)
    } catch (err) {
      console.warn('[useCallAudioSession] add WebRTC ICE candidate:', err)
    }
  }

  const handleWebRTCSignalSendError = (err: unknown) => {
    if (status.value === 'ready') {
      console.warn('[useCallAudioSession] send WebRTC signal:', err)
      return
    }
    fail(new Error('Call audio signaling failed'))
  }

  const closeWebRTCSignaling = () => {
    if (!signalWS) return
    const current = signalWS
    signalWS = null
    current.onopen = null
    current.onclose = null
    current.onerror = null
    current.onmessage = null
    current.close()
  }

  const stop = () => {
    cleanup()
    activeCallID = ''
    errorMessage.value = ''
    applyStatus({ type: 'closed' })
  }

  const setInputEnabled = (enabled: boolean) => {
    if (!stream) return
    for (const track of stream.getAudioTracks()) {
      track.enabled = enabled
    }
  }

  if (getCurrentInstance()) {
    onBeforeUnmount(stop)
  }

  return {
    status,
    mediaStatus,
    isReady,
    errorMessage,
    remoteStream,
    prepare,
    start,
    stop,
    setInputEnabled,
  }
}

const errorText = (err: unknown) => {
  if (err instanceof Error && err.message.trim()) return err.message
  if (typeof err === 'string' && err.trim()) return err
  return 'Call audio is not available'
}

const stopStream = (stream: MediaStream) => {
  for (const track of stream.getTracks()) {
    track.stop()
  }
}

const defaultIceServers: RTCIceServer[] = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun.cloudflare.com:3478' },
]

const newAbortError = () => {
  const err = new Error('WebRTC audio session was cancelled')
  err.name = 'AbortError'
  return err
}

const parseWebRTCSignalMessage = (data: unknown): WebRTCSignalMessage | null => {
  if (typeof data !== 'string') return null
  try {
    const parsed = JSON.parse(data) as Record<string, unknown>
    switch (parsed.type) {
      case 'answer':
        {
          const answer = asSessionDescription(parsed.answer)
          return answer ? { type: 'answer', answer } : null
        }
      case 'candidate':
        {
          const candidate = asICECandidate(parsed.candidate)
          return candidate ? { type: 'candidate', candidate } : null
        }
      case 'error':
        return { type: 'error', message: String(parsed.message ?? '') }
      default:
        return null
    }
  } catch {
    return null
  }
}

const asSessionDescription = (value: unknown): WebRTCSessionDescriptionPayload | null => {
  if (!value || typeof value !== 'object') return null
  const record = value as Record<string, unknown>
  if ((record.type !== 'offer' && record.type !== 'answer') || typeof record.sdp !== 'string') {
    return null
  }
  return { type: record.type, sdp: record.sdp }
}

const asICECandidate = (value: unknown): RTCIceCandidateInit | null => {
  if (!value || typeof value !== 'object') return null
  const record = value as RTCIceCandidateInit
  return typeof record.candidate === 'string' && record.candidate
    ? {
        candidate: record.candidate,
        sdpMid: record.sdpMid,
        sdpMLineIndex: record.sdpMLineIndex,
        usernameFragment: record.usernameFragment,
      }
    : null
}

const isAbortError = (err: unknown) => err instanceof Error && err.name === 'AbortError'

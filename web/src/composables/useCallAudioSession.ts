import { computed, getCurrentInstance, onBeforeUnmount, ref, shallowRef, type Ref } from 'vue'

import { useCallApi } from '@/apis/call'

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

const iceGatheringTimeoutMs = 20000
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
  let stream: MediaStream | null = null
  let inputPromise: Promise<MediaStream> | null = null
  let preparePromise: Promise<boolean> | null = null
  let sessionAbort: AbortController | null = null
  let connectionLossTimer: ReturnType<typeof setTimeout> | null = null
  let activeCallID = ''

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
    activeCallID = callID
    errorMessage.value = ''
    applyStatus({ type: 'prepare' })

    try {
      const localStream = await ensureAudioInput(nextAbort)
      if (!isCurrentSession(nextAbort)) return false
      applyStatus({ type: 'connect' })
      const nextPC = createPeerConnection({ iceServers: defaultIceServers })
      pc = nextPC
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
      if (!isCurrentSession(nextAbort)) return false
      await waitForIceGathering(nextPC, nextAbort.signal)
      if (pc !== nextPC || !isCurrentSession(nextAbort)) return false
      const localDescription = nextPC.localDescription
      if (!localDescription) {
        throw new Error('WebRTC offer is missing a local description')
      }
      const { data } = await calls.createWebRTCAnswer(modemId.value, callID, {
        type: 'offer',
        sdp: localDescription.sdp,
      })
      const answer = data.value
      if (!answer) {
        throw new Error('Call audio answer is empty')
      }
      if (!isCurrentSession(nextAbort)) return false
      await nextPC.setRemoteDescription(answer)
      if (pc === nextPC && status.value === 'connecting') {
        applyStatus({ type: 'ready' })
      }
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
      pc.ontrack = null
      pc.onconnectionstatechange = null
      pc.close()
      pc = null
    }
    remoteStream.value = null
    if (!keepInput && stream) {
      stopStream(stream)
      stream = null
    }
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

const waitForIceGathering = (pc: RTCPeerConnection, signal: AbortSignal) => {
  if (signal.aborted) {
    return Promise.reject(newAbortError())
  }
  if (pc.iceGatheringState === 'complete') {
    return Promise.resolve()
  }
  return new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(() => {
      cleanup()
      const sdp = pc.localDescription?.sdp ?? ''
      if (hasIceCandidate(sdp)) {
        resolve()
        return
      }
      reject(new Error('WebRTC ICE candidates are missing'))
    }, iceGatheringTimeoutMs)
    const abort = () => {
      cleanup()
      reject(newAbortError())
    }
    const done = () => {
      if (pc.iceGatheringState !== 'complete') return
      cleanup()
      resolve()
    }
    const cleanup = () => {
      clearTimeout(timeout)
      pc.removeEventListener('icegatheringstatechange', done)
      signal.removeEventListener('abort', abort)
    }
    pc.addEventListener('icegatheringstatechange', done)
    signal.addEventListener('abort', abort, { once: true })
  })
}

const newAbortError = () => {
  const err = new Error('WebRTC audio session was cancelled')
  err.name = 'AbortError'
  return err
}

const hasIceCandidate = (sdp: string) => /^a=candidate:/m.test(sdp)

const isAbortError = (err: unknown) => err instanceof Error && err.name === 'AbortError'

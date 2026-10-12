import { computed, onScopeDispose, readonly, ref, shallowRef, watch, type Ref } from 'vue'

interface RecordingOptions {
  callId: Readonly<Ref<string>>
  audioReady: Readonly<Ref<boolean>>
  localStream: Readonly<Ref<MediaStream | null>>
  remoteStream: Readonly<Ref<MediaStream | null>>
}

interface RecordingFile {
  blob: Blob
  name: string
}

interface RecordingSession {
  callId: string
  context: AudioContext
  output: MediaStreamAudioDestinationNode | null
  mix: GainNode | null
  sources: Map<MediaStream, MediaStreamAudioSourceNode>
  recorder: MediaRecorder | null
  chunks: Blob[]
  startedAt: Date
  timer: ReturnType<typeof setInterval> | null
}

const RECORDING_TYPES = [
  'audio/webm;codecs=opus',
  'audio/webm',
  'audio/mp4;codecs=mp4a.40.2',
  'audio/mp4',
  'audio/ogg;codecs=opus',
]
const DOWNLOAD_URL_LIFETIME_MS = 60_000

/**
 * useCallRecording records a connected browser call within the current Vue scope.
 * It borrows the call's tracks and only owns the mixer, recorder, and temporary file.
 */
export function useCallRecording(options: RecordingOptions) {
  const status = ref<'idle' | 'starting' | 'recording' | 'stopping'>('idle')
  const error = ref<
    '' | 'startFailed' | 'recordingFailed' | 'finalizeFailed' | 'empty' | 'downloadFailed'
  >('')
  const elapsedSeconds = ref(0)
  const remoteAudioAvailable = ref(false)
  const latestFile = shallowRef<RecordingFile | null>(null)
  const mimeType = computed(() => {
    if (typeof AudioContext === 'undefined' || typeof MediaRecorder === 'undefined') return ''
    return RECORDING_TYPES.find((type) => MediaRecorder.isTypeSupported(type)) ?? ''
  })
  const isSupported = computed(() => !!mimeType.value)
  const canStart = computed(
    () =>
      status.value === 'idle' &&
      isSupported.value &&
      !!options.callId.value &&
      options.audioReady.value &&
      remoteAudioAvailable.value,
  )
  const isRecording = computed(() => status.value === 'recording')
  const isBusy = computed(() => status.value === 'starting' || status.value === 'stopping')
  const durationLabel = computed(() => {
    const seconds = elapsedSeconds.value
    return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
  })
  let session: RecordingSession | null = null
  let disposed = false

  const clearTimer = (recording: RecordingSession) => {
    if (recording.timer !== null) clearInterval(recording.timer)
    recording.timer = null
  }

  const release = (recording: RecordingSession) => {
    clearTimer(recording)
    recording.context.onstatechange = null
    if (recording.recorder) {
      recording.recorder.ondataavailable = null
      recording.recorder.onstop = null
      recording.recorder.onerror = null
    }
    for (const source of recording.sources.values()) source.disconnect()
    recording.sources.clear()
    recording.mix?.disconnect()
    // Only the mixer's output belongs to us. Stopping an input would interrupt the call.
    for (const track of recording.output?.stream.getTracks() ?? []) track.stop()
    if (recording.context.state !== 'closed') {
      void recording.context.close().catch((err: unknown) => {
        console.warn('[useCallRecording] close recording audio context:', err)
      })
    }
    recording.chunks = []
  }

  const downloadFile = (file: RecordingFile) => {
    try {
      const url = URL.createObjectURL(file.blob)
      const link = document.createElement('a')
      link.href = url
      link.download = file.name
      try {
        document.body.append(link)
        link.click()
      } finally {
        link.remove()
        // Downloads may start after the click handler (including during component disposal).
        setTimeout(() => URL.revokeObjectURL(url), DOWNLOAD_URL_LIFETIME_MS)
      }
      if (error.value === 'downloadFailed') error.value = ''
    } catch {
      error.value = 'downloadFailed'
    }
  }

  const finish = (recording: RecordingSession) => {
    if (session !== recording) return
    let file: RecordingFile | undefined
    try {
      const type = recording.recorder?.mimeType || recording.chunks[0]?.type || mimeType.value
      const blob = new Blob(recording.chunks, { type })
      if (blob.size) {
        file = { blob, name: recordingFilename(recording.startedAt, type) }
      } else if (!error.value) {
        error.value = 'empty'
      }
    } catch {
      error.value = 'finalizeFailed'
    } finally {
      session = null
      release(recording)
      status.value = 'idle'
    }
    if (!file) return
    if (!disposed) latestFile.value = file
    downloadFile(file)
  }

  const stop = () => {
    const recording = session
    if (!recording || status.value === 'stopping') return
    clearTimer(recording)
    if (!recording.recorder) {
      session = null
      release(recording)
      status.value = 'idle'
      return
    }
    status.value = 'stopping'
    // An inactive recorder may still owe us its final dataavailable and stop events.
    if (recording.recorder.state !== 'inactive') recording.recorder.stop()
  }

  const connectSources = (recording: RecordingSession) => {
    const mix = recording.mix
    if (!mix) return
    const streams = [options.localStream.value, options.remoteStream.value]
    for (const [stream, source] of recording.sources) {
      if (streams.includes(stream)) continue
      source.disconnect()
      recording.sources.delete(stream)
    }
    for (const stream of streams) {
      if (
        !stream?.getAudioTracks().some((track) => track.readyState === 'live') ||
        recording.sources.has(stream)
      )
        continue
      const source = recording.context.createMediaStreamSource(stream)
      source.connect(mix)
      recording.sources.set(stream, source)
    }
  }

  const start = async () => {
    if (disposed || !canStart.value) return false
    status.value = 'starting'
    error.value = ''
    elapsedSeconds.value = 0
    let recording: RecordingSession | null = null
    try {
      recording = {
        callId: options.callId.value,
        context: new AudioContext(),
        output: null,
        mix: null,
        sources: new Map(),
        recorder: null,
        chunks: [],
        startedAt: new Date(),
        timer: null,
      }
      session = recording
      const current = recording
      current.output = current.context.createMediaStreamDestination()
      current.output.channelCount = 1
      current.mix = current.context.createGain()
      // Leave headroom when both parties speak at once; never feed the mix to the speakers.
      current.mix.gain.value = 0.5
      current.mix.connect(current.output)
      connectSources(current)
      await current.context.resume()
      if (session !== current || disposed) return false
      current.context.onstatechange = () => {
        if (status.value !== 'recording' || current.context.state === 'running') return
        error.value = 'recordingFailed'
        stop()
      }

      const recorder = new MediaRecorder(current.output.stream, {
        mimeType: mimeType.value,
        audioBitsPerSecond: 64_000,
      })
      current.recorder = recorder
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) current.chunks.push(event.data)
      }
      recorder.onstop = () => finish(current)
      recorder.onerror = () => {
        error.value = 'recordingFailed'
        stop()
      }
      recorder.start(1000)
      current.startedAt = new Date()
      const startedAt = performance.now()
      current.timer = setInterval(() => {
        elapsedSeconds.value = Math.floor((performance.now() - startedAt) / 1000)
      }, 1000)
      status.value = 'recording'
      return true
    } catch {
      // A cancelled resume may reject after a new recording has already started.
      if (recording && session !== recording) return false
      if (recording) release(recording)
      session = null
      status.value = 'idle'
      error.value = 'startFailed'
      return false
    }
  }

  watch(
    [options.callId, options.audioReady, remoteAudioAvailable],
    ([callId, ready, available]) => {
      if (session && (callId !== session.callId || !ready || !available)) stop()
    },
    { flush: 'sync' },
  )
  watch(
    options.remoteStream,
    (stream, _previous, onCleanup) => {
      const tracks = stream?.getAudioTracks() ?? []
      const update = () => {
        remoteAudioAvailable.value = tracks.some((track) => track.readyState === 'live')
      }
      for (const track of tracks) track.addEventListener('ended', update)
      update()
      onCleanup(() => {
        for (const track of tracks) track.removeEventListener('ended', update)
      })
    },
    { immediate: true, flush: 'sync' },
  )
  watch(
    [options.localStream, options.remoteStream],
    () => {
      if (!session || status.value === 'stopping') return
      try {
        // Keep the recorded output track stable when WebRTC replaces an input device/stream.
        connectSources(session)
      } catch {
        error.value = 'recordingFailed'
        stop()
      }
    },
    { flush: 'sync' },
  )

  const download = () => {
    if (latestFile.value) downloadFile(latestFile.value)
  }
  const dismiss = () => {
    latestFile.value = null
    error.value = ''
  }

  onScopeDispose(() => {
    disposed = true
    stop()
    latestFile.value = null
  })

  return {
    status: readonly(status),
    error: readonly(error),
    isSupported,
    canStart,
    isRecording,
    isBusy,
    durationLabel,
    filename: computed(() => latestFile.value?.name ?? ''),
    start,
    stop,
    download,
    dismiss,
  }
}

function recordingFilename(date: Date, mimeType: string): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  const day = `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}`
  const time = `${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  const extension = mimeType.startsWith('audio/mp4')
    ? 'm4a'
    : mimeType.startsWith('audio/ogg')
      ? 'ogg'
      : 'webm'
  return `sigmo-call-${day}-${time}-${String(date.getMilliseconds()).padStart(3, '0')}.${extension}`
}

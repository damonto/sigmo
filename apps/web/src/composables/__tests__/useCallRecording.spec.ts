import { effectScope, ref, shallowRef, type EffectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useCallRecording } from '@/composables/useCallRecording'

class FakeTrack extends EventTarget {
  readyState: MediaStreamTrackState = 'live'
  enabled = true
  stop = vi.fn(() => {
    this.readyState = 'ended'
  })
}

class FakeStream {
  tracks = [new FakeTrack()]
  getTracks() {
    return this.tracks
  }
  getAudioTracks() {
    return this.tracks
  }
}

class FakeNode {
  connect = vi.fn()
  disconnect = vi.fn()
}

class FakeAudioContext {
  static instances: FakeAudioContext[] = []
  static resume = () => Promise.resolve()
  state: AudioContextState = 'suspended'
  onstatechange: (() => void) | null = null
  output = { stream: new MediaStream(), channelCount: 2 }
  mix = Object.assign(new FakeNode(), { gain: { value: 1 } })
  sources: (FakeNode & { stream: MediaStream })[] = []
  constructor() {
    FakeAudioContext.instances.push(this)
  }
  createMediaStreamDestination = () => this.output
  createGain = () => this.mix
  createMediaStreamSource = (stream: MediaStream) => {
    const source = Object.assign(new FakeNode(), { stream })
    this.sources.push(source)
    return source
  }
  resume = vi.fn(async () => {
    await FakeAudioContext.resume()
    this.state = 'running'
  })
  close = vi.fn(async () => {
    this.state = 'closed'
  })
  changeState(state: AudioContextState) {
    this.state = state
    this.onstatechange?.()
  }
}

class FakeRecorder {
  static instances: FakeRecorder[] = []
  static supported = ['audio/webm;codecs=opus']
  static isTypeSupported = (type: string) => FakeRecorder.supported.includes(type)
  static startError = false
  state: RecordingState = 'inactive'
  mimeType: string
  finalChunk = 'last'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(
    public stream: MediaStream,
    public options: MediaRecorderOptions,
  ) {
    this.mimeType = options.mimeType ?? ''
    FakeRecorder.instances.push(this)
  }
  start = vi.fn(() => {
    if (FakeRecorder.startError) throw new Error('encoder unavailable')
    this.state = 'recording'
  })
  stop = vi.fn(() => {
    this.state = 'inactive'
    // Like the browser, deliver the final chunk asynchronously before the stop event.
    queueMicrotask(() => this.finish())
  })
  emit(text: string) {
    this.ondataavailable?.({ data: new Blob([text], { type: this.mimeType }) })
  }
  finish() {
    this.emit(this.finalChunk)
    this.onstop?.()
  }
  fail() {
    this.state = 'inactive'
    this.onerror?.()
    queueMicrotask(() => this.finish())
  }
}

const scopes: EffectScope[] = []
const createObjectURL = vi.fn<(blob: Blob) => string>()
const revokeObjectURL = vi.fn()
const downloads: string[] = []

function setup() {
  const options = {
    callId: ref('call-1'),
    audioReady: ref(true),
    localStream: shallowRef<MediaStream | null>(new MediaStream()),
    remoteStream: shallowRef<MediaStream | null>(new MediaStream()),
  }
  const scope = effectScope()
  scopes.push(scope)
  const recording = scope.run(() => useCallRecording(options))
  if (!recording) throw new Error('recording scope did not start')
  return { options, recording, scope }
}

function activeRecorder() {
  const recorder = FakeRecorder.instances[FakeRecorder.instances.length - 1]
  if (!recorder) throw new Error('recorder did not start')
  return recorder
}

function activeContext() {
  const context = FakeAudioContext.instances[FakeAudioContext.instances.length - 1]
  if (!context) throw new Error('audio context did not start')
  return context
}

describe('useCallRecording', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 9, 2, 15, 30, 0))
    FakeAudioContext.instances = []
    FakeAudioContext.resume = () => Promise.resolve()
    FakeRecorder.instances = []
    FakeRecorder.supported = ['audio/webm;codecs=opus']
    FakeRecorder.startError = false
    downloads.length = 0
    createObjectURL.mockReset().mockImplementation(() => `blob:recording-${downloads.length}`)
    revokeObjectURL.mockReset()
    vi.stubGlobal('MediaStream', FakeStream)
    vi.stubGlobal('AudioContext', FakeAudioContext)
    vi.stubGlobal('MediaRecorder', FakeRecorder)
    vi.stubGlobal('URL', Object.assign(class extends URL {}, { createObjectURL, revokeObjectURL }))
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      downloads.push(this.download)
    })
  })

  afterEach(async () => {
    for (const scope of scopes.splice(0)) scope.stop()
    await Promise.resolve()
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('mixes both parties and downloads all chunks without stopping the call tracks', async () => {
    const { recording, options } = setup()
    await expect(recording.start()).resolves.toBe(true)
    const context = activeContext()
    const recorder = activeRecorder()
    expect(context.sources.map((source) => source.stream)).toEqual([
      options.localStream.value,
      options.remoteStream.value,
    ])
    for (const source of context.sources) expect(source.connect).toHaveBeenCalledWith(context.mix)
    expect(context.mix.connect).toHaveBeenCalledExactlyOnceWith(context.output)
    expect(context.mix.gain.value).toBe(0.5)
    expect(recorder.stream).toBe(context.output.stream)
    expect(recorder.options.audioBitsPerSecond).toBe(64_000)
    recorder.emit('first')
    vi.advanceTimersByTime(65_000)
    expect(recording.durationLabel.value).toBe('1:05')

    recording.stop()
    expect(downloads).toHaveLength(0)
    expect(recording.isBusy.value).toBe(true)
    await Promise.resolve()

    expect(downloads).toEqual(['sigmo-call-20261002-153000-000.webm'])
    expect(createObjectURL.mock.calls[0]?.[0].size).toBe(9)
    expect(recording.status.value).toBe('idle')
    expect(context.close).toHaveBeenCalledOnce()
    expect(context.output.stream.getAudioTracks()[0]?.stop).toHaveBeenCalledOnce()
    expect(options.localStream.value?.getAudioTracks()[0]?.stop).not.toHaveBeenCalled()
    expect(options.remoteStream.value?.getAudioTracks()[0]?.stop).not.toHaveBeenCalled()
    expect(revokeObjectURL).not.toHaveBeenCalled()

    recording.download()
    expect(downloads).toHaveLength(2)
    expect(createObjectURL.mock.calls[1]?.[0]).toBe(createObjectURL.mock.calls[0]?.[0])
    recording.dismiss()
    recording.download()
    expect(downloads).toHaveLength(2)
    vi.advanceTimersByTime(60_000)
    expect(revokeObjectURL).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['audio/webm;codecs=opus', 'webm'],
    ['audio/mp4', 'm4a'],
    ['audio/ogg;codecs=opus', 'ogg'],
  ])('downloads %s with a matching .%s extension', async (type, extension) => {
    FakeRecorder.supported = [type]
    const { recording } = setup()
    await recording.start()
    recording.stop()
    await Promise.resolve()
    expect(recording.filename.value).toMatch(new RegExp(`\\.${extension}$`))
    expect(createObjectURL.mock.calls[0]?.[0].type).toBe(type)
  })

  it.each(['unsupported', 'no call', 'not ready', 'no remote audio'])(
    'does not start when %s',
    async (reason) => {
      if (reason === 'unsupported') FakeRecorder.supported = []
      const { recording, options } = setup()
      if (reason === 'no call') options.callId.value = ''
      if (reason === 'not ready') options.audioReady.value = false
      if (reason === 'no remote audio') options.remoteStream.value = null
      await expect(recording.start()).resolves.toBe(false)
      expect(FakeAudioContext.instances).toHaveLength(0)
    },
  )

  it('supports receive-only audio without requesting a microphone', async () => {
    const { recording, options } = setup()
    options.localStream.value = null
    await expect(recording.start()).resolves.toBe(true)
    expect(activeContext().sources.map((source) => source.stream)).toEqual([
      options.remoteStream.value,
    ])
  })

  it('replaces only the changed input without disconnecting the other party', async () => {
    const { recording, options } = setup()
    await recording.start()
    const context = activeContext()
    const originalSources = [...context.sources]
    const nextInput = new MediaStream()
    const track = nextInput.getAudioTracks()[0]
    if (!track) throw new Error('missing microphone')
    track.enabled = false
    options.localStream.value = nextInput
    expect(originalSources[0]?.disconnect).toHaveBeenCalledOnce()
    expect(originalSources[1]?.disconnect).not.toHaveBeenCalled()
    expect(context.sources[context.sources.length - 1]?.stream).toBe(nextInput)
    options.remoteStream.value = new MediaStream()
    expect(originalSources[1]?.disconnect).toHaveBeenCalledOnce()
    expect(context.sources[2]?.disconnect).not.toHaveBeenCalled()
    expect(track.enabled).toBe(false)
    expect(activeRecorder().stream).toBe(context.output.stream)
    expect(activeRecorder().stop).not.toHaveBeenCalled()
    expect(FakeRecorder.instances).toHaveLength(1)
  })

  it('keeps elapsed time monotonic when the system clock changes', async () => {
    const { recording } = setup()
    await recording.start()
    vi.advanceTimersByTime(5000)
    vi.setSystemTime(new Date(2020, 0, 1))
    vi.advanceTimersByTime(1000)
    expect(recording.durationLabel.value).toBe('0:06')
  })

  it.each(['suspended', 'interrupted', 'closed'] as const)(
    'finalizes the recording when the audio context becomes %s',
    async (state) => {
      const { recording } = setup()
      await recording.start()
      activeRecorder().emit('before interruption')
      activeContext().changeState(state)
      await Promise.resolve()
      expect(recording.error.value).toBe('recordingFailed')
      expect(recording.status.value).toBe('idle')
      expect(downloads).toHaveLength(1)
    },
  )

  it.each(['hangup', 'another call', 'connection failed', 'remote removed', 'remote ended'])(
    'finalizes exactly once on %s',
    async (reason) => {
      const { recording, options } = setup()
      await recording.start()
      if (reason === 'hangup') options.callId.value = ''
      if (reason === 'another call') options.callId.value = 'call-2'
      if (reason === 'connection failed') options.audioReady.value = false
      if (reason === 'remote removed') options.remoteStream.value = null
      if (reason === 'remote ended') {
        const track = options.remoteStream.value?.getAudioTracks()[0]
        track?.stop()
        track?.dispatchEvent(new Event('ended'))
      }
      recording.stop()
      await Promise.resolve()
      expect(activeRecorder().stop).toHaveBeenCalledOnce()
      expect(downloads).toHaveLength(1)
      expect(recording.canStart.value).toBe(reason === 'another call')
    },
  )

  it('keeps recordings separate and prevents starts during finalization', async () => {
    const { recording } = setup()
    await recording.start()
    await expect(recording.start()).resolves.toBe(false)
    activeRecorder().emit('first recording')
    recording.stop()
    const prematureStart = recording.start()
    await expect(prematureStart).resolves.toBe(false)
    await recording.start()
    recording.stop()
    await Promise.resolve()
    expect(downloads).toHaveLength(2)
    expect(createObjectURL.mock.calls[1]?.[0].size).toBe(4)
  })

  it('cancels a pending start when the call ends', async () => {
    let resume: (() => void) | undefined
    FakeAudioContext.resume = () =>
      new Promise<void>((resolve) => {
        resume = resolve
      })
    const { recording, options } = setup()
    const started = recording.start()
    options.callId.value = ''
    expect(activeContext().close).toHaveBeenCalledOnce()
    resume?.()
    await expect(started).resolves.toBe(false)
    expect(FakeRecorder.instances).toHaveLength(0)
    expect(recording.status.value).toBe('idle')
  })

  it('cleans up an encoder start failure and allows retrying', async () => {
    FakeRecorder.startError = true
    const { recording } = setup()
    await expect(recording.start()).resolves.toBe(false)
    expect(recording.error.value).toBe('startFailed')
    expect(activeContext().close).toHaveBeenCalledOnce()
    FakeRecorder.startError = false
    await expect(recording.start()).resolves.toBe(true)
  })

  it('waits for the last chunk after an encoder error makes the recorder inactive', async () => {
    const { recording } = setup()
    await recording.start()
    activeRecorder().emit('partial')
    activeRecorder().fail()
    expect(downloads).toHaveLength(0)
    await Promise.resolve()
    expect(recording.error.value).toBe('recordingFailed')
    expect(createObjectURL.mock.calls[0]?.[0].size).toBe(11)
    expect(activeRecorder().stop).not.toHaveBeenCalled()
  })

  it('reports an empty recording without downloading an empty file', async () => {
    const { recording } = setup()
    await recording.start()
    activeRecorder().finalChunk = ''
    recording.stop()
    await Promise.resolve()
    expect(recording.error.value).toBe('empty')
    expect(downloads).toHaveLength(0)
  })

  it('releases the mixer and timer even when assembling the recording fails', async () => {
    const { recording } = setup()
    await recording.start()
    const NativeBlob = Blob
    vi.stubGlobal(
      'Blob',
      class extends NativeBlob {
        constructor(parts: BlobPart[] = [], options?: BlobPropertyBag) {
          if (parts.some((part) => part instanceof NativeBlob)) {
            throw new Error('blob allocation failed')
          }
          super(parts, options)
        }
      },
    )
    recording.stop()
    await Promise.resolve()
    expect(recording.status.value).toBe('idle')
    expect(recording.error.value).toBe('finalizeFailed')
    expect(activeContext().close).toHaveBeenCalledOnce()
    expect(activeContext().onstatechange).toBeNull()
    expect(activeContext().output.stream.getTracks()[0]?.stop).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
    expect(downloads).toHaveLength(0)
  })

  it('retains a manual download after an automatic download fails', async () => {
    const { recording } = setup()
    await recording.start()
    createObjectURL.mockImplementationOnce(() => {
      throw new Error('download blocked')
    })
    recording.stop()
    await Promise.resolve()
    expect(recording.error.value).toBe('downloadFailed')
    expect(recording.filename.value).not.toBe('')
    recording.download()
    expect(downloads).toHaveLength(1)
    expect(recording.error.value).toBe('')
  })

  it('finalizes on disposal without retaining the file or stopping borrowed audio', async () => {
    const { recording, scope, options } = setup()
    await recording.start()
    scope.stop()
    await Promise.resolve()
    expect(downloads).toHaveLength(1)
    expect(recording.filename.value).toBe('')
    expect(activeContext().close).toHaveBeenCalledOnce()
    expect(options.localStream.value?.getAudioTracks()[0]?.stop).not.toHaveBeenCalled()
    await expect(recording.start()).resolves.toBe(false)
  })
})

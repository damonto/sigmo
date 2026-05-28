import { computed, getCurrentInstance, onBeforeUnmount, ref, watch, type Ref } from 'vue'

import { useCallMediaSession } from '@/composables/useCallMediaSession'
import { CallMediaPipeline, type AmrCodecAdapter, type PcmFrame } from '@/lib/callMediaPipeline'
import type { CallMediaInfo } from '@/types/call'

type AudioStatus = 'idle' | 'preparing' | 'connecting' | 'ready' | 'unsupported' | 'closed' | 'error'

type CodecFactory = (media: CallMediaInfo) => AmrCodecAdapter | Promise<AmrCodecAdapter>

type AudioDeps = {
  createAudioContext?: () => AudioContext
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  micWorkletUrl?: URL
}

type Options = {
  codecFactory?: CodecFactory
  deps?: AudioDeps
}

export const useCallAudioSession = (modemId: Ref<string>, options: Options = {}) => {
  const status = ref<AudioStatus>('idle')
  const errorMessage = ref('')
  const activeCallID = ref('')

  let audioContext: AudioContext | null = null
  let stream: MediaStream | null = null
  let sourceNode: MediaStreamAudioSourceNode | null = null
  let processorNode: AudioNode | null = null
  let muteNode: GainNode | null = null
  let pipeline: CallMediaPipeline | null = null
  let nextPlaybackTime = 0
  let inputPromise: Promise<void> | null = null

  const media = useCallMediaSession(modemId, {
    onRtpPacket: (packet) => {
      if (!pipeline) return
      void pipeline.receiveRtpPacket(packet).catch((err: unknown) => {
        fail(err, 'Receive call audio failed')
      })
    },
  })

  const isReady = computed(() => status.value === 'ready')
  const mediaStatus = computed(() => media.status.value)

  const fail = (err: unknown, fallback: string) => {
    errorMessage.value = err instanceof Error ? err.message : fallback
    status.value = 'error'
    cleanup()
  }

  const captureUnavailable = (err: unknown) => {
    errorMessage.value = err instanceof Error ? err.message : 'Microphone capture is not available'
    status.value = 'error'
    cleanup()
  }

  const playPcm = (frame: PcmFrame) => {
    if (!audioContext || frame.samples.length === 0) return
    const buffer = audioContext.createBuffer(1, frame.samples.length, frame.sampleRate)
    buffer.copyToChannel(new Float32Array(frame.samples), 0)
    const source = audioContext.createBufferSource()
    source.buffer = buffer
    source.connect(audioContext.destination)
    const startAt = Math.max(audioContext.currentTime, nextPlaybackTime)
    source.start(startAt)
    nextPlaybackTime = startAt + buffer.duration
  }

  const sendMicPcm = (samples: Float32Array<ArrayBufferLike>, sampleRate: number) => {
    const copy = new Float32Array(samples.length)
    copy.set(samples)
    void pipeline?.sendPcm(copy, sampleRate).catch((err: unknown) => {
      fail(err, 'Send call audio failed')
    })
  }

  const ensureAudioOutput = async () => {
    audioContext ??= options.deps?.createAudioContext?.() ?? new AudioContext()
    if (audioContext.state === 'suspended') {
      await audioContext.resume()
    }
    nextPlaybackTime = audioContext.currentTime
  }

  const ensureAudioInput = async () => {
    if (stream) return true
    if (inputPromise) {
      await inputPromise
      return Boolean(stream)
    }
    inputPromise = (async () => {
      audioContext ??= options.deps?.createAudioContext?.() ?? new AudioContext()
      if (audioContext.state === 'suspended') {
        await audioContext.resume()
      }
      if (!stream) {
        const getUserMedia = options.deps?.getUserMedia ?? navigator.mediaDevices?.getUserMedia.bind(navigator.mediaDevices)
        if (!getUserMedia) {
          throw new Error('Microphone capture is not available')
        }
        stream = await getUserMedia({ audio: true })
      }
      nextPlaybackTime = audioContext.currentTime
    })()
    try {
      await inputPromise
      return true
    } catch (err) {
      captureUnavailable(err)
      return false
    } finally {
      inputPromise = null
    }
  }

  const connectMicrophone = async () => {
    if (!audioContext || !stream || !pipeline) return
    try {
      sourceNode = audioContext.createMediaStreamSource(stream)
      muteNode = audioContext.createGain()
      muteNode.gain.value = 0

      processorNode = await createMicProcessor(audioContext, (samples) => {
        sendMicPcm(samples, audioContext?.sampleRate ?? 48000)
      }, options.deps?.micWorkletUrl)

      if ('onaudioprocess' in processorNode) {
        processorNode.onaudioprocess = (event: AudioProcessingEvent) => {
          sendMicPcm(event.inputBuffer.getChannelData(0), frameSampleRate(event))
        }
      }

      sourceNode.connect(processorNode)
      processorNode.connect(muteNode)
      muteNode.connect(audioContext.destination)
    } catch (err) {
      captureUnavailable(err)
      disconnectNode(sourceNode)
      disconnectNode(processorNode)
      disconnectNode(muteNode)
      sourceNode = null
      processorNode = null
      muteNode = null
    }
  }

  const attachPipeline = async (info: CallMediaInfo) => {
    if (!options.codecFactory || !audioContext || !activeCallID.value || pipeline) return
    const callID = activeCallID.value
    try {
      const codec = await options.codecFactory(info)
      if (activeCallID.value !== callID) {
        await codec.close?.()
        return
      }
      pipeline = new CallMediaPipeline({
        media: info,
        codec,
        onRemotePcm: playPcm,
        sendRtpPacket: media.sendRtpPacket,
      })
      await connectMicrophone()
      status.value = 'ready'
    } catch (err) {
      fail(err, 'Start call audio failed')
    }
  }

  const start = async (callID: string) => {
    if (!callID) return false
    cleanup(true)
    activeCallID.value = callID
    errorMessage.value = ''

    if (!options.codecFactory) {
      status.value = 'unsupported'
      errorMessage.value = 'AMR codec module is not available'
      return false
    }

      status.value = 'preparing'
    try {
      await ensureAudioOutput()
      if (!await ensureAudioInput()) {
        activeCallID.value = ''
        return false
      }
      status.value = 'connecting'
      media.connect(callID)
      if (media.mediaInfo.value) {
        void attachPipeline(media.mediaInfo.value)
      }
      return true
    } catch (err) {
      fail(err, 'Start call audio failed')
      return false
    }
  }

  const prepare = async () => {
    errorMessage.value = ''
    if (!options.codecFactory) {
      status.value = 'unsupported'
      errorMessage.value = 'AMR codec module is not available'
      return false
    }
    status.value = 'preparing'
    try {
      await ensureAudioOutput()
      if (!await ensureAudioInput()) {
        return false
      }
      if (!activeCallID.value && status.value === 'preparing') {
        status.value = 'idle'
      }
      return true
    } catch (err) {
      fail(err, 'Prepare call audio failed')
      return false
    }
  }

  const cleanup = (keepInput = false) => {
    pipeline?.close()
    pipeline = null
    disconnectNode(sourceNode)
    disconnectNode(processorNode)
    disconnectNode(muteNode)
    sourceNode = null
    processorNode = null
    muteNode = null
    if (!keepInput && stream) {
      for (const track of stream.getTracks()) {
        track.stop()
      }
      stream = null
    }
    if (!keepInput && audioContext) {
      void audioContext.close()
      audioContext = null
    }
    media.disconnect()
  }

  const stop = () => {
    cleanup()
    activeCallID.value = ''
    status.value = status.value === 'error' ? 'error' : 'closed'
  }

  watch(media.mediaInfo, (info) => {
    if (!info || status.value !== 'connecting') return
    void attachPipeline(info)
  })

  watch(media.status, (next) => {
    if (!activeCallID.value || (status.value !== 'connecting' && status.value !== 'ready')) return
    if (next === 'error') {
      errorMessage.value = media.errorMessage.value || 'Call media connection failed'
      status.value = 'error'
      cleanup()
      return
    }
    if (next === 'closed') {
      status.value = 'closed'
      cleanup()
    }
  })

  if (getCurrentInstance()) {
    onBeforeUnmount(stop)
  }

  return {
    status,
    mediaStatus,
    isReady,
    errorMessage,
    prepare,
    start,
    stop,
  }
}

const frameSampleRate = (event: AudioProcessingEvent) => event.inputBuffer.sampleRate

const createMicProcessor = async (
  audioContext: AudioContext,
  onPcm: (samples: Float32Array<ArrayBufferLike>) => void,
  workletUrl = new URL('../worklets/callMicProcessor.js', import.meta.url),
) => {
  if (audioContext.audioWorklet && typeof AudioWorkletNode !== 'undefined') {
    try {
      await audioContext.audioWorklet.addModule(workletUrl)
      const node = new AudioWorkletNode(audioContext, 'sigmo-call-mic', {
        numberOfInputs: 1,
        numberOfOutputs: 1,
        outputChannelCount: [1],
      })
      node.port.onmessage = (event: MessageEvent<{ type: string; samples?: Float32Array<ArrayBufferLike> }>) => {
        if (event.data.type === 'pcm' && event.data.samples) {
          onPcm(event.data.samples)
        }
      }
      return node
    } catch (err) {
      console.warn('[useCallAudioSession] AudioWorklet unavailable, falling back to ScriptProcessor', err)
    }
  }

  return audioContext.createScriptProcessor(2048, 1, 1)
}

const disconnectNode = (node: AudioNode | null) => {
  if (!node) return
  try {
    node.disconnect()
  } catch {
    // Some browsers throw when a node was already disconnected.
  }
}

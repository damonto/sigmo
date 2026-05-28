import { decoder as createAmrDecoder } from '@audio/decode-amr'

import type { CallMediaInfo } from '@/types/call'

import type { AmrFrame } from './amrRtp'
import type { AmrCodecAdapter, PcmFrame } from './callMediaPipeline'

type AudioData = {
  channelData: Float32Array[]
  sampleRate: number
}

const amrHeader = new Uint8Array([0x23, 0x21, 0x41, 0x4d, 0x52, 0x0a])
const amrWbHeader = new Uint8Array([0x23, 0x21, 0x41, 0x4d, 0x52, 0x2d, 0x57, 0x42, 0x0a])

const encoderWorkerURL = '/codecs/amrnb-encoder.worker.js'

const amrFrameBytes = [12, 13, 15, 17, 19, 20, 26, 31, 5]

const normalizeCodec = (media: CallMediaInfo) => media.codec.trim().toUpperCase()

export const builtInAmrCodecSupports = (media: CallMediaInfo) => normalizeCodec(media) === 'AMR'

export const createBuiltInAmrCodec = async (media: CallMediaInfo): Promise<AmrCodecAdapter> => {
  const codec = normalizeCodec(media)
  if (codec !== 'AMR') {
    throw new Error(`${media.codec} encoding is not available in the built-in codec`)
  }

  const decoder = await createAmrDecoder()
  const encoder = new AmrNbEncoder()
  let decoderPrimed = false

  return {
    decode(frame) {
      const packet = storageFrame(frame)
      const decoded = decoder.decode(decoderPrimed ? packet : joinBytes(amrHeader, packet))
      decoderPrimed = true
      return audioDataToPcm(decoded, media.clockRate)
    },
    async encode(samples, sampleRate) {
      const encoded = await encoder.encode(samples, sampleRate)
      return parseAmrStorageFrames(encoded)
    },
    close() {
      decoder.free()
      encoder.close()
    },
  }
}

export const parseAmrStorageFrames = (data: Uint8Array<ArrayBufferLike>): AmrFrame[] => {
  let offset = headerLength(data)
  if (offset === 0) {
    throw new Error('AMR data is missing a file header')
  }

  const frames: AmrFrame[] = []
  while (offset < data.byteLength) {
    const frameHeader = data[offset]
    if (frameHeader === undefined) {
      throw new Error('AMR frame header is truncated')
    }
    const frameType = (frameHeader >> 3) & 0x0f
    const quality = (frameHeader & 0x04) !== 0
    const speechBytes = amrFrameBytes[frameType]
    if (speechBytes === undefined) {
      throw new Error('AMR frame type is not supported')
    }
    const speechOffset = offset + 1
    const nextOffset = speechOffset + speechBytes
    if (nextOffset > data.byteLength) {
      throw new Error('AMR speech data is truncated')
    }
    frames.push({
      frameType,
      quality,
      data: new Uint8Array(data.slice(speechOffset, nextOffset)),
    })
    offset = nextOffset
  }
  return frames
}

const storageFrame = (frame: AmrFrame) => {
  const header = (frame.frameType << 3) | (frame.quality ? 0x04 : 0)
  const out = new Uint8Array(1 + frame.data.byteLength)
  out[0] = header
  out.set(frame.data, 1)
  return out
}

const audioDataToPcm = (data: AudioData, fallbackSampleRate: number): PcmFrame => ({
  samples: data.channelData[0] ?? new Float32Array(),
  sampleRate: data.sampleRate || fallbackSampleRate,
})

const headerLength = (data: Uint8Array<ArrayBufferLike>) => {
  if (startsWith(data, amrHeader)) return amrHeader.byteLength
  if (startsWith(data, amrWbHeader)) return amrWbHeader.byteLength
  return 0
}

const startsWith = (data: Uint8Array<ArrayBufferLike>, prefix: Uint8Array<ArrayBuffer>) => {
  if (data.byteLength < prefix.byteLength) return false
  for (const [index, value] of prefix.entries()) {
    if (data[index] !== value) return false
  }
  return true
}

const joinBytes = (left: Uint8Array<ArrayBuffer>, right: Uint8Array<ArrayBuffer>) => {
  const out = new Uint8Array(left.byteLength + right.byteLength)
  out.set(left)
  out.set(right, left.byteLength)
  return out
}

const padForBenzEncoder = (samples: Float32Array<ArrayBufferLike>) => {
  const padded = new Float32Array(samples.length + 1)
  padded.set(samples)
  return padded
}

type EncodeRequest = {
  samples: Float32Array
  sampleRate: number
  resolve: (data: Uint8Array) => void
  reject: (err: Error) => void
}

class AmrNbEncoder {
  private worker: Worker | null = null
  private workerURL = ''
  private queue: EncodeRequest[] = []

  encode(samples: Float32Array<ArrayBufferLike>, sampleRate: number) {
    return new Promise<Uint8Array>((resolve, reject) => {
      const padded = padForBenzEncoder(samples)
      this.queue.push({ samples: padded, sampleRate, resolve, reject })
      if (this.queue.length === 1) {
        try {
          this.sendCurrent()
        } catch (err) {
          this.queue.shift()
          reject(err instanceof Error ? err : new Error('AMR encoder worker failed'))
        }
      }
    })
  }

  close() {
    this.worker?.terminate()
    this.worker = null
    if (this.workerURL) {
      URL.revokeObjectURL(this.workerURL)
      this.workerURL = ''
    }
    for (const request of this.queue.splice(0)) {
      request.reject(new Error('AMR encoder is closed'))
    }
  }

  private sendCurrent() {
    const request = this.queue[0]
    if (!request) return
    const worker = this.ensureWorker()
    worker.postMessage(
      {
        command: 'encode',
        samples: request.samples,
        sampleRate: request.sampleRate,
      },
      [request.samples.buffer],
    )
  }

  private ensureWorker() {
    if (typeof Worker === 'undefined') {
      throw new Error('AMR encoder worker is not available')
    }
    if (this.worker) return this.worker

    this.worker = new Worker(encoderWorkerURL)
    this.worker.onmessage = (event: MessageEvent<{ amr?: Uint8Array }>) => {
      const request = this.queue.shift()
      if (!request) return
      if (!event.data.amr) {
        request.reject(new Error('AMR encoder returned no data'))
      } else {
        request.resolve(event.data.amr)
      }
      this.sendCurrent()
    }
    this.worker.onerror = (event) => {
      const request = this.queue.shift()
      request?.reject(new Error(event.message || 'AMR encoder worker failed'))
      this.sendCurrent()
    }
    return this.worker
  }
}

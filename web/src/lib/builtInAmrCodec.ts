import type { CallMediaInfo } from '@/types/call'

import type { AmrCodec, AmrFrame } from './amrRtp'
import { resampleMono, type AmrCodecAdapter, type PcmFrame } from './callMediaPipeline'

type OpenCoreAmrModule = {
  HEAPU8: Uint8Array
  HEAP16: Int16Array
  _malloc(size: number): number
  _free(ptr: number): void
  _sigmo_amrnb_decoder_create(): number
  _sigmo_amrnb_decoder_destroy(state: number): void
  _sigmo_amrnb_decode(state: number, frame: number, pcm: number, bfi: number): void
  _sigmo_amrnb_encoder_create(dtx: number): number
  _sigmo_amrnb_encoder_destroy(state: number): void
  _sigmo_amrnb_encode(state: number, mode: number, pcm: number, out: number): number
  _sigmo_amrwb_decoder_create(): number
  _sigmo_amrwb_decoder_destroy(state: number): void
  _sigmo_amrwb_decode(state: number, frame: number, pcm: number, bfi: number): void
  _sigmo_amrwb_encoder_create(): number
  _sigmo_amrwb_encoder_destroy(state: number): void
  _sigmo_amrwb_encode(state: number, mode: number, pcm: number, out: number, dtx: number): number
}

type OpenCoreAmrFactory = (options?: {
  locateFile?: (path: string, prefix: string) => string
  wasmBinary?: ArrayBuffer | Uint8Array<ArrayBufferLike>
}) => Promise<OpenCoreAmrModule>

type OpenCoreAmrGlobal = typeof globalThis & {
  __sigmoOpenCoreAmrFactory?: OpenCoreAmrFactory
}

const wasmModuleURL = '/codecs/opencore-amr.js'
const wasmBinaryURL = '/codecs/opencore-amr.wasm'

const amrHeader = new Uint8Array([0x23, 0x21, 0x41, 0x4d, 0x52, 0x0a])
const amrWbHeader = new Uint8Array([0x23, 0x21, 0x41, 0x4d, 0x52, 0x2d, 0x57, 0x42, 0x0a])

const frameBytesByCodec = {
  AMR: [12, 13, 15, 17, 19, 20, 26, 31, 5],
  'AMR-WB': [17, 23, 32, 36, 40, 46, 50, 58, 60, 5],
} satisfies Record<AmrCodec, number[]>

const samplesPerFrame = {
  AMR: 160,
  'AMR-WB': 320,
} satisfies Record<AmrCodec, number>

const codecSampleRate = {
  AMR: 8000,
  'AMR-WB': 16000,
} satisfies Record<AmrCodec, number>

const encodeMode = {
  AMR: 7,
  'AMR-WB': 8,
} satisfies Record<AmrCodec, number>

const maxStorageFrameBytes = 1 + 64

const normalizeCodec = (media: CallMediaInfo): AmrCodec => {
  const codec = media.codec.trim().toUpperCase()
  if (codec === 'AMR' || codec === 'AMR-WB') {
    return codec
  }
  throw new Error(`${media.codec} codec is not available`)
}

export const builtInAmrCodecSupports = (media: CallMediaInfo) => {
  const codec = media.codec.trim().toUpperCase()
  return codec === 'AMR' || codec === 'AMR-WB'
}

let modulePromise: Promise<OpenCoreAmrModule> | null = null

export const createBuiltInAmrCodec = async (media: CallMediaInfo): Promise<AmrCodecAdapter> => {
  const codec = normalizeCodec(media)
  const module = await loadOpenCoreAmrModule()
  return new OpenCoreAmrCodec(module, codec, media.clockRate || codecSampleRate[codec])
}

export const parseAmrStorageFrames = (data: Uint8Array<ArrayBufferLike>): AmrFrame[] => {
  const parsed = parseStorageFrames(data)
  return parsed.frames
}

const loadOpenCoreAmrModule = async () => {
  modulePromise ??= loadOpenCoreAmrFactory()
    .then((factory) =>
      factory({
        locateFile(path) {
          return path.endsWith('.wasm') ? wasmBinaryURL : `/codecs/${path}`
        },
      }),
    )
    .catch((err: unknown) => {
      modulePromise = null
      throw new Error(
        `AMR WASM module is not available; run scripts/build-opencore-amr-wasm.sh: ${errorMessage(err)}`,
      )
    })
  return modulePromise
}

const loadOpenCoreAmrFactory = async () => {
  const factory = (globalThis as OpenCoreAmrGlobal).__sigmoOpenCoreAmrFactory
  if (factory) {
    return factory
  }
  const loaded = (await import(/* @vite-ignore */ wasmModuleURL)) as {
    default: OpenCoreAmrFactory
  }
  return loaded.default
}

class OpenCoreAmrCodec implements AmrCodecAdapter {
  private readonly decoderState: number
  private readonly encoderState: number
  private readonly framePtr: number
  private readonly pcmPtr: number
  private readonly outPtr: number
  private closed = false

  constructor(
    private readonly module: OpenCoreAmrModule,
    private readonly codec: AmrCodec,
    private readonly sampleRate: number,
  ) {
    this.decoderState =
      codec === 'AMR' ? module._sigmo_amrnb_decoder_create() : module._sigmo_amrwb_decoder_create()
    this.encoderState =
      codec === 'AMR' ? module._sigmo_amrnb_encoder_create(0) : module._sigmo_amrwb_encoder_create()
    this.framePtr = module._malloc(maxStorageFrameBytes)
    this.pcmPtr = module._malloc(samplesPerFrame[codec] * Int16Array.BYTES_PER_ELEMENT)
    this.outPtr = module._malloc(maxStorageFrameBytes)
    if (
      !this.decoderState ||
      !this.encoderState ||
      !this.framePtr ||
      !this.pcmPtr ||
      !this.outPtr
    ) {
      this.close()
      throw new Error('AMR WASM codec allocation failed')
    }
  }

  decode(frame: AmrFrame): PcmFrame {
    this.ensureOpen()
    const storage = storageFrame(frame)
    if (storage.byteLength > maxStorageFrameBytes) {
      throw new Error('AMR frame is too large')
    }
    this.module.HEAPU8.set(storage, this.framePtr)
    if (this.codec === 'AMR') {
      this.module._sigmo_amrnb_decode(
        this.decoderState,
        this.framePtr,
        this.pcmPtr,
        frame.quality ? 0 : 1,
      )
    } else {
      this.module._sigmo_amrwb_decode(
        this.decoderState,
        this.framePtr,
        this.pcmPtr,
        frame.quality ? 0 : 1,
      )
    }
    return {
      samples: pcm16ToFloat32(this.module, this.pcmPtr, samplesPerFrame[this.codec]),
      sampleRate: this.sampleRate,
    }
  }

  encode(samples: Float32Array<ArrayBufferLike>, sampleRate: number): AmrFrame[] {
    this.ensureOpen()
    const converted = resampleMono(samples, sampleRate, codecSampleRate[this.codec])
    const frameSamples = samplesPerFrame[this.codec]
    if (converted.length === 0 || converted.length % frameSamples !== 0) {
      throw new Error('AMR encoder requires whole 20 ms PCM frames')
    }

    const frames: AmrFrame[] = []
    for (let offset = 0; offset < converted.length; offset += frameSamples) {
      float32ToPcm16(this.module, converted.subarray(offset, offset + frameSamples), this.pcmPtr)
      const length =
        this.codec === 'AMR'
          ? this.module._sigmo_amrnb_encode(
              this.encoderState,
              encodeMode.AMR,
              this.pcmPtr,
              this.outPtr,
            )
          : this.module._sigmo_amrwb_encode(
              this.encoderState,
              encodeMode['AMR-WB'],
              this.pcmPtr,
              this.outPtr,
              0,
            )
      if (length <= 0 || length > maxStorageFrameBytes) {
        throw new Error('AMR encoder returned an invalid frame')
      }
      const data = this.module.HEAPU8.slice(this.outPtr, this.outPtr + length)
      frames.push(...parseStorageFrames(data, this.codec).frames)
    }
    return frames
  }

  close() {
    if (this.closed) return
    this.closed = true
    if (this.decoderState) {
      if (this.codec === 'AMR') {
        this.module._sigmo_amrnb_decoder_destroy(this.decoderState)
      } else {
        this.module._sigmo_amrwb_decoder_destroy(this.decoderState)
      }
    }
    if (this.encoderState) {
      if (this.codec === 'AMR') {
        this.module._sigmo_amrnb_encoder_destroy(this.encoderState)
      } else {
        this.module._sigmo_amrwb_encoder_destroy(this.encoderState)
      }
    }
    if (this.framePtr) this.module._free(this.framePtr)
    if (this.pcmPtr) this.module._free(this.pcmPtr)
    if (this.outPtr) this.module._free(this.outPtr)
  }

  private ensureOpen() {
    if (this.closed) {
      throw new Error('AMR codec is closed')
    }
  }
}

const parseStorageFrames = (data: Uint8Array<ArrayBufferLike>, explicitCodec?: AmrCodec) => {
  const header = headerInfo(data, explicitCodec)
  let offset = header.length
  const codec = header.codec
  const frames: AmrFrame[] = []

  while (offset < data.byteLength) {
    const frameHeader = data[offset]
    if (frameHeader === undefined) {
      throw new Error('AMR frame header is truncated')
    }
    const frameType = (frameHeader >> 3) & 0x0f
    const quality = (frameHeader & 0x04) !== 0
    const speechBytes = frameBytesByCodec[codec][frameType]
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
  return { codec, frames }
}

const headerInfo = (data: Uint8Array<ArrayBufferLike>, explicitCodec?: AmrCodec) => {
  if (startsWith(data, amrHeader)) return { codec: 'AMR' as const, length: amrHeader.byteLength }
  if (startsWith(data, amrWbHeader))
    return { codec: 'AMR-WB' as const, length: amrWbHeader.byteLength }
  if (explicitCodec) return { codec: explicitCodec, length: 0 }
  throw new Error('AMR data is missing a file header')
}

const storageFrame = (frame: AmrFrame) => {
  const header = (frame.frameType << 3) | (frame.quality ? 0x04 : 0)
  const out = new Uint8Array(1 + frame.data.byteLength)
  out[0] = header
  out.set(frame.data, 1)
  return out
}

const startsWith = (data: Uint8Array<ArrayBufferLike>, prefix: Uint8Array<ArrayBuffer>) => {
  if (data.byteLength < prefix.byteLength) return false
  for (const [index, value] of prefix.entries()) {
    if (data[index] !== value) return false
  }
  return true
}

const pcm16ToFloat32 = (module: OpenCoreAmrModule, ptr: number, count: number) => {
  const start = ptr >> 1
  const pcm = module.HEAP16.slice(start, start + count)
  const out = new Float32Array(count)
  for (let i = 0; i < count; i++) {
    out[i] = Math.max(-1, Math.min(1, (pcm[i] ?? 0) / 32768))
  }
  return out
}

const float32ToPcm16 = (
  module: OpenCoreAmrModule,
  samples: Float32Array<ArrayBufferLike>,
  ptr: number,
) => {
  const start = ptr >> 1
  for (let i = 0; i < samples.length; i++) {
    module.HEAP16[start + i] = Math.round(Math.max(-1, Math.min(1, samples[i] ?? 0)) * 32767)
  }
}

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

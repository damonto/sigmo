import type { CallMediaInfo } from '@/types/call'

import {
  buildAmrBandwidthEfficientPayload,
  buildAmrOctetAlignedPayload,
  buildRtpPacket,
  parseAmrBandwidthEfficientPayload,
  parseAmrOctetAlignedPayload,
  parseRtpPacket,
  type AmrCodec,
  type AmrFrame,
} from './amrRtp'

export type PcmFrame = {
  samples: Float32Array<ArrayBufferLike>
  sampleRate: number
}

export type AmrCodecAdapter = {
  decode(frame: AmrFrame): PcmFrame | Promise<PcmFrame>
  encode(
    samples: Float32Array<ArrayBufferLike>,
    sampleRate: number,
  ): AmrFrame[] | Promise<AmrFrame[]>
  close?: () => void | Promise<void>
}

export type CallMediaPipelineOptions = {
  media: CallMediaInfo
  codec?: AmrCodecAdapter
  onRemotePcm: (frame: PcmFrame) => void
  sendRtpPacket: (packet: Uint8Array<ArrayBuffer>) => boolean
  initialSequenceNumber?: number
  initialTimestamp?: number
  ssrc?: number
}

const defaultPTimeMillis = 20

const random16 = () => Math.floor(Math.random() * 0x10000)
const random32 = () => Math.floor(Math.random() * 0x100000000)

const normalizeAmrCodec = (value: string): AmrCodec => {
  const codec = value.trim().toUpperCase()
  if (codec === 'AMR' || codec === 'AMR-WB') {
    return codec
  }
  throw new Error(`call media codec ${value} is not supported`)
}

type PipelineCodec = AmrCodec | 'PCMU'

const normalizePipelineCodec = (value: string): PipelineCodec => {
  const codec = value.trim().toUpperCase()
  if (codec === 'PCMU') {
    return codec
  }
  return normalizeAmrCodec(codec)
}

export const resampleMono = (
  input: Float32Array<ArrayBufferLike>,
  fromRate: number,
  toRate: number,
) => {
  if (fromRate <= 0 || toRate <= 0) {
    throw new Error('sample rates must be positive')
  }
  if (fromRate === toRate) {
    return input.slice()
  }
  if (input.length === 0) {
    return new Float32Array()
  }

  const outputLength = Math.max(1, Math.round((input.length * toRate) / fromRate))
  const output = new Float32Array(outputLength)
  const scale = fromRate / toRate
  for (let i = 0; i < outputLength; i++) {
    const source = i * scale
    const left = Math.floor(source)
    const right = Math.min(left + 1, input.length - 1)
    const mix = source - left
    const leftValue = input[left] ?? 0
    const rightValue = input[right] ?? leftValue
    output[i] = leftValue * (1 - mix) + rightValue * mix
  }
  return output
}

export class CallMediaPipeline {
  readonly media: CallMediaInfo
  readonly codecName: PipelineCodec

  private readonly codec?: AmrCodecAdapter
  private readonly onRemotePcm: (frame: PcmFrame) => void
  private readonly sendRtpPacket: (packet: Uint8Array<ArrayBuffer>) => boolean
  private readonly samplesPerPacket: number
  private sequenceNumber: number
  private timestamp: number
  private readonly ssrc: number
  private localBuffer: Float32Array<ArrayBufferLike> = new Float32Array()
  private sentFirstPacket = false

  constructor(options: CallMediaPipelineOptions) {
    this.media = options.media
    this.codecName = normalizePipelineCodec(options.media.codec)
    this.codec = options.codec
    this.onRemotePcm = options.onRemotePcm
    this.sendRtpPacket = options.sendRtpPacket
    this.samplesPerPacket = Math.max(
      1,
      Math.round(
        (options.media.clockRate * (options.media.ptimeMillis || defaultPTimeMillis)) / 1000,
      ),
    )
    this.sequenceNumber = options.initialSequenceNumber ?? random16()
    this.timestamp = options.initialTimestamp ?? random32()
    this.ssrc = options.ssrc ?? random32()
  }

  async receiveRtpPacket(packet: ArrayBuffer | Uint8Array<ArrayBuffer>) {
    const rtp = parseRtpPacket(packet)
    if (rtp.payloadType !== this.media.payloadType) {
      return false
    }

    if (this.codecName === 'PCMU') {
      this.onRemotePcm({ samples: decodePCMU(rtp.payload), sampleRate: this.media.clockRate })
      return true
    }

    if (!this.codec) {
      throw new Error(`${this.codecName} codec is not available`)
    }
    const payload = this.media.octetAlign
      ? parseAmrOctetAlignedPayload(this.codecName, rtp.payload)
      : parseAmrBandwidthEfficientPayload(this.codecName, rtp.payload)
    for (const frame of payload.frames) {
      if (!frame.quality || frame.data.byteLength === 0) {
        continue
      }
      this.onRemotePcm(await this.codec.decode(frame))
    }
    return true
  }

  async sendPcm(samples: Float32Array<ArrayBufferLike>, sampleRate: number) {
    const converted = resampleMono(samples, sampleRate, this.media.clockRate)
    this.localBuffer = appendSamples(this.localBuffer, converted)

    let sent = 0
    while (this.localBuffer.length >= this.samplesPerPacket) {
      const chunk = this.localBuffer.slice(0, this.samplesPerPacket)
      this.localBuffer = this.localBuffer.slice(this.samplesPerPacket)
      let payload: Uint8Array<ArrayBuffer>
      if (this.codecName === 'PCMU') {
        payload = encodePCMU(chunk)
      } else {
        if (!this.codec) {
          throw new Error(`${this.codecName} codec is not available`)
        }
        const frames = await this.codec.encode(chunk, this.media.clockRate)
        if (frames.length === 0) {
          continue
        }
        payload = this.media.octetAlign
          ? buildAmrOctetAlignedPayload(this.codecName, frames)
          : buildAmrBandwidthEfficientPayload(this.codecName, frames)
      }
      const packet = buildRtpPacket({
        payloadType: this.media.payloadType,
        sequenceNumber: this.sequenceNumber,
        timestamp: this.timestamp,
        ssrc: this.ssrc,
        marker: !this.sentFirstPacket,
        payload,
      })

      if (!this.sendRtpPacket(packet)) {
        return sent
      }
      this.sentFirstPacket = true
      this.sequenceNumber = (this.sequenceNumber + 1) & 0xffff
      this.timestamp = (this.timestamp + this.samplesPerPacket) >>> 0
      sent++
    }
    return sent
  }

  close() {
    this.localBuffer = new Float32Array()
    void this.codec?.close?.()
  }
}

export const decodePCMU = (payload: Uint8Array<ArrayBufferLike>) => {
  const out = new Float32Array(payload.byteLength)
  for (let i = 0; i < payload.byteLength; i++) {
    const value = ~payload[i] & 0xff
    const sign = value & 0x80
    const exponent = (value >> 4) & 0x07
    const mantissa = value & 0x0f
    const sample = (((mantissa << 3) + 0x84) << exponent) - 0x84
    out[i] = (sign ? -sample : sample) / 32768
  }
  return out
}

export const encodePCMU = (samples: Float32Array<ArrayBufferLike>) => {
  const out = new Uint8Array(samples.length)
  for (let i = 0; i < samples.length; i++) {
    let sample = Math.round(Math.max(-1, Math.min(1, samples[i] ?? 0)) * 32767)
    let sign = 0
    if (sample < 0) {
      sign = 0x80
      sample = -sample
    }
    sample = Math.min(sample, 32635) + 0x84

    let exponent = 7
    for (let mask = 0x4000; exponent > 0 && (sample & mask) === 0; mask >>= 1) {
      exponent--
    }
    const mantissa = (sample >> (exponent + 3)) & 0x0f
    out[i] = ~(sign | (exponent << 4) | mantissa) & 0xff
  }
  return out
}

const appendSamples = (
  left: Float32Array<ArrayBufferLike>,
  right: Float32Array<ArrayBufferLike>,
) => {
  if (left.length === 0) {
    return right
  }
  if (right.length === 0) {
    return left
  }
  const out = new Float32Array(left.length + right.length)
  out.set(left)
  out.set(right, left.length)
  return out
}

import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  builtInAmrCodecSupports,
  createBuiltInAmrCodec,
  parseAmrStorageFrames,
} from '@/lib/builtInAmrCodec'
import type { CallMediaInfo } from '@/types/call'

import {
  installGeneratedOpenCoreAmrFactory,
  uninstallGeneratedOpenCoreAmrFactory,
} from './opencoreAmrWasm'

const media = (codec: string): CallMediaInfo => ({
  codec,
  payloadType: codec === 'AMR' ? 102 : 104,
  clockRate: codec === 'AMR' ? 8000 : 16000,
  channels: 1,
  octetAlign: true,
  dtmfPayloadType: 101,
  dtmfClockRate: 8000,
  ptimeMillis: 20,
})

describe('built-in AMR codec adapter', () => {
  beforeAll(async () => {
    await installGeneratedOpenCoreAmrFactory()
  })

  afterAll(() => {
    uninstallGeneratedOpenCoreAmrFactory()
  })

  it('supports full-duplex AMR-NB and AMR-WB', () => {
    expect(builtInAmrCodecSupports(media('AMR'))).toBe(true)
    expect(builtInAmrCodecSupports(media('AMR-WB'))).toBe(true)
  })

  it('parses AMR storage frames into RTP speech frames', () => {
    const data = new Uint8Array([
      0x23,
      0x21,
      0x41,
      0x4d,
      0x52,
      0x0a,
      0x3c,
      ...new Array(31).fill(0xaa),
    ])

    const frames = parseAmrStorageFrames(data)

    expect(frames).toHaveLength(1)
    expect(frames[0]).toMatchObject({ frameType: 7, quality: true })
    expect(frames[0]?.data).toHaveLength(31)
  })

  it('rejects AMR data without a file header', () => {
    expect(() => parseAmrStorageFrames(new Uint8Array([0x3c]))).toThrow(
      'AMR data is missing a file header',
    )
  })

  it('parses AMR-WB storage frames into RTP speech frames', () => {
    const data = new Uint8Array([
      0x23,
      0x21,
      0x41,
      0x4d,
      0x52,
      0x2d,
      0x57,
      0x42,
      0x0a,
      0x44,
      ...new Array(60).fill(0xaa),
    ])

    const frames = parseAmrStorageFrames(data)

    expect(frames).toHaveLength(1)
    expect(frames[0]).toMatchObject({ frameType: 8, quality: true })
    expect(frames[0]?.data).toHaveLength(60)
  })

  it('encodes and decodes one AMR-NB speech frame with the generated WASM module', async () => {
    const codec = await createBuiltInAmrCodec(media('AMR'))
    try {
      const frames = await codec.encode(new Float32Array(160), 8000)
      expect(frames).toHaveLength(1)
      expect(frames[0]).toMatchObject({ frameType: 7, quality: true })
      expect(frames[0]?.data).toHaveLength(31)

      const decoded = await codec.decode(frames[0]!)
      expect(decoded.sampleRate).toBe(8000)
      expect(decoded.samples).toHaveLength(160)
    } finally {
      codec.close?.()
    }
  })

  it('encodes multiple AMR-NB speech frames for longer ptime audio', async () => {
    const codec = await createBuiltInAmrCodec(media('AMR'))
    try {
      const frames = await codec.encode(new Float32Array(320), 8000)

      expect(frames).toHaveLength(2)
      expect(frames[0]).toMatchObject({ frameType: 7, quality: true })
      expect(frames[1]).toMatchObject({ frameType: 7, quality: true })
    } finally {
      codec.close?.()
    }
  })

  it('encodes and decodes one AMR-WB speech frame with the generated WASM module', async () => {
    const codec = await createBuiltInAmrCodec(media('AMR-WB'))
    try {
      const frames = await codec.encode(new Float32Array(320), 16000)
      expect(frames).toHaveLength(1)
      expect(frames[0]).toMatchObject({ frameType: 8, quality: true })
      expect(frames[0]?.data).toHaveLength(60)

      const decoded = await codec.decode(frames[0]!)
      expect(decoded.sampleRate).toBe(16000)
      expect(decoded.samples).toHaveLength(320)
    } finally {
      codec.close?.()
    }
  })

  it('encodes multiple AMR-WB speech frames for longer ptime audio', async () => {
    const codec = await createBuiltInAmrCodec(media('AMR-WB'))
    try {
      const frames = await codec.encode(new Float32Array(640), 16000)

      expect(frames).toHaveLength(2)
      expect(frames[0]).toMatchObject({ frameType: 8, quality: true })
      expect(frames[1]).toMatchObject({ frameType: 8, quality: true })
    } finally {
      codec.close?.()
    }
  })
})

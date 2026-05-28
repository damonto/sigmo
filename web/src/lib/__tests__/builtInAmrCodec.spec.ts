import { describe, expect, it } from 'vitest'

import {
  builtInAmrCodecSupports,
  createBuiltInAmrCodec,
  parseAmrStorageFrames,
} from '@/lib/builtInAmrCodec'
import type { CallMediaInfo } from '@/types/call'

const media = (codec: string): CallMediaInfo => ({
  codec,
  payloadType: codec === 'AMR' ? 102 : 104,
  clockRate: codec === 'AMR' ? 8000 : 16000,
  channels: 1,
  dtmfPayloadType: 101,
  dtmfClockRate: 8000,
  ptimeMillis: 20,
})

describe('built-in AMR codec adapter', () => {
  it('supports full-duplex AMR-NB only', () => {
    expect(builtInAmrCodecSupports(media('AMR'))).toBe(true)
    expect(builtInAmrCodecSupports(media('AMR-WB'))).toBe(false)
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
    expect(() => parseAmrStorageFrames(new Uint8Array([0x3c]))).toThrow('AMR data is missing a file header')
  })

  it('reports when the encoder worker is unavailable', async () => {
    const codec = await createBuiltInAmrCodec(media('AMR'))
    try {
      await expect(codec.encode(new Float32Array(160), 8000)).rejects.toThrow(
        'AMR encoder worker is not available',
      )
    } finally {
      await codec.close?.()
    }
  })
})

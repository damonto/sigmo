import { describe, expect, it } from 'vitest'

import {
  buildAmrBandwidthEfficientPayload,
  buildAmrOctetAlignedPayload,
  buildRtpPacket,
  parseAmrBandwidthEfficientPayload,
  parseAmrOctetAlignedPayload,
  parseRtpPacket,
} from '@/lib/amrRtp'

const withUnusedPayloadBitsCleared = (data: Uint8Array, bits: number) => {
  const out = data.slice()
  const usedBits = bits % 8
  if (usedBits === 0 || out.length === 0) {
    return out
  }
  out[out.length - 1] &= (0xff << (8 - usedBits)) & 0xff
  return out
}

describe('AMR RTP helpers', () => {
  it('round-trips an RTP packet', () => {
    const payload = new Uint8Array([0xf0, 0x04, 0xaa, 0xbb, 0xcc, 0xdd, 0xee])

    const packet = buildRtpPacket({
      payloadType: 104,
      sequenceNumber: 42,
      timestamp: 0x01020304,
      ssrc: 0xaabbccdd,
      marker: true,
      payload,
    })
    const parsed = parseRtpPacket(packet)

    expect(parsed.payloadType).toBe(104)
    expect(parsed.sequenceNumber).toBe(42)
    expect(parsed.timestamp).toBe(0x01020304)
    expect(parsed.ssrc).toBe(0xaabbccdd)
    expect(parsed.marker).toBe(true)
    expect(Array.from(parsed.payload)).toEqual(Array.from(payload))
  })

  it('parses RTP packets with extension and padding', () => {
    const packet = new Uint8Array([
      0xa0, 102, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0xf0, 0x04, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 2, 2,
    ])

    const parsed = parseRtpPacket(packet)

    expect(parsed.payloadType).toBe(102)
    expect(Array.from(parsed.payload)).toEqual([0xf0, 0x04, 0xaa, 0xbb, 0xcc, 0xdd, 0xee])
  })

  it('round-trips AMR octet-aligned payloads', () => {
    const speech = new Uint8Array([0xaa, 0xbb, 0xcc, 0xdd, 0xee])
    const payload = buildAmrOctetAlignedPayload('AMR', [
      { frameType: 8, quality: true, data: speech },
    ])
    const parsed = parseAmrOctetAlignedPayload('AMR', payload)

    expect(parsed.cmr).toBe(15)
    expect(parsed.frames).toHaveLength(1)
    expect(parsed.frames[0]).toMatchObject({ frameType: 8, quality: true })
    expect(Array.from(parsed.frames[0]?.data ?? [])).toEqual(Array.from(speech))
  })

  it('round-trips AMR-WB payloads with multiple frames', () => {
    const frame1 = new Uint8Array(17).fill(1)
    const frame2 = new Uint8Array(23).fill(2)

    const payload = buildAmrOctetAlignedPayload('AMR-WB', [
      { frameType: 0, quality: true, data: frame1 },
      { frameType: 1, quality: false, data: frame2 },
    ])
    const parsed = parseAmrOctetAlignedPayload('AMR-WB', payload)

    expect(parsed.frames.map((frame) => frame.frameType)).toEqual([0, 1])
    expect(parsed.frames.map((frame) => frame.quality)).toEqual([true, false])
    expect(Array.from(parsed.frames[0]?.data ?? [])).toEqual(Array.from(frame1))
    expect(Array.from(parsed.frames[1]?.data ?? [])).toEqual(Array.from(frame2))
  })

  it('round-trips AMR-WB bandwidth-efficient payloads', () => {
    const frame1 = new Uint8Array(17).fill(1)
    const frame2 = new Uint8Array(23).fill(2)

    const payload = buildAmrBandwidthEfficientPayload('AMR-WB', [
      { frameType: 0, quality: true, data: frame1 },
      { frameType: 1, quality: false, data: frame2 },
    ])
    const parsed = parseAmrBandwidthEfficientPayload('AMR-WB', payload)

    expect(parsed.cmr).toBe(15)
    expect(parsed.frames.map((frame) => frame.frameType)).toEqual([0, 1])
    expect(parsed.frames.map((frame) => frame.quality)).toEqual([true, false])
    expect(Array.from(parsed.frames[0]?.data ?? [])).toEqual(
      Array.from(withUnusedPayloadBitsCleared(frame1, 132)),
    )
    expect(Array.from(parsed.frames[1]?.data ?? [])).toEqual(
      Array.from(withUnusedPayloadBitsCleared(frame2, 177)),
    )
  })

  it('accepts AMR no-data frames without speech bytes', () => {
    const payload = buildAmrOctetAlignedPayload('AMR', [
      { frameType: 15, quality: false, data: new Uint8Array() },
    ])
    const parsed = parseAmrOctetAlignedPayload('AMR', payload)

    expect(parsed.frames).toEqual([{ frameType: 15, quality: false, data: new Uint8Array() }])
  })

  it('accepts AMR-WB speech-lost frames without speech bytes', () => {
    const payload = buildAmrOctetAlignedPayload('AMR-WB', [
      { frameType: 14, quality: false, data: new Uint8Array() },
    ])
    const parsed = parseAmrOctetAlignedPayload('AMR-WB', payload)

    expect(parsed.frames).toEqual([{ frameType: 14, quality: false, data: new Uint8Array() }])
  })

  it('rejects truncated AMR speech data', () => {
    expect(() => parseAmrOctetAlignedPayload('AMR', new Uint8Array([0xf0, 0x04, 0xaa]))).toThrow(
      'amr speech data is truncated',
    )
  })
})

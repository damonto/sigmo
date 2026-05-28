import { describe, expect, it, vi } from 'vitest'

import {
  CallMediaPipeline,
  decodePCMU,
  encodePCMU,
  resampleMono,
  type AmrCodecAdapter,
  type PcmFrame,
} from '@/lib/callMediaPipeline'
import {
  buildAmrOctetAlignedPayload,
  buildRtpPacket,
  parseAmrOctetAlignedPayload,
  parseRtpPacket,
  type AmrFrame,
} from '@/lib/amrRtp'
import type { CallMediaInfo } from '@/types/call'

const media: CallMediaInfo = {
  codec: 'AMR',
  payloadType: 102,
  clockRate: 8000,
  channels: 1,
  dtmfPayloadType: 101,
  dtmfClockRate: 8000,
  ptimeMillis: 20,
}

const frame = (value: number): AmrFrame => ({
  frameType: 8,
  quality: true,
  data: new Uint8Array([value, value, value, value, value]),
})

const codec = (decoded: PcmFrame = { samples: new Float32Array([0.1, 0.2]), sampleRate: 8000 }) => {
  const adapter: AmrCodecAdapter = {
    decode: vi.fn(() => decoded),
    encode: vi.fn(() => [frame(9)]),
    close: vi.fn(),
  }
  return adapter
}

describe('call media pipeline', () => {
  it('decodes matching AMR RTP packets into PCM frames', async () => {
    const onRemotePcm = vi.fn()
    const adapter = codec()
    const pipeline = new CallMediaPipeline({
      media,
      codec: adapter,
      onRemotePcm,
      sendRtpPacket: vi.fn(),
      initialSequenceNumber: 7,
      initialTimestamp: 160,
      ssrc: 42,
    })
    const payload = buildAmrOctetAlignedPayload('AMR', [frame(3)])
    const packet = buildRtpPacket({
      payloadType: media.payloadType,
      sequenceNumber: 1,
      timestamp: 2,
      ssrc: 3,
      marker: false,
      payload,
    })

    await expect(pipeline.receiveRtpPacket(packet)).resolves.toBe(true)

    expect(adapter.decode).toHaveBeenCalledWith(frame(3))
    expect(onRemotePcm).toHaveBeenCalledWith({ samples: new Float32Array([0.1, 0.2]), sampleRate: 8000 })
  })

  it('ignores RTP packets for another payload type', async () => {
    const adapter = codec()
    const onRemotePcm = vi.fn()
    const pipeline = new CallMediaPipeline({
      media,
      codec: adapter,
      onRemotePcm,
      sendRtpPacket: vi.fn(),
    })
    const packet = buildRtpPacket({
      payloadType: media.dtmfPayloadType,
      sequenceNumber: 1,
      timestamp: 2,
      ssrc: 3,
      marker: false,
      payload: new Uint8Array([0]),
    })

    await expect(pipeline.receiveRtpPacket(packet)).resolves.toBe(false)

    expect(adapter.decode).not.toHaveBeenCalled()
    expect(onRemotePcm).not.toHaveBeenCalled()
  })

  it('skips AMR no-data frames without invoking the codec', async () => {
    const adapter = codec()
    const onRemotePcm = vi.fn()
    const pipeline = new CallMediaPipeline({
      media,
      codec: adapter,
      onRemotePcm,
      sendRtpPacket: vi.fn(),
    })
    const payload = buildAmrOctetAlignedPayload('AMR', [
      { frameType: 15, quality: false, data: new Uint8Array() },
    ])
    const packet = buildRtpPacket({
      payloadType: media.payloadType,
      sequenceNumber: 1,
      timestamp: 2,
      ssrc: 3,
      marker: false,
      payload,
    })

    await expect(pipeline.receiveRtpPacket(packet)).resolves.toBe(true)

    expect(adapter.decode).not.toHaveBeenCalled()
    expect(onRemotePcm).not.toHaveBeenCalled()
  })

  it('encodes local PCM into timestamped RTP packets', async () => {
    const sent: Uint8Array<ArrayBuffer>[] = []
    const adapter = codec()
    const pipeline = new CallMediaPipeline({
      media,
      codec: adapter,
      onRemotePcm: vi.fn(),
      sendRtpPacket: (packet) => {
        sent.push(packet)
        return true
      },
      initialSequenceNumber: 10,
      initialTimestamp: 1000,
      ssrc: 55,
    })

    await expect(pipeline.sendPcm(new Float32Array(159), 8000)).resolves.toBe(0)
    await expect(pipeline.sendPcm(new Float32Array([1]), 8000)).resolves.toBe(1)

    const encodedSamples = vi.mocked(adapter.encode).mock.calls[0]?.[0]
    expect(encodedSamples).toBeDefined()
    if (!encodedSamples) return
    expect(encodedSamples).toHaveLength(160)
    expect(encodedSamples[0]).toBe(0)
    expect(encodedSamples[159]).toBe(1)
    expect(sent).toHaveLength(1)

    const packet = sent[0]
    expect(packet).toBeDefined()
    if (!packet) return
    const rtp = parseRtpPacket(packet)
    expect(rtp.payloadType).toBe(102)
    expect(rtp.sequenceNumber).toBe(10)
    expect(rtp.timestamp).toBe(1000)
    expect(rtp.ssrc).toBe(55)
    expect(rtp.marker).toBe(true)
    expect(parseAmrOctetAlignedPayload('AMR', rtp.payload).frames).toEqual([frame(9)])
  })

  it('decodes matching PCMU RTP packets into PCM frames', async () => {
    const onRemotePcm = vi.fn()
    const pipeline = new CallMediaPipeline({
      media: { ...media, codec: 'PCMU', payloadType: 0 },
      onRemotePcm,
      sendRtpPacket: vi.fn(),
    })
    const packet = buildRtpPacket({
      payloadType: 0,
      sequenceNumber: 1,
      timestamp: 2,
      ssrc: 3,
      marker: false,
      payload: encodePCMU(new Float32Array([0, 0.5, -0.5])),
    })

    await expect(pipeline.receiveRtpPacket(packet)).resolves.toBe(true)

    expect(onRemotePcm).toHaveBeenCalledWith({
      samples: expect.any(Float32Array),
      sampleRate: 8000,
    })
  })

  it('encodes local PCM into PCMU RTP packets', async () => {
    const sent: Uint8Array<ArrayBuffer>[] = []
    const pipeline = new CallMediaPipeline({
      media: { ...media, codec: 'PCMU', payloadType: 0 },
      onRemotePcm: vi.fn(),
      sendRtpPacket: (packet) => {
        sent.push(packet)
        return true
      },
      initialSequenceNumber: 10,
      initialTimestamp: 1000,
      ssrc: 55,
    })

    await expect(pipeline.sendPcm(new Float32Array(160), 8000)).resolves.toBe(1)

    const packet = sent[0]
    expect(packet).toBeDefined()
    if (!packet) return
    const rtp = parseRtpPacket(packet)
    expect(rtp.payloadType).toBe(0)
    expect(rtp.payload).toHaveLength(160)
    expect(decodePCMU(rtp.payload)).toHaveLength(160)
  })

  it('resamples mono PCM with linear interpolation', () => {
    const out = resampleMono(new Float32Array([0, 1, 0]), 3, 6)

    expect(Array.from(out)).toEqual([0, 0.5, 1, 0.5, 0, 0])
  })

  it('rejects codecs that the browser pipeline cannot handle', () => {
    expect(
      () =>
        new CallMediaPipeline({
          media: { ...media, codec: 'EVS' },
          codec: codec(),
          onRemotePcm: vi.fn(),
          sendRtpPacket: vi.fn(),
        }),
    ).toThrow('call media codec EVS is not supported')
  })
})

export type AmrCodec = 'AMR' | 'AMR-WB'

export type RtpPacket = {
  payloadType: number
  sequenceNumber: number
  timestamp: number
  ssrc: number
  marker: boolean
  payload: Uint8Array<ArrayBuffer>
}

export type AmrFrame = {
  frameType: number
  quality: boolean
  data: Uint8Array<ArrayBuffer>
}

const rtpHeaderSize = 12
const rtpVersion = 2
const noModeRequest = 15

const amrFrameBytes = [12, 13, 15, 17, 19, 20, 26, 31, 5]
const amrWbFrameBytes = [17, 23, 32, 36, 40, 46, 50, 58, 60, 5]

const toUint8 = (packet: ArrayBuffer | Uint8Array<ArrayBuffer>) =>
  packet instanceof ArrayBuffer ? new Uint8Array(packet) : packet

const byteAt = (data: Uint8Array<ArrayBuffer>, offset: number) => {
  const value = data[offset]
  if (value === undefined) {
    throw new Error('packet is truncated')
  }
  return value
}

const frameBytes = (codec: AmrCodec, frameType: number) => {
  if (frameType === 15 || (codec === 'AMR-WB' && frameType === 14)) {
    return 0
  }
  const table = codec === 'AMR-WB' ? amrWbFrameBytes : amrFrameBytes
  return table[frameType] ?? -1
}

export const parseRtpPacket = (packet: ArrayBuffer | Uint8Array<ArrayBuffer>): RtpPacket => {
  const data = toUint8(packet)
  if (data.byteLength < rtpHeaderSize) {
    throw new Error('rtp packet is too short')
  }
  const first = byteAt(data, 0)
  const second = byteAt(data, 1)
  const version = first >> 6
  if (version !== rtpVersion) {
    throw new Error('rtp version is not supported')
  }

  const hasPadding = (first & 0x20) !== 0
  const hasExtension = (first & 0x10) !== 0
  const csrcCount = first & 0x0f
  const marker = (second & 0x80) !== 0
  const payloadType = second & 0x7f
  const sequenceNumber = (byteAt(data, 2) << 8) | byteAt(data, 3)
  const timestamp =
    byteAt(data, 4) * 0x1000000 + (byteAt(data, 5) << 16) + (byteAt(data, 6) << 8) + byteAt(data, 7)
  const ssrc =
    byteAt(data, 8) * 0x1000000 + (byteAt(data, 9) << 16) + (byteAt(data, 10) << 8) + byteAt(data, 11)

  let offset = rtpHeaderSize + csrcCount * 4
  if (offset > data.byteLength) {
    throw new Error('rtp csrc list is truncated')
  }
  if (hasExtension) {
    if (offset + 4 > data.byteLength) {
      throw new Error('rtp extension header is truncated')
    }
    const extensionWords = (byteAt(data, offset + 2) << 8) | byteAt(data, offset + 3)
    offset += 4 + extensionWords * 4
    if (offset > data.byteLength) {
      throw new Error('rtp extension payload is truncated')
    }
  }

  let end = data.byteLength
  if (hasPadding) {
    const padding = byteAt(data, data.byteLength - 1)
    if (padding === 0 || padding > data.byteLength - offset) {
      throw new Error('rtp padding is invalid')
    }
    end -= padding
  }

  return {
    payloadType,
    sequenceNumber,
    timestamp,
    ssrc,
    marker,
    payload: data.slice(offset, end),
  }
}

export const buildRtpPacket = (packet: Omit<RtpPacket, 'payload'> & { payload: Uint8Array<ArrayBuffer> }) => {
  const out = new Uint8Array(rtpHeaderSize + packet.payload.byteLength)
  out[0] = rtpVersion << 6
  out[1] = packet.payloadType & 0x7f
  if (packet.marker) {
    out[1] |= 0x80
  }
  out[2] = (packet.sequenceNumber >> 8) & 0xff
  out[3] = packet.sequenceNumber & 0xff
  out[4] = (packet.timestamp >>> 24) & 0xff
  out[5] = (packet.timestamp >>> 16) & 0xff
  out[6] = (packet.timestamp >>> 8) & 0xff
  out[7] = packet.timestamp & 0xff
  out[8] = (packet.ssrc >>> 24) & 0xff
  out[9] = (packet.ssrc >>> 16) & 0xff
  out[10] = (packet.ssrc >>> 8) & 0xff
  out[11] = packet.ssrc & 0xff
  out.set(packet.payload, rtpHeaderSize)
  return out
}

export const parseAmrOctetAlignedPayload = (codec: AmrCodec, payload: Uint8Array<ArrayBuffer>) => {
  if (payload.byteLength < 2) {
    throw new Error('amr payload is too short')
  }

  const cmr = byteAt(payload, 0) >> 4
  const frames: AmrFrame[] = []
  let offset = 1
  let hasMore = true
  while (hasMore) {
    if (offset >= payload.byteLength) {
      throw new Error('amr table of contents is truncated')
    }
    const toc = byteAt(payload, offset)
    offset += 1
    hasMore = (toc & 0x80) !== 0
    const frameType = (toc >> 3) & 0x0f
    const quality = (toc & 0x04) !== 0
    const size = frameBytes(codec, frameType)
    if (size < 0) {
      throw new Error('amr frame type is not supported')
    }
    frames.push({ frameType, quality, data: new Uint8Array(size) })
  }

  for (const frame of frames) {
    const end = offset + frame.data.byteLength
    if (end > payload.byteLength) {
      throw new Error('amr speech data is truncated')
    }
    frame.data.set(payload.slice(offset, end))
    offset = end
  }

  return { cmr, frames }
}

export const buildAmrOctetAlignedPayload = (
  codec: AmrCodec,
  frames: AmrFrame[],
  cmr = noModeRequest,
) => {
  if (frames.length === 0) {
    throw new Error('amr payload requires at least one frame')
  }
  const speechBytes = frames.reduce((sum, frame) => {
    const want = frameBytes(codec, frame.frameType)
    if (want < 0) {
      throw new Error('amr frame type is not supported')
    }
    if (frame.data.byteLength !== want) {
      throw new Error('amr frame size does not match frame type')
    }
    return sum + frame.data.byteLength
  }, 0)

  const out = new Uint8Array(1 + frames.length + speechBytes)
  out[0] = (cmr & 0x0f) << 4
  let offset = 1
  for (const [index, frame] of frames.entries()) {
    const toc = ((index < frames.length - 1 ? 1 : 0) << 7) | ((frame.frameType & 0x0f) << 3)
    out[offset] = toc
    if (frame.quality) {
      out[offset] = toc | 0x04
    }
    offset += 1
  }
  for (const frame of frames) {
    out.set(frame.data, offset)
    offset += frame.data.byteLength
  }
  return out
}

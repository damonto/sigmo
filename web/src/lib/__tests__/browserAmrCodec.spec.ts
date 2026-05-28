import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { createBrowserAmrCodec, hasBrowserAmrCodec } from '@/lib/browserAmrCodec'
import type { AmrCodecAdapter } from '@/lib/callMediaPipeline'
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

describe('browser AMR codec loader', () => {
  beforeAll(async () => {
    await installGeneratedOpenCoreAmrFactory()
  })

  afterAll(() => {
    uninstallGeneratedOpenCoreAmrFactory()
  })

  it('routes AMR-NB to the built-in WASM codec', async () => {
    expect(hasBrowserAmrCodec()).toBe(true)
    const codec = (await createBrowserAmrCodec(media('AMR'))) as AmrCodecAdapter
    try {
      const frames = await codec.encode(new Float32Array(160), 8000)
      expect(frames[0]).toMatchObject({ frameType: 7, quality: true })
    } finally {
      await codec.close?.()
    }
  })

  it('accepts PCMU without loading an AMR module', async () => {
    expect(createBrowserAmrCodec(media('PCMU'))).toEqual({
      decode: expect.any(Function),
      encode: expect.any(Function),
    })
  })

  it('routes AMR-WB to the built-in WASM codec', async () => {
    expect(hasBrowserAmrCodec()).toBe(true)
    const codec = (await createBrowserAmrCodec(media('AMR-WB'))) as AmrCodecAdapter
    try {
      const frames = await codec.encode(new Float32Array(320), 16000)
      expect(frames[0]).toMatchObject({ frameType: 8, quality: true })
    } finally {
      await codec.close?.()
    }
  })
})

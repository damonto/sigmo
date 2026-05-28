import { describe, expect, it } from 'vitest'

import { createBrowserAmrCodec, hasBrowserAmrCodec } from '@/lib/browserAmrCodec'
import type { CallMediaInfo } from '@/types/call'

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
  it('loads the built-in AMR-NB codec', async () => {
    expect(hasBrowserAmrCodec()).toBe(true)
    await expect(createBrowserAmrCodec(media('AMR'))).resolves.toEqual({
      decode: expect.any(Function),
      encode: expect.any(Function),
      close: expect.any(Function),
    })
  })

  it('accepts PCMU without loading an AMR module', async () => {
    expect(createBrowserAmrCodec(media('PCMU'))).toEqual({
      decode: expect.any(Function),
      encode: expect.any(Function),
    })
  })

  it('rejects codecs that the built-in adapter cannot encode', async () => {
    expect(hasBrowserAmrCodec()).toBe(true)
    await expect(createBrowserAmrCodec(media('AMR-WB'))).rejects.toThrow(
      'AMR-WB codec is not available',
    )
  })
})

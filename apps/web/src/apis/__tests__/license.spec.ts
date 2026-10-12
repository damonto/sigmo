import { afterEach, describe, expect, it, vi } from 'vitest'

import { useLicenseApi } from '@/apis/license'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('license API', () => {
  it('decodes a Telegram pairing and disables status caching', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        Response.json({
          authorized: false,
          deviceId: '0123456789abcdef0123456789abcdef',
        }),
      )
      .mockResolvedValueOnce(
        Response.json(
          {
            id: 'pairing-id',
            activationUrl: 'https://t.me/SigmoProBot?start=pairing-id',
            status: 'pending',
            expiresAt: '2099-08-09T12:00:00Z',
          },
          { status: 201 },
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    const api = useLicenseApi()
    const statusResponse = await api.status()
    await expect(api.decodeStatus(statusResponse)).resolves.toEqual({
      authorized: false,
      deviceId: '0123456789abcdef0123456789abcdef',
    })
    await expect(api.createPairing()).resolves.toMatchObject({
      id: 'pairing-id',
      status: 'pending',
    })
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ cache: 'no-store' })
  })

  it.each([
    'https://t.me/SigmoProBot?start=other',
    'https://t.me/not-a-bot?start=pairing-id',
    'https://t.me/SigmoProBot?start=pairing-id&next=evil',
    'https://user:password@t.me/SigmoProBot?start=pairing-id',
  ])('rejects unsafe Telegram activation URL %s', async (activationUrl) => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json(
          {
            id: 'pairing-id',
            activationUrl,
            status: 'pending',
            expiresAt: '2099-08-09T12:00:00Z',
          },
          { status: 201 },
        ),
      ),
    )

    await expect(useLicenseApi().createPairing()).rejects.toThrow(
      'invalid Telegram activation URL',
    )
  })
})

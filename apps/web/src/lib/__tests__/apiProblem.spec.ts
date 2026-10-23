import { afterEach, describe, expect, it, vi } from 'vitest'

import fixtures from '../../../../../internal/pkg/apierror/testdata/problems.json'
import {
  createApiError,
  fetchWithBoundedErrors,
  isApiError,
  isLocalAuthenticationError,
  parseRetryAfter,
} from '../apiProblem'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('HTTP Problem Details', () => {
  it('distinguishes backend errors already handled by the fetch layer', () => {
    expect(isApiError({ source: 'backend', status: 422, error_code: 'test_auth_invalid' })).toBe(
      true,
    )
    expect(isApiError(new Error('invalid response'))).toBe(false)
  })

  it.each(fixtures)('decodes the shared fixture: $name', (fixture) => {
    const now = Date.parse('2026-10-09T00:00:00Z')
    vi.useFakeTimers()
    vi.setSystemTime(now)
    const body = fixture.repeat > 0 ? fixture.body.repeat(fixture.repeat) : fixture.body
    const headers = new Headers()
    for (const [name, value] of Object.entries(fixture.headers)) {
      if (typeof value === 'string') headers.set(name, value)
    }
    const response = new Response(body, { status: fixture.status, headers })
    const error = createApiError(response, body)
    expect(error.status).toBe(fixture.status)
    expect(error.error_code).toBe(fixture.code || 'unknown_error')
    expect(error.request_id).toBe(fixture.headers['X-Request-ID'])
    expect(error.allow).toBe(fixture.headers.Allow)
    expect(error.retryAfter).toBe(fixture.headers['Retry-After'])
    expect(error.authenticate).toBe(fixture.headers['WWW-Authenticate'])
    // The caller owns the source. Remote JSON never chooses its trust boundary.
    expect(isLocalAuthenticationError({ ...error, source: fixture.source })).toBe(
      fixture.loginInvalid,
    )
    if (fixture.name === 'numeric retry after' || fixture.name === 'HTTP date retry after') {
      expect(error.retryAt).toBe(now + 90_000)
    }
  })

  it.each(['', '-1', '+1', '1.5', '2147483648', '2026-10-10', 'Thu, 08 Oct 2026 00:00:00 GMT'])(
    'ignores invalid or past Retry-After %s',
    (value) => {
      expect(parseRetryAfter(value, Date.parse('2026-10-09T00:00:00Z'))).toBeUndefined()
    },
  )

  it('cancels an oversized error stream while preserving transport metadata', async () => {
    const cancel = vi.fn()
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(4096))
      },
      cancel,
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(stream, {
          status: 503,
          headers: {
            'Content-Type': 'application/problem+json',
            'Retry-After': '90',
            'X-Request-ID': 'large',
            Allow: 'POST',
          },
        }),
      ),
    )
    const response = await fetchWithBoundedErrors('/api/v1/modems')
    const error = createApiError(response, await response.text())
    expect(cancel).toHaveBeenCalledOnce()
    expect(error).toMatchObject({
      status: 503,
      error_code: 'unknown_error',
      request_id: 'large',
      retryAfter: '90',
      allow: 'POST',
    })
  })

  it('retains status after an interrupted error body and does not buffer success downloads', async () => {
    const failure = new Response(
      new ReadableStream({
        start(controller) {
          controller.error(new Error('interrupted'))
        },
      }),
      {
        status: 401,
        headers: { 'X-Request-ID': 'interrupted' },
      },
    )
    const download = new Response('file contents', { status: 200 })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(failure).mockResolvedValueOnce(download))
    const response = await fetchWithBoundedErrors('/api/v1/modems')
    const error = createApiError(response, await response.text())
    expect(error).toMatchObject({
      status: 401,
      request_id: 'interrupted',
      error_code: 'unknown_error',
    })
    expect(isLocalAuthenticationError(error)).toBe(false)
    expect(await fetchWithBoundedErrors('/api/v1/download')).toBe(download)
    expect(download.bodyUsed).toBe(false)
  })
})

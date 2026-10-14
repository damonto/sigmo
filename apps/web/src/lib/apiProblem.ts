import catalog from '../../../../internal/pkg/apierror/catalog.json'
import type { ApiErrorResponse } from '@/types/api'

export const PROBLEM_MEDIA_TYPE = 'application/problem+json'
export const API_ACCEPT = `application/json, ${PROBLEM_MEDIA_TYPE}`
const TYPE_PREFIX = 'https://sigmo-pro.952007.xyz/problems/'
const MAX_ERROR_BYTES = 64 * 1024

/** ApiError is a UI model, not the Problem Details wire representation. */
export interface ApiError extends ApiErrorResponse {
  status: number
  source: 'backend'
  type?: string
  retryAfter?: string
  retryAt?: number
  allow?: string
  authenticate?: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** parseRetryAfter returns a deadline; an invalid or past value imposes no wait. */
export function parseRetryAfter(value: string | null, now = Date.now()): number | undefined {
  if (!value) return undefined
  const trimmed = value.trim()
  if (/^\d+$/.test(trimmed)) {
    const seconds = Number(trimmed)
    return seconds <= 2_147_483_647 ? now + seconds * 1000 : undefined
  }
  // Date.parse also accepts plain numbers and ISO dates, neither is an HTTP-date.
  if (!/^(?:[A-Za-z]{3},|[A-Za-z]+,|[A-Za-z]{3} )/.test(trimmed)) return undefined
  const deadline = Date.parse(trimmed)
  return Number.isFinite(deadline) && deadline > now ? deadline : undefined
}

/** createApiError keeps HTTP metadata even when the body violates the contract. */
export function createApiError(response: Response, data?: unknown): ApiError {
  const requestID = response.headers.get('X-Request-ID') ?? undefined
  const error: ApiError = {
    status: response.status,
    source: 'backend',
    error_code: 'unknown_error',
    message: `Error ${response.status}: ${response.statusText}`,
    request_id: requestID,
    retryAfter: response.headers.get('Retry-After') ?? undefined,
    retryAt: parseRetryAfter(response.headers.get('Retry-After')),
    allow: response.headers.get('Allow') ?? undefined,
    authenticate: response.headers.get('WWW-Authenticate') ?? undefined,
  }
  if (
    response.headers.get('Content-Type')?.split(';')[0]?.trim().toLowerCase() !== PROBLEM_MEDIA_TYPE
  )
    return error
  if (typeof data === 'string') {
    if (new TextEncoder().encode(data).byteLength > MAX_ERROR_BYTES) return error
    try {
      data = JSON.parse(data) as unknown
    } catch {
      return error
    }
  }
  if (!isRecord(data)) return error
  if (typeof data.type === 'string') error.type = data.type
  if (!error.type?.startsWith(TYPE_PREFIX)) return error
  const code = error.type.slice(TYPE_PREFIX.length)
  if (
    !Object.prototype.hasOwnProperty.call(catalog, code) ||
    data.status !== response.status ||
    typeof data.title !== 'string' ||
    !data.title ||
    (data.error_code != null && data.error_code !== code) ||
    (data.detail != null && typeof data.detail !== 'string') ||
    (data.request_id != null && typeof data.request_id !== 'string') ||
    (requestID &&
      typeof data.request_id === 'string' &&
      data.request_id &&
      data.request_id !== requestID)
  )
    return error
  error.error_code = code
  if (typeof data.detail === 'string' && data.detail) error.message = data.detail
  if (!requestID && typeof data.request_id === 'string') error.request_id = data.request_id
  return error
}

/** isLocalAuthenticationError is shared by token removal and navigation. */
export function isLocalAuthenticationError(value: unknown): boolean {
  return (
    isRecord(value) &&
    value.source === 'backend' &&
    value.status === 401 &&
    value.type === `${TYPE_PREFIX}invalid_session` &&
    value.error_code === 'invalid_session'
  )
}

/** fetchWithBoundedErrors stops reading failures at 64 KiB before VueUse or a
 * resource API decodes them. Success bodies keep their existing streaming rules.
 */
export const fetchWithBoundedErrors: typeof fetch = async (input, init) => {
  const response = await globalThis.fetch(input, init)
  if (response.ok || !response.body) return response
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  let complete = false
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) {
        complete = true
        break
      }
      size += value.byteLength
      if (size > MAX_ERROR_BYTES) {
        await reader.cancel()
        break
      }
      chunks.push(value)
    }
  } catch {
    // A read failure still has useful HTTP status/headers; discard partial JSON.
  } finally {
    reader.releaseLock()
  }
  let body: Uint8Array<ArrayBuffer> | null = null
  if (complete) {
    body = new Uint8Array(size)
    let offset = 0
    for (const chunk of chunks) {
      body.set(chunk, offset)
      offset += chunk.byteLength
    }
  }
  const headers = new Headers(response.headers)
  headers.delete('Content-Length')
  return new Response(body, { status: response.status, statusText: response.statusText, headers })
}

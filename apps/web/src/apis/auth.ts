import {
  API_ACCEPT,
  createApiError,
  fetchWithBoundedErrors,
  isLocalAuthenticationError,
} from '@/lib/apiProblem'
import { resolveAPIURL } from '@/lib/apiUrl'
import type { AuthMethods, OTPChallenge } from '@/types/auth'

const request = async (path: string, options: RequestInit = {}) => {
  const response = await fetchWithBoundedErrors(resolveAPIURL(path), {
    ...options,
    credentials: 'same-origin',
    headers: { Accept: API_ACCEPT, 'Content-Type': 'application/json' },
  })
  if (!response.ok) throw createApiError(response, await response.text())
  return response
}

/** useAuthApi uses host-only cookies; no browser-readable bearer credentials exist. */
export const useAuthApi = () => ({
  async methods(): Promise<AuthMethods> {
    const response = await request('auth/methods')
    const value: unknown = await response.json()
    if (
      !value ||
      typeof value !== 'object' ||
      !('version' in value) ||
      value.version !== 1 ||
      !('mode' in value) ||
      (value.mode !== 'none' &&
        value.mode !== 'otp' &&
        value.mode !== 'oidc' &&
        value.mode !== 'cloudflare_access') ||
      !('methods' in value) ||
      !Array.isArray(value.methods) ||
      !value.methods.every(
        (method: unknown): method is 'otp' | 'oidc' => method === 'otp' || method === 'oidc',
      ) ||
      (value.mode === 'otp' || value.mode === 'oidc'
        ? value.methods.length !== 1 || value.methods[0] !== value.mode
        : value.methods.length !== 0)
    )
      throw new Error('Invalid authentication response')
    return { version: 1, mode: value.mode, methods: value.methods }
  },
  async session(): Promise<boolean> {
    const response = await fetchWithBoundedErrors(resolveAPIURL('auth/sessions/current'), {
      credentials: 'same-origin',
      headers: { Accept: API_ACCEPT },
    })
    if (response.status === 401) return false
    if (!response.ok) throw createApiError(response, await response.text())
    return true
  },
  async challenge(): Promise<OTPChallenge> {
    const value: unknown = await (
      await request('auth/otp/challenges', { method: 'POST', body: '{}' })
    ).json()
    if (
      !value ||
      typeof value !== 'object' ||
      !('id' in value) ||
      typeof value.id !== 'string' ||
      !value.id ||
      !('expiresAt' in value) ||
      typeof value.expiresAt !== 'string' ||
      !(Date.parse(value.expiresAt) > Date.now())
    )
      throw new Error('Invalid OTP challenge')
    return { id: value.id, expiresAt: value.expiresAt }
  },
  async verify(challengeId: string, code: string): Promise<void> {
    await request('auth/sessions', {
      method: 'POST',
      body: JSON.stringify({ method: 'otp', client: 'web', challengeId, code }),
    })
  },
  async browser(): Promise<string> {
    const api = new URL(resolveAPIURL('auth/browser-sessions'))
    const endpoint = '/api/v1/auth/browser-sessions'
    if (!api.pathname.endsWith(endpoint)) throw new Error('Invalid authentication endpoint')
    const response = await request('auth/browser-sessions', {
      method: 'POST',
      body: JSON.stringify({ client: 'web', basePath: api.pathname.slice(0, -endpoint.length) }),
    })
    const result: unknown = await response.json()
    if (
      !result ||
      typeof result !== 'object' ||
      !('authorizationUrl' in result) ||
      typeof result.authorizationUrl !== 'string'
    )
      throw new Error('Invalid authorization response')
    const target = new URL(result.authorizationUrl)
    if (
      target.origin !== api.origin ||
      target.username ||
      target.password ||
      target.hash ||
      target.search ||
      !target.pathname.startsWith(api.pathname + '/')
    )
      throw new Error('Unexpected login URL')
    return target.toString()
  },
  async logout(): Promise<void> {
    try {
      await request('auth/sessions/current', { method: 'DELETE' })
    } catch (error) {
      // An expired local session is already signed out; provider and transport
      // failures must still be visible instead of pretending revocation succeeded.
      if (!isLocalAuthenticationError(error)) throw error
    }
  },
})

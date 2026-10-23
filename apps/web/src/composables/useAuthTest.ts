import { onScopeDispose, ref, toRaw, type Ref } from 'vue'

import { useSettingsApi } from '@/apis/settings'
import { resolveAPIURL } from '@/lib/apiUrl'
import type {
  SettingsAuth,
  SettingsAuthTestResult,
  SettingsOIDCTestSession,
} from '@/types/settings'

const AUTH_TEST_POLL_MS = 1_000

const clone = <T>(value: T): T => {
  return structuredClone(toRaw(value))
}

const isAuthTestResult = (value: unknown): value is SettingsAuthTestResult => {
  if (!value || typeof value !== 'object' || !('status' in value)) return false
  const status = value.status
  if (status !== 'pending' && status !== 'succeeded' && status !== 'failed') return false
  if ('reason' in value && value.reason !== undefined && typeof value.reason !== 'string')
    return false
  return !('subject' in value) || value.subject === undefined || typeof value.subject === 'string'
}

const isOIDCTestSession = (value: unknown): value is SettingsOIDCTestSession => {
  if (!value || typeof value !== 'object') return false
  if (!('id' in value) || typeof value.id !== 'string' || !value.id) return false
  if (!('authorizationUrl' in value) || typeof value.authorizationUrl !== 'string') return false
  if (!('expiresAt' in value) || typeof value.expiresAt !== 'string') return false
  const expiresAt = Date.parse(value.expiresAt)
  return Number.isFinite(expiresAt) && expiresAt > Date.now()
}

const delay = (milliseconds: number, signal: AbortSignal) => {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException('Authentication test cancelled', 'AbortError'))
      return
    }
    const finish = (callback: () => void) => {
      clearTimeout(timer)
      signal.removeEventListener('abort', abort)
      callback()
    }
    const abort = () =>
      finish(() => reject(new DOMException('Authentication test cancelled', 'AbortError')))
    const timer = setTimeout(() => finish(resolve), milliseconds)
    signal.addEventListener('abort', abort, { once: true })
  })
}

const isAbortError = (error: unknown) => {
  return error instanceof DOMException && error.name === 'AbortError'
}

const closePopup = (popup: Window | null) => {
  if (!popup) return
  try {
    popup.close()
  } catch {
    // A browser may reject close after the popup navigates cross-origin.
  }
}

/**
 * Runs authentication checks against the current draft without persisting it.
 * OIDC uses a popup because provider login pages generally reject framing.
 */
export const useAuthTest = (auth: Ref<SettingsAuth | null>, isSaving: Ref<boolean>) => {
  const api = useSettingsApi()
  const isTesting = ref(false)
  let controller: AbortController | null = null
  let popup: Window | null = null

  const testOTP = async (): Promise<SettingsAuthTestResult | null> => {
    if (!auth.value?.otpChannels.length) return null
    await api.testAuth({ channels: clone(auth.value.otpChannels) })
    return { status: 'succeeded' }
  }

  const testAccess = async (): Promise<SettingsAuthTestResult | null> => {
    if (!auth.value) return null
    const { data } = await api.testCloudflareAccess(clone(auth.value))
    return isAuthTestResult(data.value) && data.value.status !== 'pending' ? data.value : null
  }

  const pollOIDCTest = async (
    id: string,
    expiresAt: string,
    currentPopup: Window,
    signal: AbortSignal,
  ): Promise<SettingsAuthTestResult> => {
    const deadline = Date.parse(expiresAt)
    while (!signal.aborted) {
      const { data } = await api.getOIDCTest(id)
      if (!isAuthTestResult(data.value)) throw new Error('Invalid OIDC test result')
      if (data.value.status !== 'pending') return data.value
      if (Date.now() >= deadline) return { status: 'failed', reason: 'expired' }
      if (currentPopup.closed) return { status: 'failed', reason: 'cancelled' }
      await delay(AUTH_TEST_POLL_MS, signal)
    }
    return { status: 'failed', reason: 'cancelled' }
  }

  const testOIDC = async (signal: AbortSignal): Promise<SettingsAuthTestResult | null> => {
    if (!auth.value) return null
    if (window.location.protocol !== 'https:') {
      return { status: 'failed', reason: 'https_required' }
    }
    popup = window.open('', '_blank')
    if (!popup) return { status: 'failed', reason: 'popup_blocked' }
    try {
      popup.opener = null
    } catch {
      // Some browsers expose opener as read-only; the provider URL is still same-origin.
    }
    const endpoint = new URL(resolveAPIURL('settings/auth-tests/oidc'))
    const marker = '/api/v1/settings/auth-tests/oidc'
    if (!endpoint.pathname.endsWith(marker)) throw new Error('Invalid authentication endpoint')
    const { data } = await api.createOIDCTest({
      client: 'web',
      basePath: endpoint.pathname.slice(0, -marker.length),
      state: '',
      auth: clone(auth.value),
    })
    const session = data.value
    if (!isOIDCTestSession(session)) throw new Error('Invalid OIDC test response')
    const target = new URL(session.authorizationUrl)
    if (
      target.origin !== window.location.origin ||
      target.username ||
      target.password ||
      target.search ||
      target.hash ||
      !target.pathname.endsWith(`/settings/auth-tests/oidc/${session.id}/browser`)
    ) {
      throw new Error('Unexpected OIDC test URL')
    }
    if (popup.closed) return { status: 'failed', reason: 'cancelled' }
    popup.location.replace(target.toString())
    return pollOIDCTest(session.id, session.expiresAt, popup, signal)
  }

  const test = async (): Promise<SettingsAuthTestResult | null> => {
    const current = auth.value
    if (!current || isSaving.value || isTesting.value) return null
    isTesting.value = true
    controller = new AbortController()
    try {
      switch (current.mode) {
        case 'otp':
          return await testOTP()
        case 'oidc':
          return await testOIDC(controller.signal)
        case 'cloudflare_access':
          return await testAccess()
        default:
          return null
      }
    } catch (error) {
      if (isAbortError(error)) return { status: 'failed', reason: 'cancelled' }
      throw error
    } finally {
      closePopup(popup)
      popup = null
      controller = null
      isTesting.value = false
    }
  }

  onScopeDispose(() => {
    controller?.abort()
    closePopup(popup)
  })

  return { isTesting, test }
}

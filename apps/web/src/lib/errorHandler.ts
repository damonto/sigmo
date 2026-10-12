import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

import { createApiError, isLocalAuthenticationError, type ApiError } from './apiProblem'
import { clearStoredToken } from './authStorage'
import { notifyError } from './notify'

export { createApiError } from './apiProblem'
export type { ApiError } from './apiProblem'

const titleFromStatus = (status?: number) => {
  switch (status) {
    case 400:
    case 422:
      return 'Invalid Request'
    case 401:
      return 'Unauthorized'
    case 404:
      return 'Not Found'
    case 408:
      return 'Request Timeout'
    case 409:
      return 'Conflict'
    case 429:
      return 'Too Many Requests'
    default:
      if (typeof status === 'number' && status >= 500) {
        return 'Server Error'
      }
      return 'Error'
  }
}

export const localizeErrorMessage = (error: unknown, defaultMessage = 'An error occurred') => {
  if (error && typeof error === 'object') {
    const apiError = error as Partial<ApiError>
    if (typeof apiError.error_code === 'string') {
      const languages = typeof navigator === 'undefined' ? [] : navigator.languages
      const language = languages.find((candidate) => {
        const normalized = candidate.toLowerCase()
        return normalized.startsWith('en') || normalized.startsWith('zh')
      })
      const messages: Record<string, string> = language?.toLowerCase().startsWith('zh')
        ? zh.errors
        : en.errors
      const translated = messages[apiError.error_code]
      if (translated) return translated
    }
    if (typeof apiError.message === 'string') return apiError.message
  }
  return defaultMessage
}

const resolveErrorInfo = (error: unknown, defaultMessage: string) => {
  let message = defaultMessage
  let title = 'Error'
  let requestId = ''

  if (error && typeof error === 'object') {
    const apiError = error as Partial<ApiError>
    message = localizeErrorMessage(error, defaultMessage)
    if (typeof apiError.request_id === 'string') {
      requestId = apiError.request_id
    }
    if (typeof apiError.status === 'number') {
      title = titleFromStatus(apiError.status)
    }
  }

  return { message, title, requestId }
}

/**
 * Error handler for API errors
 * Shows error messages to users
 */
export const handleError = (error: unknown, defaultMessage = 'An error occurred') => {
  const { message, title, requestId } = resolveErrorInfo(error, defaultMessage)

  notifyError(title, message)

  // Log to console for debugging
  console.error('[Error]', requestId ? { requestId, error } : error)
}

/**
 * Handle API response errors
 */
export const handleResponseError = (response: Response, data?: unknown) => {
  const apiError = createApiError(response, data)

  if (isLocalAuthenticationError(apiError)) {
    clearStoredToken()
  }

  // A removable card can advertise eUICC capability in its ATR while not
  // exposing an ISD-R application. Callers still receive the typed error and
  // can render an empty eSIM state without showing a global failure toast.
  if (apiError.error_code !== 'euicc_not_supported') {
    handleError(apiError)
  }
  console.error('[API Error]', response.status, apiError)

  return Object.assign(new Error(apiError.message), apiError)
}

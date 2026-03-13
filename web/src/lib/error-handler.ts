import type { ApiErrorResponse } from '@/types/api'

import { clearStoredToken } from './auth-storage'

export type ApiError = ApiErrorResponse & {
  status?: number
}

/**
 * Global error handler interface
 * Will be initialized by useErrorHandler
 */
let showErrorFunction: ((message: string, title?: string) => void) | null = null

const extractErrorResponse = (data: unknown): ApiErrorResponse | null => {
  if (data && typeof data === 'object') {
    const record = data as Record<string, unknown>
    if (
      typeof record.error_code === 'string' &&
      typeof record.message === 'string' &&
      typeof record.request_id === 'string'
    ) {
      return {
        error_code: record.error_code,
        message: record.message,
        request_id: record.request_id,
      }
    }
  }

  return null
}

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

const resolveErrorInfo = (error: unknown, defaultMessage: string) => {
  let message = defaultMessage
  let title = 'Error'
  let requestId = ''

  if (error && typeof error === 'object') {
    const apiError = error as Partial<ApiError>
    if (typeof apiError.message === 'string') {
      message = apiError.message
    }
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
 * Register the global error handler
 * Called from App.vue to initialize error handling
 */
export const registerErrorHandler = (showError: (message: string, title?: string) => void) => {
  showErrorFunction = showError
}

/**
 * Error handler for API errors
 * Shows error messages to users
 */
export const handleError = (error: unknown, defaultMessage = 'An error occurred') => {
  const { message, title, requestId } = resolveErrorInfo(error, defaultMessage)

  if (showErrorFunction) {
    showErrorFunction(message, title)
  }

  // Log to console for debugging
  console.error('[Error]', requestId ? { requestId, error } : error)
}

/**
 * Handle API response errors
 */
export const handleResponseError = (response: Response, data?: unknown) => {
  const extracted = extractErrorResponse(data)
  const apiError: ApiError = extracted ?? {
    error_code: 'unknown_error',
    message: `Error ${response.status}: ${response.statusText}`,
    request_id: response.headers.get('X-Request-ID') ?? '',
  }
  apiError.status = response.status

  if (response.status === 401) {
    clearStoredToken()
  }

  handleError(apiError)
  console.error('[API Error]', response.status, apiError)

  return Object.assign(new Error(apiError.message), apiError)
}

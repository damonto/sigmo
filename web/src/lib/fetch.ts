import { createFetch, type UseFetchReturn } from '@vueuse/core'
import type { Ref } from 'vue'

import router from '@/router'

import { getStoredToken } from './authStorage'
import { handleError, handleResponseError } from './errorHandler'

const rawBaseUrl = import.meta.env.VITE_API_BASE_URL
const baseUrl =
  rawBaseUrl && rawBaseUrl.trim().length > 0 ? rawBaseUrl.replace(/\/$/, '') : '/api/v1'

const requestHeaders = (options: RequestInit) => {
  const headers = new Headers(options.headers)
  const token = getStoredToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const hasBody = options.body !== undefined && options.body !== null
  if (hasBody && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  return headers
}

/**
 * Custom fetch instance with global configuration.
 * Unified error handling - no need to handle errors in callers.
 */
export const useFetch = createFetch({
  baseUrl,
  options: {
    updateDataOnError: true,
    async beforeFetch({ options }) {
      options.headers = requestHeaders(options)

      return { options }
    },

    afterFetch({ response, data }) {
      // Unified logging in development
      if (import.meta.env.DEV) {
        console.log(`[API] ${response.url} → ${response.status}`, data)
      }

      return { response, data }
    },

    onFetchError({ response, error, data }) {
      if (response && response.status <= 299) {
        return { response, error, data }
      }

      if (response) {
        const handledError = handleResponseError(response, data)
        if (response.status === 401 && router.currentRoute.value.name !== 'auth') {
          router.replace({ name: 'auth' })
        }
        console.error('[API] Response error:', response.status, data)
        throw handledError
      } else {
        // Unified network error handling
        handleError(error, 'Network error occurred')
        console.error('[API] Network error:', error)
        throw error || new Error('Request failed')
      }
    },
  },
  fetchOptions: {
    mode: 'cors',
  },
})

export type ApiFetchReturn<T> = Omit<UseFetchReturn<string>, 'data'> & {
  data: Ref<T | undefined>
}

const parseResponseText = <T>(value: string | null) => {
  if (!value?.trim()) return undefined
  return JSON.parse(value) as T
}

export const fetchJson = async <T>(
  url: string,
  options?: RequestInit,
): Promise<ApiFetchReturn<T>> => {
  const request = options
    ? useFetch<string>(url, options, { immediate: false }).text()
    : useFetch<string>(url, { immediate: false }).text()

  await request.execute(true)

  const text = request.data.value
  const data = request.data as unknown as Ref<T | undefined>
  data.value = parseResponseText<T>(text)

  return request as unknown as ApiFetchReturn<T>
}

// Expected restart windows must not pass through the global error notifier.
export const fetchJsonQuietly = async <T>(url: string, options: RequestInit = {}): Promise<T> => {
  const response = await fetch(`${baseUrl}/${url.replace(/^\/+/, '')}`, {
    ...options,
    headers: requestHeaders(options),
    mode: 'cors',
  })
  if (!response.ok) throw new Error(`request returned HTTP ${response.status}`)

  const data = parseResponseText<T>(await response.text())
  if (data === undefined) throw new Error('request returned an empty response')
  return data
}

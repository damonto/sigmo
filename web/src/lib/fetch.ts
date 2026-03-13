import { createFetch } from '@vueuse/core'

import router from '@/router'

import { getStoredToken } from './auth-storage'
import { handleError, handleResponseError } from './error-handler'

const rawBaseUrl = import.meta.env.VITE_API_BASE_URL
const baseUrl =
  rawBaseUrl && rawBaseUrl.trim().length > 0 ? rawBaseUrl.replace(/\/$/, '') : '/api/v1'

/**
 * Custom fetch instance with global configuration
 * Unified error handling - no need to handle errors in callers
 */
export const useFetch = createFetch({
  baseUrl,
  options: {
    updateDataOnError: true,
    async beforeFetch({ options }) {
      const headers = new Headers(options.headers)

      // Add authentication token if available
      const token = getStoredToken()
      if (token) {
        headers.set('Authorization', `Bearer ${token}`)
      }

      const hasBody = options.body !== undefined && options.body !== null
      if (hasBody && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
        headers.set('Content-Type', 'application/json')
      }

      options.headers = headers

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

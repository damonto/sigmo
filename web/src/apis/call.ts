import { shallowRef, type Ref } from 'vue'

import { clearStoredToken, getStoredToken } from '@/lib/authStorage'
import { handleError } from '@/lib/errorHandler'

import type { ApiErrorResponse } from '@/types/api'
import type { CallRecord, DialCallRequest, UpdateCallRequest } from '@/types/call'

type CallApiResult<T> = {
  data: Ref<T | undefined>
}

type CallApiError = ApiErrorResponse & {
  status?: number
}

const extractErrorResponse = (data: unknown): ApiErrorResponse | null => {
  if (!data || typeof data !== 'object') return null
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
  return null
}

const buildCallApiError = (response: Response, data: unknown) => {
  const apiError: CallApiError = extractErrorResponse(data) ?? {
    error_code: 'unknown_error',
    message: `Error ${response.status}: ${response.statusText}`,
    request_id: response.headers.get('X-Request-ID') ?? '',
  }
  apiError.status = response.status
  return Object.assign(new Error(apiError.message), apiError)
}

const apiBase = () => {
  const rawBase = import.meta.env.VITE_API_BASE_URL as string | undefined
  return rawBase && rawBase.trim().length > 0 ? rawBase.replace(/\/$/, '') : '/api/v1'
}

const buildCallHttpUrl = (id: string, path: string) => {
  const apiUrl = new URL(apiBase(), window.location.origin)
  apiUrl.pathname = `${apiUrl.pathname.replace(/\/$/, '')}/modems/${id}/calls${path}`
  return apiUrl.toString()
}

const buildCallWebSocketUrl = (id: string, path: string) => {
  const base = apiBase()
  const apiUrl = new URL(base, window.location.origin)
  apiUrl.protocol = apiUrl.protocol === 'https:' ? 'wss:' : 'ws:'
  apiUrl.pathname = `${apiUrl.pathname.replace(/\/$/, '')}/modems/${id}/calls${path}`
  const token = getStoredToken()
  if (token) {
    apiUrl.searchParams.set('token', token)
  }
  return apiUrl.toString()
}

export const buildCallEventsUrl = (id: string) => buildCallWebSocketUrl(id, '/events')

export const buildCallMediaUrl = (id: string, callID: string) =>
  buildCallWebSocketUrl(id, `/${encodeURIComponent(callID)}/media`)

const requestCallApi = async <T>(
  id: string,
  path: string,
  init: RequestInit = {},
): Promise<CallApiResult<T>> => {
  const headers = new Headers(init.headers)
  const token = getStoredToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  let data: unknown
  try {
    response = await fetch(buildCallHttpUrl(id, path), {
      ...init,
      headers,
      mode: 'cors',
    })
    const contentType = response.headers.get('Content-Type') ?? ''
    data = contentType.includes('application/json') ? await response.json() : undefined
  } catch (err) {
    handleError(err, 'Network error occurred')
    throw err instanceof Error ? err : new Error('Request failed')
  }

  if (!response.ok) {
    if (response.status === 401) {
      clearStoredToken()
    }
    throw buildCallApiError(response, data)
  }

  const dataRef = shallowRef<T>()
  dataRef.value = data as T
  return { data: dataRef }
}

export const useCallApi = () => {
  const listCalls = (id: string) => {
    return requestCallApi<CallRecord[]>(id, '')
  }

  const dialCall = (id: string, payload: DialCallRequest) => {
    return requestCallApi<CallRecord>(id, '', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  const updateCall = (id: string, callID: string, payload: UpdateCallRequest) => {
    return requestCallApi<CallRecord>(id, `/${encodeURIComponent(callID)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  }

  const answerCall = (id: string, callID: string) => {
    return updateCall(id, callID, { state: 'active' })
  }

  const rejectCall = (id: string, callID: string) => {
    return updateCall(id, callID, { state: 'ended', reason: 'busy' })
  }

  const hangupCall = (id: string, callID: string) => {
    return requestCallApi<CallRecord>(id, `/${encodeURIComponent(callID)}`, { method: 'DELETE' })
  }

  return {
    listCalls,
    dialCall,
    updateCall,
    answerCall,
    rejectCall,
    hangupCall,
  }
}

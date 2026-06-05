import { shallowRef, type Ref } from 'vue'

import { clearStoredToken, getStoredToken } from '@/lib/authStorage'
import { resolveAPIURL, resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { handleError } from '@/lib/errorHandler'

import type { ApiErrorResponse } from '@/types/api'
import type {
  CallRecord,
  DialCallRequest,
  SendDTMFRequest,
  UpdateCallRequest,
  WebRTCICEServersPayload,
} from '@/types/call'

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

const buildCallHttpUrl = (id: string, path: string, query?: string) => {
  const apiUrl = new URL(resolveAPIURL(`modems/${id}/calls${path}`))
  const trimmed = query?.trim()
  if (trimmed) {
    apiUrl.searchParams.set('q', trimmed)
  }
  return apiUrl.toString()
}

const buildCallMediaHttpUrl = (path: string) => {
  return resolveAPIURL(`call-media${path}`)
}

const buildCallWebSocketUrl = (id: string, path: string) => {
  return resolveAPIWebSocketURL(`modems/${id}/calls${path}`, getStoredToken())
}

export const buildCallEventsUrl = (id: string) => buildCallWebSocketUrl(id, '/events')

export const buildWebRTCSessionUrl = (id: string, callID: string) =>
  buildCallWebSocketUrl(id, `/${encodeURIComponent(callID)}/webrtc-sessions`)

const requestCallApi = async <T>(
  id: string,
  path: string,
  init: RequestInit = {},
  query?: string,
): Promise<CallApiResult<T>> => {
  return requestApi<T>(buildCallHttpUrl(id, path, query), init)
}

const requestCallMediaApi = async <T>(
  path: string,
  init: RequestInit = {},
): Promise<CallApiResult<T>> => {
  return requestApi<T>(buildCallMediaHttpUrl(path), init)
}

const requestApi = async <T>(url: string, init: RequestInit = {}): Promise<CallApiResult<T>> => {
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
    response = await fetch(url, {
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
  const listCalls = (id: string, query?: string) => {
    return requestCallApi<CallRecord[]>(id, '', {}, query)
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
    return updateCall(id, callID, { state: 'ended' })
  }

  const holdCall = (id: string, callID: string) => {
    return updateCall(id, callID, { hold: 'local' })
  }

  const resumeCall = (id: string, callID: string) => {
    return updateCall(id, callID, { hold: 'none' })
  }

  const sendDTMF = (id: string, callID: string, payload: SendDTMFRequest) => {
    return requestCallApi<void>(id, `/${encodeURIComponent(callID)}/dtmf-events`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  const getWebRTCICEServers = () => {
    return requestCallMediaApi<WebRTCICEServersPayload>('/ice-servers')
  }

  const deleteCall = (id: string, callID: string) => {
    return requestCallApi<void>(id, `/${encodeURIComponent(callID)}`, { method: 'DELETE' })
  }

  return {
    listCalls,
    dialCall,
    updateCall,
    answerCall,
    rejectCall,
    hangupCall,
    holdCall,
    resumeCall,
    sendDTMF,
    getWebRTCICEServers,
    deleteCall,
  }
}

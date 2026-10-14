import { shallowRef, type Ref } from 'vue'

import { invalidateSession } from '@/lib/authSession'
import { resolveAPIURL, resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { handleError } from '@/lib/errorHandler'
import { callClientId } from '@/lib/callClient'
import {
  API_ACCEPT,
  createApiError,
  fetchWithBoundedErrors,
  isLocalAuthenticationError,
} from '@/lib/apiProblem'

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

const buildCallApiError = (response: Response, data: unknown) => {
  const apiError = createApiError(response, data)
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
  return resolveAPIWebSocketURL(`modems/${id}/calls${path}`)
}

export const buildCallEventsUrl = (id: string) => buildCallWebSocketUrl(id, '/events')

export const buildWebRTCSessionUrl = (id: string, callID: string) =>
  buildCallWebSocketUrl(id, `/${encodeURIComponent(callID)}/webrtc/sessions`)

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
  headers.set('Accept', API_ACCEPT)
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  let data: unknown
  try {
    response = await fetchWithBoundedErrors(url, {
      ...init,
      headers,
      mode: 'same-origin',
      credentials: 'same-origin',
    })
    const contentType = response.headers.get('Content-Type') ?? ''
    data = response.ok
      ? contentType.includes('application/json')
        ? await response.json()
        : undefined
      : await response.text()
  } catch (err) {
    handleError(err, 'Network error occurred')
    throw err instanceof Error ? err : new Error('Request failed')
  }

  if (!response.ok) {
    const error = buildCallApiError(response, data)
    if (isLocalAuthenticationError(error)) {
      invalidateSession()
    }
    throw error
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
      body: JSON.stringify({ ...payload, clientId: callClientId() }),
    })
  }

  const updateCall = (id: string, callID: string, payload: UpdateCallRequest) => {
    return requestCallApi<CallRecord>(id, `/${encodeURIComponent(callID)}`, {
      method: 'PATCH',
      body: JSON.stringify({ ...payload, clientId: callClientId() }),
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
      body: JSON.stringify({ ...payload, clientId: callClientId() }),
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

import type { LicensePairing, LicenseStatus } from '@/types/license'
import { createApiError } from '@/lib/errorHandler'

const rawBaseURL = import.meta.env.VITE_API_BASE_URL
const baseURL = rawBaseURL?.trim() ? rawBaseURL.replace(/\/+$/, '') : '/api/v1'
const telegramBotUsername = /^[A-Za-z][A-Za-z0-9_]{1,28}bot$/i

const responseError = async (response: Response) => {
  let data: unknown
  try {
    data = (await response.json()) as unknown
  } catch {
    data = undefined
  }
  const apiError = createApiError(response, data)
  return Object.assign(new Error(apiError.message), apiError)
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const decodePairing = async (response: Response): Promise<LicensePairing> => {
  const data = (await response.json()) as unknown
  if (!isRecord(data)) throw new Error('invalid license pairing response')
  const status = data.status
  if (
    typeof data.id !== 'string' ||
    !data.id ||
    typeof data.activationUrl !== 'string' ||
    (status !== 'pending' && status !== 'active' && status !== 'expired') ||
    typeof data.expiresAt !== 'string' ||
    Number.isNaN(Date.parse(data.expiresAt))
  ) {
    throw new Error('invalid license pairing response')
  }
  let activationURL: URL
  try {
    activationURL = new URL(data.activationUrl)
  } catch {
    throw new Error('invalid Telegram activation URL')
  }
  if (activationURL.protocol !== 'https:' || activationURL.hostname !== 't.me') {
    throw new Error('invalid Telegram activation URL')
  }
  const path = activationURL.pathname.split('/').filter(Boolean)
  const starts = activationURL.searchParams.getAll('start')
  const queryKeys = [...activationURL.searchParams.keys()]
  if (
    activationURL.username ||
    activationURL.password ||
    activationURL.port ||
    activationURL.hash ||
    path.length !== 1 ||
    !telegramBotUsername.test(path[0] ?? '') ||
    queryKeys.length !== 1 ||
    queryKeys[0] !== 'start' ||
    starts.length !== 1 ||
    starts[0] !== data.id
  ) {
    throw new Error('invalid Telegram activation URL')
  }
  return {
    id: data.id,
    activationUrl: activationURL.toString(),
    status,
    expiresAt: data.expiresAt,
  }
}

const decodeStatus = async (response: Response): Promise<LicenseStatus> => {
  const data = (await response.json()) as unknown
  if (
    !isRecord(data) ||
    typeof data.authorized !== 'boolean' ||
    typeof data.deviceId !== 'string' ||
    !/^[0-9a-f]{32}$/.test(data.deviceId)
  ) {
    throw new Error('invalid license status response')
  }
  return { authorized: data.authorized, deviceId: data.deviceId }
}

export const useLicenseApi = () => ({
  async status(): Promise<Response> {
    return fetch(`${baseURL}/license`, {
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    })
  },
  async createPairing(): Promise<LicensePairing> {
    const response = await fetch(`${baseURL}/license/pairings`, {
      method: 'POST',
      headers: { Accept: 'application/json' },
    })
    if (!response.ok) throw await responseError(response)
    return decodePairing(response)
  },
  async pairing(id: string): Promise<LicensePairing> {
    const response = await fetch(`${baseURL}/license/pairings/${encodeURIComponent(id)}`, {
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    })
    if (!response.ok) throw await responseError(response)
    return decodePairing(response)
  },
  async decodeStatus(response: Response): Promise<LicenseStatus> {
    return decodeStatus(response)
  },
  async decodeError(response: Response) {
    return responseError(response)
  },
})

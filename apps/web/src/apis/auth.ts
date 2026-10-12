import { fetchJson } from '@/lib/fetch'
import type {
  AuthOtpRequirementResponse,
  AuthVerifyPayload,
  AuthVerifyResponse,
} from '@/types/auth'

export const useAuthApi = () => {
  const sendCode = () => {
    return fetchJson<void>('auth/otp', {
      method: 'POST',
    })
  }

  const getOtpRequirement = () => {
    return fetchJson<AuthOtpRequirementResponse>('auth/otp/required')
  }

  const verifyCode = (payload: AuthVerifyPayload) => {
    return fetchJson<AuthVerifyResponse>('auth/otp/verify', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  return {
    getOtpRequirement,
    sendCode,
    verifyCode,
  }
}

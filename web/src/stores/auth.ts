import { defineStore } from 'pinia'

import { useAuthApi } from '@/apis/auth'
import { clearStoredToken, getStoredToken, setStoredToken } from '@/lib/authStorage'

const RESEND_COOLDOWN_MS = 60_000
const CODE_LENGTH = 6

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: getStoredToken(),
    isSending: false,
    isVerifying: false,
    resendAvailableAt: 0,
    otpRequired: true,
  }),
  getters: {
    isAuthenticated: (state) => Boolean(state.token),
  },
  actions: {
    setToken(token: string) {
      this.token = token
      setStoredToken(token)
    },
    clearToken() {
      this.token = null
      clearStoredToken()
    },
    async fetchOtpRequirement() {
      try {
        const { data } = await useAuthApi().getOtpRequirement()
        const required = data.value?.otpRequired
        this.otpRequired = required ?? true
        return this.otpRequired
      } catch {
        this.otpRequired = true
        return this.otpRequired
      }
    },
    async sendCode() {
      if (this.isSending) return
      if (!this.otpRequired) return
      if (this.resendAvailableAt > Date.now()) return

      this.isSending = true
      try {
        await useAuthApi().sendCode()
        this.resendAvailableAt = Date.now() + RESEND_COOLDOWN_MS
      } catch {
        return
      } finally {
        this.isSending = false
      }
    },
    async verifyCode(code: string) {
      if (this.isVerifying) return null
      if (code.trim().length !== CODE_LENGTH) return null

      this.isVerifying = true
      try {
        const { data } = await useAuthApi().verifyCode({ code })
        const token = data.value?.token
        if (!token) return null

        this.setToken(token)
        return token
      } catch {
        return null
      } finally {
        this.isVerifying = false
      }
    },
  },
})

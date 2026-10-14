import { defineStore } from 'pinia'

import { useAuthApi } from '@/apis/auth'
import { invalidateSession } from '@/lib/authSession'
import type { AuthMethods } from '@/types/auth'

/** useAuthStore tracks authentication state, never cookie or provider credentials. */
export const useAuthStore = defineStore('auth', {
  state: () => ({
    configuration: null as AuthMethods | null,
    authenticated: false,
    isSending: false,
    isVerifying: false,
    isOpeningBrowser: false,
    resendAvailableAt: 0,
    challengeId: '',
    failed: false,
  }),
  getters: {
    hasOTP: (state) => state.configuration?.methods.includes('otp') === true,
    hasOIDC: (state) => state.configuration?.methods.includes('oidc') === true,
    isAuthenticated: (state) => state.authenticated,
  },
  actions: {
    async check(): Promise<boolean> {
      try {
        // Discard the retired browser bearer credential during the one-way migration.
        window.localStorage.removeItem('sigmo:token')
        const configuration = await useAuthApi().methods()
        if (
          configuration.version !== 1 ||
          !['none', 'otp', 'oidc', 'cloudflare_access'].includes(configuration.mode)
        )
          throw new Error('Unsupported authentication protocol')
        this.configuration = configuration
        this.authenticated = configuration.mode === 'none' || (await useAuthApi().session())
        this.failed = false
      } catch {
        this.configuration = null
        this.authenticated = false
        this.failed = true
      }
      return this.authenticated
    },
    async sendCode(): Promise<void> {
      if (!this.hasOTP || this.isSending || this.resendAvailableAt > Date.now()) return
      this.isSending = true
      try {
        const challenge = await useAuthApi().challenge()
        this.challengeId = challenge.id
        this.resendAvailableAt = Date.now() + 60_000
        this.failed = false
      } catch {
        this.failed = true
      } finally {
        this.isSending = false
      }
    },
    async verifyCode(code: string): Promise<boolean> {
      if (this.isVerifying || !this.challengeId || !/^\d{6}$/.test(code)) return false
      this.isVerifying = true
      try {
        await useAuthApi().verify(this.challengeId, code)
        invalidateSession()
        this.authenticated = true
        this.failed = false
        return true
      } catch {
        this.failed = true
        return false
      } finally {
        this.isVerifying = false
      }
    },
    async browserLogin(): Promise<void> {
      if (this.isOpeningBrowser) return
      this.isOpeningBrowser = true
      try {
        window.location.assign(await useAuthApi().browser())
      } catch {
        this.failed = true
      } finally {
        this.isOpeningBrowser = false
      }
    },
    async logout(): Promise<void> {
      if (this.configuration?.mode === 'cloudflare_access') {
        invalidateSession()
        this.authenticated = false
        window.location.assign('/cdn-cgi/access/logout')
        return
      }
      await useAuthApi().logout()
      invalidateSession()
      this.authenticated = false
      this.challengeId = ''
    },
  },
})

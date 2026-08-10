import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { useLicenseApi } from '@/apis/license'

type LicenseMode = 'unknown' | 'community' | 'authorized' | 'activation' | 'unavailable'

export const useLicenseStore = defineStore('license', () => {
  const mode = ref<LicenseMode>('unknown')
  const deviceId = ref('')
  const errorCode = ref('')
  const errorMessage = ref('')
  const checking = ref(false)
  let currentCheck: Promise<boolean> | undefined

  const businessEnabled = computed(
    () => mode.value === 'community' || mode.value === 'authorized',
  )
  const activationRequired = computed(() => mode.value === 'activation')
  const unavailable = computed(() => mode.value === 'unavailable')

  const clearError = () => {
    errorCode.value = ''
    errorMessage.value = ''
  }

  const performCheck = async (): Promise<boolean> => {
    try {
      const api = useLicenseApi()
      const response = await api.status()
      if (response.status === 404) {
        mode.value = 'community'
        deviceId.value = ''
        clearError()
        return true
      }
      if (!response.ok) {
        const error = await api.decodeError(response)
        mode.value = 'unavailable'
        errorCode.value = error.error_code
        errorMessage.value = error.message
        return false
      }
      const status = await api.decodeStatus(response)
      deviceId.value = status.deviceId
      clearError()
      mode.value = status.authorized ? 'authorized' : 'activation'
      return status.authorized
    } catch (error) {
      mode.value = 'unavailable'
      errorCode.value =
        error && typeof error === 'object' && 'error_code' in error
          ? String(error.error_code)
          : 'license_status_unavailable'
      errorMessage.value =
        error instanceof Error ? error.message : 'license status is unavailable'
      return false
    }
  }

  const check = (force = false): Promise<boolean> => {
    if (!force && mode.value !== 'unknown') return Promise.resolve(businessEnabled.value)
    if (currentCheck) return currentCheck

    checking.value = true
    currentCheck = performCheck().finally(() => {
      checking.value = false
      currentCheck = undefined
    })
    return currentCheck
  }

  return {
    mode,
    deviceId,
    errorCode,
    errorMessage,
    checking,
    businessEnabled,
    activationRequired,
    unavailable,
    check,
  }
})

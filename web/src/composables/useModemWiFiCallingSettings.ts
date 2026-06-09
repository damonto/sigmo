import { computed, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { WiFiCallingSettingsResponse } from '@/types/modem'
import type { CarrierWebsheetInfo } from '@/types/websheet'

type Options = {
  modemId: ComputedRef<string>
  enabled: ComputedRef<boolean>
  onSuccess?: (message: string) => void
}

type FetchSettingsOptions = {
  silent?: boolean
}

const pollIntervalMs = 5000

export const useModemWiFiCallingSettings = ({ modemId, enabled, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const settingsWiFiCallingEnabled = ref(false)
  const settingsWiFiCallingPreferred = ref(false)
  const settingsWiFiCallingConnected = ref(false)
  const settingsWiFiCallingState = ref('')
  const settingsWiFiCallingDurationSeconds = ref(0)
  const settingsWiFiCallingEmergencyAddressUpdateAvailable = ref(false)
  const settingsWiFiCallingWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const settingsWiFiCallingEmergencyAddressWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const isWiFiCallingSettingsLoading = ref(false)
  const isWiFiCallingSettingsUpdating = ref(false)
  const isWiFiCallingReconnecting = ref(false)
  const isWiFiCallingWebsheetStarting = ref(false)
  const isWiFiCallingEmergencyAddressWebsheetStarting = ref(false)
  const isWiFiCallingSettingsFetching = ref(false)
  const pollTimer = ref<number>()
  const durationTimer = ref<number>()

  const isWiFiCallingConnected = computed(() => settingsWiFiCallingConnected.value)

  const stopPolling = () => {
    if (pollTimer.value === undefined) return
    window.clearInterval(pollTimer.value)
    pollTimer.value = undefined
  }

  const startPolling = () => {
    if (pollTimer.value !== undefined) return
    pollTimer.value = window.setInterval(() => {
      const targetId = modemId.value
      if (!targetId || !enabled.value) return
      void fetchSettings(targetId, { silent: true })
    }, pollIntervalMs)
  }

  const stopDurationTimer = () => {
    if (durationTimer.value === undefined) return
    window.clearInterval(durationTimer.value)
    durationTimer.value = undefined
  }

  const startDurationTimer = () => {
    if (durationTimer.value !== undefined) return
    durationTimer.value = window.setInterval(() => {
      if (!settingsWiFiCallingConnected.value) return
      settingsWiFiCallingDurationSeconds.value += 1
    }, 1000)
  }

  const resetSettings = () => {
    stopPolling()
    stopDurationTimer()
    settingsWiFiCallingEnabled.value = false
    settingsWiFiCallingPreferred.value = false
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = ''
    settingsWiFiCallingDurationSeconds.value = 0
    settingsWiFiCallingEmergencyAddressUpdateAvailable.value = false
    settingsWiFiCallingWebsheet.value = null
    settingsWiFiCallingEmergencyAddressWebsheet.value = null
  }

  const fetchSettings = async (id: string, options?: FetchSettingsOptions) => {
    if (!enabled.value || isWiFiCallingSettingsFetching.value) return
    isWiFiCallingSettingsFetching.value = true
    if (!options?.silent) {
      isWiFiCallingSettingsLoading.value = true
    }
    try {
      const { data } = await modemApi.getWiFiCallingSettings(id)
      const payload: WiFiCallingSettingsResponse | undefined = data.value
      settingsWiFiCallingEnabled.value = payload?.enabled ?? false
      settingsWiFiCallingPreferred.value = payload?.preferred ?? false
      settingsWiFiCallingConnected.value = data.value?.connected ?? false
      settingsWiFiCallingState.value = data.value?.state ?? ''
      settingsWiFiCallingDurationSeconds.value = payload?.durationSeconds ?? 0
      settingsWiFiCallingEmergencyAddressUpdateAvailable.value =
        payload?.emergencyAddressUpdateAvailable ?? false
    } finally {
      if (!options?.silent) {
        isWiFiCallingSettingsLoading.value = false
      }
      isWiFiCallingSettingsFetching.value = false
    }
  }

  const handleWiFiCallingUpdate = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingSettingsUpdating.value) return
    isWiFiCallingSettingsUpdating.value = true
    try {
      await modemApi.updateWiFiCallingSettings(targetId, {
        enabled: settingsWiFiCallingEnabled.value,
        preferred: settingsWiFiCallingEnabled.value && settingsWiFiCallingPreferred.value,
      })
      await fetchSettings(targetId)
      onSuccess?.(t('modemDetail.settings.wifiCallingSuccess'))
    } catch (err) {
      console.error('[useModemWiFiCallingSettings] Failed to update settings:', err)
    } finally {
      isWiFiCallingSettingsUpdating.value = false
    }
  }

  const reconnectWiFiCalling = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingReconnecting.value) return
    isWiFiCallingReconnecting.value = true
    const previousConnected = settingsWiFiCallingConnected.value
    const previousState = settingsWiFiCallingState.value
    const previousDurationSeconds = settingsWiFiCallingDurationSeconds.value
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = 'connecting'
    settingsWiFiCallingDurationSeconds.value = 0
    let reconnectStarted = false
    try {
      await modemApi.createWiFiCallingSession(targetId)
      reconnectStarted = true
      await fetchSettings(targetId)
    } catch (err) {
      if (!reconnectStarted) {
        settingsWiFiCallingConnected.value = previousConnected
        settingsWiFiCallingState.value = previousState
        settingsWiFiCallingDurationSeconds.value = previousDurationSeconds
      }
      console.error('[useModemWiFiCallingSettings] Failed to reconnect Wi-Fi Calling:', err)
    } finally {
      isWiFiCallingReconnecting.value = false
    }
  }

  const startWiFiCallingWebsheet = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingWebsheetStarting.value) return
    isWiFiCallingWebsheetStarting.value = true
    try {
      const { data } = await modemApi.startWiFiCallingWebsheet(targetId)
      settingsWiFiCallingWebsheet.value = data.value ?? null
    } finally {
      isWiFiCallingWebsheetStarting.value = false
    }
  }

  const startWiFiCallingEmergencyAddressWebsheet = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingEmergencyAddressWebsheetStarting.value) return
    isWiFiCallingEmergencyAddressWebsheetStarting.value = true
    try {
      const { data } = await modemApi.startWiFiCallingEmergencyAddressWebsheet(targetId)
      settingsWiFiCallingEmergencyAddressWebsheet.value = data.value ?? null
    } finally {
      isWiFiCallingEmergencyAddressWebsheetStarting.value = false
    }
  }

  const completeWiFiCallingWebsheet = async () => {
    const targetId = modemId.value
    settingsWiFiCallingWebsheet.value = null
    if (!targetId) return
    await fetchSettings(targetId)
  }

  const completeWiFiCallingEmergencyAddressWebsheet = async () => {
    const targetId = modemId.value
    settingsWiFiCallingEmergencyAddressWebsheet.value = null
    if (!targetId) return
    await fetchSettings(targetId)
  }

  watch(
    [modemId, enabled],
    async ([id, canUseWiFiCalling]) => {
      if (!canUseWiFiCalling || !id) {
        resetSettings()
        return
      }
      await fetchSettings(id)
      startPolling()
    },
    { immediate: true },
  )

  watch(
    isWiFiCallingConnected,
    (connected) => {
      if (connected) {
        startDurationTimer()
        return
      }
      stopDurationTimer()
    },
    { immediate: true },
  )

  onUnmounted(() => {
    stopPolling()
    stopDurationTimer()
  })

  return {
    settingsWiFiCallingEnabled,
    settingsWiFiCallingPreferred,
    settingsWiFiCallingConnected,
    settingsWiFiCallingState,
    settingsWiFiCallingDurationSeconds,
    settingsWiFiCallingEmergencyAddressUpdateAvailable,
    settingsWiFiCallingWebsheet,
    settingsWiFiCallingEmergencyAddressWebsheet,
    isWiFiCallingSettingsLoading,
    isWiFiCallingSettingsUpdating,
    isWiFiCallingReconnecting,
    isWiFiCallingWebsheetStarting,
    isWiFiCallingEmergencyAddressWebsheetStarting,
    handleWiFiCallingUpdate,
    reconnectWiFiCalling,
    startWiFiCallingWebsheet,
    startWiFiCallingEmergencyAddressWebsheet,
    completeWiFiCallingWebsheet,
    completeWiFiCallingEmergencyAddressWebsheet,
  }
}

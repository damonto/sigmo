import { computed, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { WiFiCallingSettings, WiFiCallingSettingsResponse } from '@/types/modem'
import type { CarrierWebsheetInfo } from '@/types/websheet'

type Options = {
  modemId: ComputedRef<string>
  enabled: ComputedRef<boolean>
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

type FetchSettingsOptions = {
  silent?: boolean
  applySettings?: boolean
}

const pollIntervalMs = 5000

export const useModemWiFiCallingSettings = ({ modemId, enabled, onSuccess, onError }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const settingsWiFiCallingEnabled = ref(false)
  const confirmedWiFiCallingSettings = ref<WiFiCallingSettings>({
    enabled: false,
  })
  const settingsWiFiCallingConnected = ref(false)
  const settingsWiFiCallingState = ref('')
  const settingsWiFiCallingDurationSeconds = ref(0)
  const settingsWiFiCallingEmergencyAddressUpdateAvailable = ref(false)
  const settingsWiFiCallingWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const settingsWiFiCallingEmergencyAddressWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const isWiFiCallingSettingsLoading = ref(false)
  const isWiFiCallingSettingsUpdating = ref(false)
  const settingsRequestEpoch = ref(0)
  const isWiFiCallingReconnecting = ref(false)
  const isWiFiCallingDisconnecting = ref(false)
  const isWiFiCallingWebsheetStarting = ref(false)
  const isWiFiCallingEmergencyAddressWebsheetStarting = ref(false)
  const isWiFiCallingSettingsFetching = ref(false)
  const pollTimer = ref<number>()
  const durationTimer = ref<number>()

  const isWiFiCallingConnected = computed(() => settingsWiFiCallingConnected.value)

  const advanceSettingsRequestEpoch = () => {
    settingsRequestEpoch.value += 1
    return settingsRequestEpoch.value
  }

  const stopPolling = () => {
    if (pollTimer.value === undefined) return
    window.clearInterval(pollTimer.value)
    pollTimer.value = undefined
  }

  const startPolling = () => {
    if (pollTimer.value !== undefined) return
    pollTimer.value = window.setInterval(() => {
      const targetId = modemId.value
      if (!targetId || !enabled.value || isWiFiCallingSettingsUpdating.value) return
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
    confirmedWiFiCallingSettings.value = { enabled: false }
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = ''
    settingsWiFiCallingDurationSeconds.value = 0
    settingsWiFiCallingEmergencyAddressUpdateAvailable.value = false
    settingsWiFiCallingWebsheet.value = null
    settingsWiFiCallingEmergencyAddressWebsheet.value = null
  }

  const fetchSettings = async (id: string, options?: FetchSettingsOptions) => {
    if (!enabled.value || isWiFiCallingSettingsFetching.value) return
    const requestEpoch = settingsRequestEpoch.value
    const preserveSettings = isWiFiCallingSettingsUpdating.value
    isWiFiCallingSettingsFetching.value = true
    if (!options?.silent) {
      isWiFiCallingSettingsLoading.value = true
    }
    try {
      const { data } = await modemApi.getWiFiCallingSettings(id)
      if (!enabled.value || modemId.value !== id) return
      if (requestEpoch !== settingsRequestEpoch.value) return
      const payload: WiFiCallingSettingsResponse | undefined = data.value
      if (!preserveSettings || options?.applySettings) {
        const settings = {
          enabled: payload?.enabled ?? false,
        }
        settingsWiFiCallingEnabled.value = settings.enabled
        confirmedWiFiCallingSettings.value = settings
      }
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

  const updateWiFiCallingSettings = async (next: WiFiCallingSettings) => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingSettingsUpdating.value) return
    const mutationEpoch = advanceSettingsRequestEpoch()
    isWiFiCallingSettingsUpdating.value = true
    const previous = confirmedWiFiCallingSettings.value
    const settings = {
      enabled: next.enabled,
    }
    settingsWiFiCallingEnabled.value = settings.enabled
    try {
      await modemApi.updateWiFiCallingSettings(targetId, settings)
      if (modemId.value !== targetId || settingsRequestEpoch.value !== mutationEpoch) return
      confirmedWiFiCallingSettings.value = settings
      await fetchSettings(targetId, { applySettings: true })
      if (modemId.value !== targetId || settingsRequestEpoch.value !== mutationEpoch) return
      onSuccess?.(t('modemDetail.settings.wifiCallingSuccess'))
    } catch (err) {
      if (modemId.value !== targetId || settingsRequestEpoch.value !== mutationEpoch) return
      settingsWiFiCallingEnabled.value = previous.enabled
      console.error('[useModemWiFiCallingSettings] Failed to update settings:', err)
      onError?.(t('modemDetail.settings.wifiCallingUpdateFailed'))
    } finally {
      if (settingsRequestEpoch.value === mutationEpoch) {
        isWiFiCallingSettingsUpdating.value = false
      }
    }
  }

  const handleWiFiCallingUpdate = () =>
    updateWiFiCallingSettings({
      enabled: settingsWiFiCallingEnabled.value,
    })

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

  const disconnectWiFiCalling = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingDisconnecting.value) return
    isWiFiCallingDisconnecting.value = true
    const previousConnected = settingsWiFiCallingConnected.value
    const previousState = settingsWiFiCallingState.value
    const previousDurationSeconds = settingsWiFiCallingDurationSeconds.value
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = 'disconnected'
    settingsWiFiCallingDurationSeconds.value = 0
    try {
      await modemApi.deleteWiFiCallingSession(targetId)
      await fetchSettings(targetId)
    } catch (err) {
      settingsWiFiCallingConnected.value = previousConnected
      settingsWiFiCallingState.value = previousState
      settingsWiFiCallingDurationSeconds.value = previousDurationSeconds
      console.error('[useModemWiFiCallingSettings] Failed to disconnect Wi-Fi Calling:', err)
    } finally {
      isWiFiCallingDisconnecting.value = false
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
      advanceSettingsRequestEpoch()
      isWiFiCallingSettingsUpdating.value = false
      resetSettings()
      if (!canUseWiFiCalling || !id) {
        return
      }
      await fetchSettings(id)
      if (!enabled.value || modemId.value !== id) return
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
    settingsWiFiCallingConnected,
    settingsWiFiCallingState,
    settingsWiFiCallingDurationSeconds,
    settingsWiFiCallingEmergencyAddressUpdateAvailable,
    settingsWiFiCallingWebsheet,
    settingsWiFiCallingEmergencyAddressWebsheet,
    isWiFiCallingSettingsLoading,
    isWiFiCallingSettingsUpdating,
    isWiFiCallingReconnecting,
    isWiFiCallingDisconnecting,
    isWiFiCallingWebsheetStarting,
    isWiFiCallingEmergencyAddressWebsheetStarting,
    updateWiFiCallingSettings,
    handleWiFiCallingUpdate,
    reconnectWiFiCalling,
    disconnectWiFiCalling,
    startWiFiCallingWebsheet,
    startWiFiCallingEmergencyAddressWebsheet,
    completeWiFiCallingWebsheet,
    completeWiFiCallingEmergencyAddressWebsheet,
  }
}

import { computed, nextTick, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ConnectionProgress } from '@/types/connectionProgress'

import { useModemApi } from '@/apis/modem'
import type {
  WiFiCallingSettings,
  WiFiCallingSettingsRequest,
  WiFiCallingSettingsResponse,
  WiFiCallingUnderlay,
} from '@/types/modem'
import type { CarrierWebsheetInfo } from '@/types/websheet'

type Options = {
  modemId: ComputedRef<string>
  simId?: ComputedRef<string | undefined>
  enabled: ComputedRef<boolean>
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

type FetchSettingsOptions = {
  silent?: boolean
  reconcile?: boolean
}

const pollIntervalMs = 5000
const systemUnderlay = (): WiFiCallingUnderlay => ({ mode: 'system' })

export const useModemWiFiCallingSettings = ({
  modemId,
  simId,
  enabled,
  onSuccess,
  onError,
}: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const confirmedSettings = ref<WiFiCallingSettingsRequest>()
  const pendingSettings = ref<WiFiCallingSettingsRequest>()
  const currentSettings = computed(() => pendingSettings.value ?? confirmedSettings.value)
  const hasWiFiCallingSettings = computed(() => confirmedSettings.value !== undefined)
  const savedWiFiCallingProxyId = computed(() => confirmedSettings.value?.underlay.proxyId)
  const settingsWiFiCallingEnabled = computed(() => currentSettings.value?.enabled ?? false)
  const settingsWiFiCallingUnderlay = computed(
    () => currentSettings.value?.underlay ?? systemUnderlay(),
  )
  const settingsWiFiCallingConnected = ref(false)
  const settingsWiFiCallingState = ref('')
  const settingsWiFiCallingProgress = ref<ConnectionProgress>()
  const settingsWiFiCallingLastError = ref('')
  const settingsWiFiCallingDurationSeconds = ref(0)
  const settingsWiFiCallingEmergencyAddressUpdateAvailable = ref(false)
  const settingsWiFiCallingWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const settingsWiFiCallingEmergencyAddressWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const isWiFiCallingSettingsLoading = ref(false)
  const isWiFiCallingSettingsUpdating = ref(false)
  let settingsRequestEpoch = 0
  let settingsRead: { epoch: number; result: Promise<boolean> } | undefined
  const isWiFiCallingReconnecting = ref(false)
  const isWiFiCallingDisconnecting = ref(false)
  const isWiFiCallingWebsheetStarting = ref(false)
  const isWiFiCallingEmergencyAddressWebsheetStarting = ref(false)
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
    confirmedSettings.value = undefined
    pendingSettings.value = undefined
    settingsRead = undefined
    isWiFiCallingSettingsLoading.value = false
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = ''
    settingsWiFiCallingProgress.value = undefined
    settingsWiFiCallingLastError.value = ''
    settingsWiFiCallingDurationSeconds.value = 0
    settingsWiFiCallingEmergencyAddressUpdateAvailable.value = false
    settingsWiFiCallingWebsheet.value = null
    settingsWiFiCallingEmergencyAddressWebsheet.value = null
  }

  const fetchSettings = (id: string, options?: FetchSettingsOptions): Promise<boolean> => {
    if (!enabled.value || !id) return Promise.resolve(false)
    const epoch = settingsRequestEpoch
    if (settingsRead?.epoch === epoch && !options?.reconcile) return settingsRead.result

    const applySettings = !isWiFiCallingSettingsUpdating.value || options?.reconcile
    if (!options?.silent) isWiFiCallingSettingsLoading.value = true

    const result = modemApi
      .getWiFiCallingSettings(id, { quiet: options?.silent })
      .then(({ data }) => {
        if (
          settingsRead?.result !== result ||
          epoch !== settingsRequestEpoch ||
          modemId.value !== id ||
          !enabled.value
        ) {
          return false
        }
        const payload: WiFiCallingSettingsResponse | undefined = data.value
        if (!payload) return false
        if (applySettings) {
          confirmedSettings.value = {
            enabled: payload.enabled,
            underlay: payload.underlay ?? systemUnderlay(),
          }
        }
        settingsWiFiCallingConnected.value = payload.connected ?? false
        settingsWiFiCallingState.value = payload.state ?? ''
        settingsWiFiCallingProgress.value = payload.progress
        settingsWiFiCallingLastError.value = payload.lastError ?? ''
        settingsWiFiCallingDurationSeconds.value = payload.durationSeconds ?? 0
        settingsWiFiCallingEmergencyAddressUpdateAvailable.value =
          payload.emergencyAddressUpdateAvailable ?? false
        return true
      })
      .catch(() => false)
      .finally(() => {
        if (settingsRead?.result === result) {
          settingsRead = undefined
          isWiFiCallingSettingsLoading.value = false
        }
      })

    settingsRead = { epoch, result }
    return result
  }

  const updateWiFiCallingSettings = async (next: WiFiCallingSettings) => {
    const targetId = modemId.value
    if (!enabled.value || !targetId || isWiFiCallingSettingsUpdating.value) return false
    const epoch = ++settingsRequestEpoch
    const isCurrent = () =>
      enabled.value && modemId.value === targetId && settingsRequestEpoch === epoch
    const settings = { enabled: next.enabled, underlay: next.underlay ?? systemUnderlay() }
    isWiFiCallingSettingsUpdating.value = true
    pendingSettings.value = settings
    try {
      await modemApi.updateWiFiCallingSettings(targetId, settings)
      if (!isCurrent()) return false
      confirmedSettings.value = settings
      pendingSettings.value = undefined
      // The saved state is authoritative even if the status refresh needs a retry.
      await fetchSettings(targetId, { silent: true, reconcile: true })
      if (!isCurrent()) return false
      onSuccess?.(t('modemDetail.settings.wifiCallingSuccess'))
      return true
    } catch (err) {
      if (!isCurrent()) return false
      console.error('[useModemWiFiCallingSettings] Failed to update settings:', err)
      onError?.(t('modemDetail.settings.wifiCallingUpdateFailed'))
      return false
    } finally {
      if (isCurrent()) {
        pendingSettings.value = undefined
        isWiFiCallingSettingsUpdating.value = false
      }
    }
  }

  const ensureWiFiCallingEnabled = async () => {
    const id = modemId.value
    const activeSIM = simId?.value
    // Opening the shortcut can also start loading settings for this SIM.
    await nextTick()
    if (!enabled.value || id !== modemId.value || activeSIM !== simId?.value) return false
    if (!confirmedSettings.value && !(await fetchSettings(id))) return false
    const settings = confirmedSettings.value
    return settings
      ? settings.enabled || (await updateWiFiCallingSettings({ ...settings, enabled: true }))
      : false
  }

  const reconnectWiFiCalling = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId) return
    if (isWiFiCallingReconnecting.value) return
    isWiFiCallingReconnecting.value = true
    const epoch = settingsRequestEpoch
    const previousConnected = settingsWiFiCallingConnected.value
    const previousState = settingsWiFiCallingState.value
    const previousProgress = settingsWiFiCallingProgress.value
    const previousDurationSeconds = settingsWiFiCallingDurationSeconds.value
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingProgress.value = undefined
    settingsWiFiCallingState.value = 'connecting'
    settingsWiFiCallingDurationSeconds.value = 0
    let reconnectStarted = false
    try {
      await modemApi.createWiFiCallingSession(targetId)
      reconnectStarted = true
      if (epoch === settingsRequestEpoch) await fetchSettings(targetId)
    } catch (err) {
      if (!reconnectStarted && epoch === settingsRequestEpoch) {
        settingsWiFiCallingConnected.value = previousConnected
        settingsWiFiCallingState.value = previousState
        settingsWiFiCallingProgress.value = previousProgress
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
    const epoch = settingsRequestEpoch
    const previousConnected = settingsWiFiCallingConnected.value
    const previousState = settingsWiFiCallingState.value
    const previousProgress = settingsWiFiCallingProgress.value
    const previousDurationSeconds = settingsWiFiCallingDurationSeconds.value
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingProgress.value = undefined
    settingsWiFiCallingState.value = 'disconnected'
    settingsWiFiCallingDurationSeconds.value = 0
    try {
      await modemApi.deleteWiFiCallingSession(targetId)
      if (epoch === settingsRequestEpoch) await fetchSettings(targetId)
    } catch (err) {
      if (epoch === settingsRequestEpoch) {
        settingsWiFiCallingConnected.value = previousConnected
        settingsWiFiCallingState.value = previousState
        settingsWiFiCallingProgress.value = previousProgress
        settingsWiFiCallingDurationSeconds.value = previousDurationSeconds
      }
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
    [modemId, enabled, () => simId?.value],
    ([id, canUseWiFiCalling]) => {
      settingsRequestEpoch += 1
      isWiFiCallingSettingsUpdating.value = false
      resetSettings()
      if (!canUseWiFiCalling || !id) {
        return
      }
      void fetchSettings(id)
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
    settingsRequestEpoch += 1
    settingsRead = undefined
    stopPolling()
    stopDurationTimer()
  })

  return {
    hasWiFiCallingSettings,
    savedWiFiCallingProxyId,
    settingsWiFiCallingEnabled,
    settingsWiFiCallingUnderlay,
    settingsWiFiCallingConnected,
    settingsWiFiCallingState,
    settingsWiFiCallingProgress,
    settingsWiFiCallingLastError,
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
    ensureWiFiCallingEnabled,
    reconnectWiFiCalling,
    disconnectWiFiCalling,
    startWiFiCallingWebsheet,
    startWiFiCallingEmergencyAddressWebsheet,
    completeWiFiCallingWebsheet,
    completeWiFiCallingEmergencyAddressWebsheet,
  }
}

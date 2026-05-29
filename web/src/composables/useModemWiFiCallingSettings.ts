import { ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { WiFiCallingSettingsResponse } from '@/types/modem'
import type { CarrierWebsheetInfo } from '@/types/websheet'

type Options = {
  modemId: ComputedRef<string>
  enabled: ComputedRef<boolean>
  onSuccess?: (message: string) => void
}

export const useModemWiFiCallingSettings = ({ modemId, enabled, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const settingsWiFiCallingEnabled = ref(false)
  const settingsWiFiCallingPreferred = ref(false)
  const settingsWiFiCallingConnected = ref(false)
  const settingsWiFiCallingState = ref('')
  const settingsWiFiCallingWebsheet = ref<CarrierWebsheetInfo | null>(null)
  const isWiFiCallingSettingsLoading = ref(false)
  const isWiFiCallingSettingsUpdating = ref(false)
  const isWiFiCallingWebsheetStarting = ref(false)

  const resetSettings = () => {
    settingsWiFiCallingEnabled.value = false
    settingsWiFiCallingPreferred.value = false
    settingsWiFiCallingConnected.value = false
    settingsWiFiCallingState.value = ''
    settingsWiFiCallingWebsheet.value = null
  }

  const fetchSettings = async (id: string) => {
    if (!enabled.value || isWiFiCallingSettingsLoading.value) return
    isWiFiCallingSettingsLoading.value = true
    try {
      const { data } = await modemApi.getWiFiCallingSettings(id)
      const payload: WiFiCallingSettingsResponse | undefined = data.value
      settingsWiFiCallingEnabled.value = payload?.enabled ?? false
      settingsWiFiCallingPreferred.value = payload?.preferred ?? false
      settingsWiFiCallingConnected.value = data.value?.connected ?? false
      settingsWiFiCallingState.value = data.value?.state ?? ''
    } finally {
      isWiFiCallingSettingsLoading.value = false
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

  const completeWiFiCallingWebsheet = async () => {
    const targetId = modemId.value
    settingsWiFiCallingWebsheet.value = null
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
    },
    { immediate: true },
  )

  return {
    settingsWiFiCallingEnabled,
    settingsWiFiCallingPreferred,
    settingsWiFiCallingConnected,
    settingsWiFiCallingState,
    settingsWiFiCallingWebsheet,
    isWiFiCallingSettingsLoading,
    isWiFiCallingSettingsUpdating,
    isWiFiCallingWebsheetStarting,
    handleWiFiCallingUpdate,
    startWiFiCallingWebsheet,
    completeWiFiCallingWebsheet,
  }
}

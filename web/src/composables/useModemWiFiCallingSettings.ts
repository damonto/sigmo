import { ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { WiFiCallingSettings } from '@/types/modem'

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
  const isWiFiCallingSettingsLoading = ref(false)
  const isWiFiCallingSettingsUpdating = ref(false)

  const resetSettings = () => {
    settingsWiFiCallingEnabled.value = false
    settingsWiFiCallingPreferred.value = false
  }

  const fetchSettings = async (id: string) => {
    if (!enabled.value || isWiFiCallingSettingsLoading.value) return
    isWiFiCallingSettingsLoading.value = true
    try {
      const { data } = await modemApi.getWiFiCallingSettings(id)
      const payload: WiFiCallingSettings | undefined = data.value
      settingsWiFiCallingEnabled.value = payload?.enabled ?? false
      settingsWiFiCallingPreferred.value = payload?.preferred ?? false
    } finally {
      isWiFiCallingSettingsLoading.value = false
    }
  }

  const handleWiFiCallingUpdate = async () => {
    const targetId = modemId.value
    if (!enabled.value || !targetId || targetId === 'unknown') return
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

  watch(
    [modemId, enabled],
    async ([id, canUseWiFiCalling]) => {
      if (!canUseWiFiCalling || !id || id === 'unknown') {
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
    isWiFiCallingSettingsLoading,
    isWiFiCallingSettingsUpdating,
    handleWiFiCallingUpdate,
  }
}

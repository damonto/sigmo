import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { ModemSettings } from '@/types/modem'

type Options = {
  modemId: ComputedRef<string>
  onSuccess?: (message: string) => void
}

export const useModemDeviceSettings = ({ modemId, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const settingsAlias = ref('')
  const settingsMss = ref('')
  const settingsCompatible = ref(false)
  const isSettingsLoading = ref(false)
  const isSettingsUpdating = ref(false)

  const mssValue = computed(() => Number.parseInt(settingsMss.value, 10))
  const isMssValid = computed(() => {
    return Number.isInteger(mssValue.value) && mssValue.value >= 64 && mssValue.value <= 254
  })

  const resetSettings = () => {
    settingsAlias.value = ''
    settingsMss.value = ''
    settingsCompatible.value = false
  }

  const fetchSettings = async (id: string) => {
    if (isSettingsLoading.value) return
    isSettingsLoading.value = true
    try {
      const { data } = await modemApi.getSettings(id)
      const payload: ModemSettings | undefined = data.value
      settingsAlias.value = payload?.alias ?? ''
      settingsMss.value = payload?.mss ? String(payload.mss) : ''
      settingsCompatible.value = payload?.compatible ?? false
    } finally {
      isSettingsLoading.value = false
    }
  }

  const handleSettingsUpdate = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!isMssValid.value || isSettingsUpdating.value) return
    isSettingsUpdating.value = true
    try {
      const payload: ModemSettings = {
        alias: settingsAlias.value.trim(),
        compatible: settingsCompatible.value,
        mss: mssValue.value,
      }
      await modemApi.updateSettings(targetId, payload)
      await fetchSettings(targetId)
      onSuccess?.(t('modemDetail.settings.deviceSuccess'))
    } catch (err) {
      console.error('[useModemDeviceSettings] Failed to update settings:', err)
    } finally {
      isSettingsUpdating.value = false
    }
  }

  watch(
    modemId,
    async (id) => {
      if (!id || id === 'unknown') {
        resetSettings()
        return
      }
      await fetchSettings(id)
    },
    { immediate: true },
  )

  return {
    settingsAlias,
    settingsMss,
    settingsCompatible,
    isSettingsLoading,
    isSettingsUpdating,
    isMssValid,
    handleSettingsUpdate,
  }
}

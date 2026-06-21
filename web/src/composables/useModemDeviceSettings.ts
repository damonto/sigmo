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
  const isSettingsLoading = ref(false)
  const isSettingsUpdating = ref(false)

  const mssValue = computed(() => Number.parseInt(settingsMss.value, 10))
  const isMssValid = computed(() => {
    return Number.isInteger(mssValue.value) && mssValue.value >= 64 && mssValue.value <= 254
  })

  const resetSettings = () => {
    settingsAlias.value = ''
    settingsMss.value = ''
  }

  const fetchSettings = async (id: string) => {
    if (isSettingsLoading.value) return
    isSettingsLoading.value = true
    try {
      const { data } = await modemApi.getSettings(id)
      const payload: ModemSettings | undefined = data.value
      settingsAlias.value = payload?.alias ?? ''
      settingsMss.value = payload?.mss ? String(payload.mss) : ''
    } finally {
      isSettingsLoading.value = false
    }
  }

  const saveSettings = async (mss: number) => {
    const targetId = modemId.value
    if (!targetId) return
    if (isSettingsUpdating.value) return
    isSettingsUpdating.value = true
    try {
      const payload: ModemSettings = {
        alias: settingsAlias.value.trim(),
        mss,
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

  const handleSettingsUpdate = async () => {
    if (!isMssValid.value) return
    await saveSettings(mssValue.value)
  }

  watch(
    modemId,
    async (id) => {
      if (!id) {
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
    isSettingsLoading,
    isSettingsUpdating,
    isMssValid,
    handleSettingsUpdate,
  }
}

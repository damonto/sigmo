import { inject, provide } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import { useSettings } from '@/composables/useSettings'
import { useSettingsForm } from '@/composables/useSettingsForm'

const settingsContextKey = Symbol('settings-context')

const createSettingsContext = () => {
  const { t } = useI18n()
  const {
    settings,
    values,
    isLoading,
    isSaving,
    isSavingAuth,
    isSavingProxy,
    savingNotificationChannels,
    fetchSettings,
    saveAuth: saveAuthSettings,
    saveProxy: saveProxySettings,
    saveNotificationChannel,
  } = useSettings()
  const form = useSettingsForm(settings, values)

  const load = async () => {
    await fetchSettings()
    form.initializeExpandedChannels()
  }

  const saveAuth = async () => {
    const response = await saveAuthSettings()
    if (!response) return false

    toast.success(t('settings.saveSuccess'))
    return true
  }

  const saveProxy = async () => {
    const response = await saveProxySettings()
    if (!response) return false

    toast.success(t('settings.saveSuccess'))
    return true
  }

  const saveChannel = async (channel: string) => {
    const response = await saveNotificationChannel(channel)
    if (!response) return false

    toast.success(t('settings.notificationSaveSuccess'))
    return true
  }

  return {
    settings,
    values,
    isLoading,
    isSaving,
    isSavingAuth,
    isSavingProxy,
    savingNotificationChannels,
    ...form,
    load,
    saveAuth,
    saveProxy,
    saveChannel,
  }
}

export type SettingsContext = ReturnType<typeof createSettingsContext>

export const provideSettingsContext = () => {
  const context = createSettingsContext()
  provide(settingsContextKey, context)
  return context
}

export const useSettingsContext = () => {
  const context = inject<SettingsContext | null>(settingsContextKey, null)
  if (!context) throw new Error('settings context is unavailable')
  return context
}

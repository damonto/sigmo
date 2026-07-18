import { computed, ref, toRaw } from 'vue'

import { useSettingsApi } from '@/apis/settings'
import type { SettingsResponse, SettingsValues } from '@/types/settings'

const clone = <T>(value: T): T => {
  return structuredClone(toRaw(value))
}

export const useSettings = () => {
  const configApi = useSettingsApi()

  const settings = ref<SettingsResponse | null>(null)
  const values = ref<SettingsValues | null>(null)
  const isLoading = ref(false)
  const isSavingAuth = ref(false)
  const isSavingProxy = ref(false)
  const savingNotificationChannels = ref<Record<string, boolean>>({})
  const isSavingNotification = computed(
    () => Object.keys(savingNotificationChannels.value).length > 0,
  )
  const isSaving = computed(
    () => isSavingAuth.value || isSavingProxy.value || isSavingNotification.value,
  )

  const fetchSettings = async () => {
    if (isLoading.value) return
    isLoading.value = true
    try {
      const { data } = await configApi.getSettings()
      if (!data.value) return
      settings.value = data.value
      values.value = clone(data.value.values)
    } finally {
      isLoading.value = false
    }
  }

  const saveAuth = async () => {
    if (!values.value || isSaving.value) return null
    isSavingAuth.value = true
    try {
      const { data } = await configApi.updateAuth(values.value.auth)
      if (!data.value) return null
      settings.value = data.value
      values.value.auth = clone(data.value.values.auth)
      return data.value
    } finally {
      isSavingAuth.value = false
    }
  }

  const saveProxy = async () => {
    if (!values.value || isSaving.value) return null
    isSavingProxy.value = true
    try {
      const { data } = await configApi.updateProxy(values.value.proxy)
      if (!data.value) return null
      settings.value = data.value
      values.value.proxy = clone(data.value.values.proxy)
      return data.value
    } finally {
      isSavingProxy.value = false
    }
  }

  const saveNotificationChannel = async (channel: string) => {
    const value = values.value?.channels[channel]
    if (!values.value || !value || isSaving.value) return null

    savingNotificationChannels.value = {
      ...savingNotificationChannels.value,
      [channel]: true,
    }
    try {
      const { data } = await configApi.updateNotificationChannel(channel, value)
      if (!data.value) return null

      settings.value = data.value
      const savedChannel = data.value.values.channels[channel]
      if (savedChannel) {
        values.value.channels[channel] = clone(savedChannel)
      }
      if (savedChannel?.enabled !== true) {
        values.value.auth.authProviders = values.value.auth.authProviders.filter(
          (provider) => provider !== channel,
        )
      }
      return data.value
    } finally {
      const next = { ...savingNotificationChannels.value }
      delete next[channel]
      savingNotificationChannels.value = next
    }
  }

  return {
    settings,
    values,
    isLoading,
    isSaving,
    isSavingAuth,
    isSavingProxy,
    savingNotificationChannels,
    fetchSettings,
    saveAuth,
    saveProxy,
    saveNotificationChannel,
  }
}

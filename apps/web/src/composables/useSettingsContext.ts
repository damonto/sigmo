import { computed, inject, provide } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import { useAuthTest } from '@/composables/useAuthTest'
import { useSettings } from '@/composables/useSettings'
import { useSettingsForm } from '@/composables/useSettingsForm'
import { isApiError } from '@/lib/apiProblem'

const settingsContextKey = Symbol('settings-context')

const authTestFailureKey = (reason: string | undefined) => {
  switch (reason) {
    case 'discovery_failed':
      return 'settings.authTestOIDCDiscoveryFailed'
    case 'authorization_failed':
      return 'settings.authTestOIDCAuthorizationFailed'
    case 'token_exchange_failed':
      return 'settings.authTestOIDCTokenExchangeFailed'
    case 'token_invalid':
      return 'settings.authTestOIDCTokenInvalid'
    case 'subject_not_allowed':
      return 'settings.authTestSubjectNotAllowed'
    case 'access_assertion_missing':
      return 'settings.authTestAccessAssertionMissing'
    case 'access_assertion_invalid':
      return 'settings.authTestAccessAssertionInvalid'
    case 'popup_blocked':
      return 'settings.authTestPopupBlocked'
    case 'cancelled':
      return 'settings.authTestCancelled'
    case 'expired':
      return 'settings.authTestExpired'
    case 'https_required':
      return 'settings.authTestHTTPSRequired'
    default:
      return 'settings.authTestFailed'
  }
}

const createSettingsContext = () => {
  const { t } = useI18n()
  const {
    settings,
    values,
    isLoading,
    isSaving,
    isSavingAuth,
    isSavingProxy,
    isSavingNotifications,
    savingNotificationChannels,
    fetchSettings,
    saveAuth: saveAuthSettings,
    saveProxy: saveProxySettings,
    saveNotificationChannel,
    saveNotifications: saveNotificationSettings,
  } = useSettings()
  const form = useSettingsForm(settings, values)
  const { isTesting: isTestingAuth, test: runAuthTest } = useAuthTest(
    computed(() => values.value?.auth ?? null),
    isSaving,
  )

  const load = async () => {
    await fetchSettings()
    form.initializeExpandedChannels()
  }

  const saveAuth = async () => {
    if (isTestingAuth.value) return false
    const response = await saveAuthSettings()
    if (!response) return false

    toast.success(t('settings.saveSuccess'))
    return true
  }

  const testAuth = async () => {
    try {
      const result = await runAuthTest()
      if (!result || result.status === 'pending') return false
      if (result.status === 'succeeded') {
        const mode = values.value?.auth.mode
        const key =
          mode === 'oidc'
            ? 'settings.authTestOIDCSuccess'
            : mode === 'cloudflare_access'
              ? 'settings.authTestAccessSuccess'
              : 'settings.authTestOTPSuccess'
        toast.success(t(key))
        return true
      }
      toast.error(t(authTestFailureKey(result.reason), { subject: result.subject ?? '' }))
      return false
    } catch (error) {
      if (!isApiError(error)) toast.error(t('settings.authTestFailed'))
      return false
    }
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

  const saveNotifications = async () => {
    const response = await saveNotificationSettings()
    if (!response) return false

    toast.success(t('settings.saveSuccess'))
    return true
  }

  return {
    settings,
    values,
    isLoading,
    isSaving,
    isTestingAuth,
    isSavingAuth,
    isSavingProxy,
    isSavingNotifications,
    savingNotificationChannels,
    ...form,
    load,
    saveAuth,
    saveProxy,
    saveChannel,
    saveNotifications,
    testAuth,
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

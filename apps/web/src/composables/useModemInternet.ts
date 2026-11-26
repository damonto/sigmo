import { computed, onScopeDispose, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useInternetApi } from '@/apis/internet'
import { useModemPublicIP } from '@/composables/useModemPublicIP'
import type {
  InternetConnectionPreferencesPayload,
  InternetConnectionResponse,
} from '@/types/internet'

type Options = {
  modemId: ComputedRef<string>
  enabled?: ComputedRef<boolean>
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

type FetchConnectionOptions = {
  silent?: boolean
}

const pollIntervalMs = 5000
const defaultAPNAuth = 'default'
const defaultIPType = 'ipv4v6'

export const useModemInternet = ({ modemId, enabled, onSuccess, onError }: Options) => {
  const { t } = useI18n()
  const internetApi = useInternetApi()
  const isInternetEnabled = computed(() => enabled?.value ?? true)

  const internetConnection = ref<InternetConnectionResponse | null>(null)
  const internetAPN = ref('')
  const internetIPType = ref(defaultIPType)
  const internetAPNUsername = ref('')
  const internetAPNPassword = ref('')
  const internetAPNAuth = ref(defaultAPNAuth)
  const internetDefaultRoute = ref(false)
  const internetProxyEnabled = ref(false)
  const internetAlwaysOn = ref(false)
  const isInternetLoading = ref(false)
  const isInternetConnecting = ref(false)
  const isInternetDisconnecting = ref(false)
  const isInternetPreferencesUpdating = ref(false)
  const preferenceRequestEpoch = ref(0)
  const pollTimer = ref<number>()

  const isInternetConnected = computed(() => internetConnection.value?.status === 'connected')
  const canConnectInternet = computed(() => {
    return !isInternetConnected.value && !isInternetConnecting.value
  })

  const advancePreferenceRequestEpoch = () => {
    preferenceRequestEpoch.value += 1
    return preferenceRequestEpoch.value
  }

  const stopPolling = () => {
    if (pollTimer.value === undefined) return
    window.clearInterval(pollTimer.value)
    pollTimer.value = undefined
  }

  const startPolling = () => {
    if (pollTimer.value !== undefined) return
    pollTimer.value = window.setInterval(() => {
      if (!isInternetEnabled.value || isInternetPreferencesUpdating.value) return
      void fetchInternetConnection({ silent: true })
    }, pollIntervalMs)
  }

  const resetInternet = () => {
    stopPolling()
    internetConnection.value = null
    internetAPN.value = ''
    internetIPType.value = defaultIPType
    internetAPNUsername.value = ''
    internetAPNPassword.value = ''
    internetAPNAuth.value = defaultAPNAuth
    internetDefaultRoute.value = false
    internetProxyEnabled.value = false
    internetAlwaysOn.value = false
  }

  const applyConnection = (
    connection: InternetConnectionResponse | null,
    preservePreferences = false,
  ) => {
    internetConnection.value = connection
    if (!preservePreferences) {
      internetAPN.value = connection?.apn ?? ''
      internetIPType.value = connection?.ipType || defaultIPType
      internetAPNUsername.value = connection?.apnUsername ?? ''
      internetAPNPassword.value = connection?.apnPassword ?? ''
      internetAPNAuth.value = connection?.apnAuth || defaultAPNAuth
      internetDefaultRoute.value = connection?.defaultRoute ?? false
      internetProxyEnabled.value = connection?.proxyEnabled ?? false
      internetAlwaysOn.value = connection?.alwaysOn ?? false
    }
  }

  const fetchInternetConnection = async (options?: FetchConnectionOptions) => {
    const targetId = modemId.value
    if (!targetId || !isInternetEnabled.value) {
      resetInternet()
      return
    }
    if (!options?.silent) {
      isInternetLoading.value = true
    }
    const requestEpoch = preferenceRequestEpoch.value
    const preservePreferences = isInternetPreferencesUpdating.value
    try {
      const { data } = await internetApi.getCurrentConnection(targetId)
      if (!isInternetEnabled.value || modemId.value !== targetId) return
      if (requestEpoch !== preferenceRequestEpoch.value) return
      const connection = data.value ?? null
      applyConnection(connection, preservePreferences)
    } catch (err) {
      console.error('[useModemInternet] Failed to fetch connection:', err)
    } finally {
      if (!options?.silent) {
        isInternetLoading.value = false
      }
    }
  }

  const handleInternetConnect = async () => {
    const targetId = modemId.value
    if (!targetId || !isInternetEnabled.value) return
    if (!canConnectInternet.value) return
    isInternetConnecting.value = true
    try {
      const { data } = await internetApi.connect(targetId, {
        apn: internetAPN.value.trim(),
        ipType: internetIPType.value.trim(),
        apnUsername: internetAPNUsername.value.trim(),
        apnPassword: internetAPNPassword.value,
        apnAuth: internetAPNAuth.value === defaultAPNAuth ? '' : internetAPNAuth.value.trim(),
        defaultRoute: internetDefaultRoute.value,
        proxyEnabled: internetProxyEnabled.value,
        alwaysOn: internetAlwaysOn.value,
      })
      const connection = data.value ?? null
      applyConnection(connection)
      onSuccess?.(t('modemDetail.settings.internetConnectSuccess'))
    } catch (err) {
      console.error('[useModemInternet] Failed to connect:', err)
    } finally {
      isInternetConnecting.value = false
    }
  }

  const handleInternetDisconnect = async () => {
    const targetId = modemId.value
    if (!targetId || !isInternetEnabled.value) return
    if (isInternetDisconnecting.value || isInternetPreferencesUpdating.value) return
    isInternetDisconnecting.value = true
    try {
      await internetApi.disconnect(targetId)
      await fetchInternetConnection()
      onSuccess?.(t('modemDetail.settings.internetDisconnectSuccess'))
    } catch (err) {
      console.error('[useModemInternet] Failed to disconnect:', err)
    } finally {
      isInternetDisconnecting.value = false
    }
  }

  const handleInternetPreferencesUpdate = async (next: InternetConnectionPreferencesPayload) => {
    const targetId = modemId.value
    if (!targetId || !isInternetEnabled.value || !isInternetConnected.value) return
    if (isInternetPreferencesUpdating.value) return

    const mutationEpoch = advancePreferenceRequestEpoch()
    isInternetPreferencesUpdating.value = true
    const previous = {
      defaultRoute: internetDefaultRoute.value,
      proxyEnabled: internetProxyEnabled.value,
      alwaysOn: internetAlwaysOn.value,
    }
    internetDefaultRoute.value = next.defaultRoute
    internetProxyEnabled.value = next.proxyEnabled
    internetAlwaysOn.value = next.alwaysOn
    try {
      const { data } = await internetApi.updatePreferences(targetId, next)
      if (modemId.value !== targetId || preferenceRequestEpoch.value !== mutationEpoch) return
      applyConnection(data.value ?? null)
      onSuccess?.(t('modemDetail.settings.internetPreferencesSuccess'))
    } catch (err) {
      if (modemId.value !== targetId || preferenceRequestEpoch.value !== mutationEpoch) return
      internetDefaultRoute.value = previous.defaultRoute
      internetProxyEnabled.value = previous.proxyEnabled
      internetAlwaysOn.value = previous.alwaysOn
      console.error('[useModemInternet] Failed to update connection preferences:', err)
      onError?.(t('modemDetail.settings.internetPreferencesUpdateFailed'))
    } finally {
      if (preferenceRequestEpoch.value === mutationEpoch) {
        isInternetPreferencesUpdating.value = false
      }
    }
  }

  watch(
    [modemId, isInternetEnabled],
    async ([id, canUseInternet]) => {
      advancePreferenceRequestEpoch()
      isInternetPreferencesUpdating.value = false
      resetInternet()
      if (!id || !canUseInternet) {
        return
      }
      await fetchInternetConnection()
    },
    { immediate: true },
  )

  watch(
    [isInternetConnected, isInternetEnabled],
    ([connected, canUseInternet]) => {
      if (connected && canUseInternet) {
        startPolling()
        return
      }
      stopPolling()
    },
    { immediate: true },
  )

  onScopeDispose(stopPolling)

  const internetPublicInfo = useModemPublicIP(
    modemId,
    computed(() => {
      const connection = internetConnection.value
      if (!isInternetEnabled.value || connection?.status !== 'connected') return ''
      return connection.sessionId
    }),
  )

  return {
    internetConnection,
    internetPublicInfo,
    internetAPN,
    internetIPType,
    internetAPNUsername,
    internetAPNPassword,
    internetAPNAuth,
    internetDefaultRoute,
    internetProxyEnabled,
    internetAlwaysOn,
    isInternetLoading,
    isInternetConnecting,
    isInternetDisconnecting,
    isInternetPreferencesUpdating,
    isInternetConnected,
    canConnectInternet,
    handleInternetConnect,
    handleInternetDisconnect,
    handleInternetPreferencesUpdate,
  }
}

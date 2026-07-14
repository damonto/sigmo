import { onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useVoLTEApi } from '@/apis/volte'
import type {
  UpdateVoLTESettingsRequest,
  VoLTENetworkDriver,
  VoLTEQMINetworkDriver,
} from '@/types/volte'

type Options = {
  modemId: ComputedRef<string>
  enabled: ComputedRef<boolean>
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

const pollIntervalMs = 5000

export const useModemVoLTE = ({ modemId, enabled, onSuccess, onError }: Options) => {
  const { t } = useI18n()
  const api = useVoLTEApi()

  const volteEnabled = ref(false)
  const volteConnected = ref(false)
  const volteState = ref('')
  const volteDurationSeconds = ref(0)
  const volteModemRegistered = ref(false)
  const volteNetworkDriver = ref<VoLTENetworkDriver>('qmap')
  const setIMSAPNAsDefault = ref(false)
  const enablePCSCFViaPCO = ref(false)
  const isVoLTELoading = ref(false)
  const isVoLTEUpdating = ref(false)
  const isVoLTEFetching = ref(false)
  const pollTimer = ref<number>()
  const durationTimer = ref<number>()

  const stopTimers = () => {
    if (pollTimer.value !== undefined) window.clearInterval(pollTimer.value)
    if (durationTimer.value !== undefined) window.clearInterval(durationTimer.value)
    pollTimer.value = undefined
    durationTimer.value = undefined
  }

  const reset = () => {
    stopTimers()
    volteEnabled.value = false
    volteConnected.value = false
    volteState.value = ''
    volteDurationSeconds.value = 0
    volteModemRegistered.value = false
    volteNetworkDriver.value = 'qmap'
    setIMSAPNAsDefault.value = false
    enablePCSCFViaPCO.value = false
  }

  const fetchSettings = async (id: string, silent = false) => {
    if (!enabled.value || isVoLTEFetching.value) return
    isVoLTEFetching.value = true
    if (!silent) isVoLTELoading.value = true
    try {
      const { data } = await api.settings(id)
      if (!enabled.value || modemId.value !== id) return
      const settings = data.value
      volteEnabled.value = settings?.enabled ?? false
      volteConnected.value = settings?.connected ?? false
      volteState.value = settings?.state ?? ''
      volteDurationSeconds.value = settings?.durationSeconds ?? 0
      volteModemRegistered.value = settings?.modemRegistered ?? false
      volteNetworkDriver.value = settings?.networkDriver ?? 'qmap'
      setIMSAPNAsDefault.value = settings?.setIMSAPNAsDefault ?? false
      enablePCSCFViaPCO.value = settings?.enablePCSCFViaPCO ?? false
    } catch (err) {
      console.error('[useModemVoLTE] Failed to load settings:', err)
      if (!silent) onError?.(t('modemDetail.settings.volteLoadFailed'))
    } finally {
      if (!silent) isVoLTELoading.value = false
      isVoLTEFetching.value = false
    }
  }

  const updateVoLTE = async (nextEnabled: boolean) => {
    const id = modemId.value
    if (!enabled.value || !id || isVoLTEUpdating.value) return
    isVoLTEUpdating.value = true
    try {
      const payload: UpdateVoLTESettingsRequest = {
        enabled: nextEnabled,
        setIMSAPNAsDefault: setIMSAPNAsDefault.value,
        enablePCSCFViaPCO: enablePCSCFViaPCO.value,
      }
      if (volteNetworkDriver.value !== 'mbim') {
        payload.networkDriver = volteNetworkDriver.value
      }
      await api.updateSettings(id, payload)
      await fetchSettings(id)
      onSuccess?.(
        t(
          nextEnabled
            ? 'modemDetail.settings.volteEnabledSuccess'
            : 'modemDetail.settings.volteDisabledSuccess',
        ),
      )
    } catch (err) {
      console.error('[useModemVoLTE] Failed to update settings:', err)
      onError?.(t('modemDetail.settings.volteUpdateFailed'))
    } finally {
      isVoLTEUpdating.value = false
    }
  }

  const updateNetworkDriver = async (networkDriver: VoLTEQMINetworkDriver) => {
    const id = modemId.value
    if (!enabled.value || !id || volteEnabled.value || isVoLTEUpdating.value) return
    isVoLTEUpdating.value = true
    try {
      await api.updateSettings(id, {
        enabled: false,
        networkDriver,
        setIMSAPNAsDefault: setIMSAPNAsDefault.value,
        enablePCSCFViaPCO: enablePCSCFViaPCO.value,
      })
      await fetchSettings(id)
      onSuccess?.(t('modemDetail.settings.volteNetworkDriverSuccess'))
    } catch (err) {
      console.error('[useModemVoLTE] Failed to update network driver:', err)
      onError?.(t('modemDetail.settings.volteUpdateFailed'))
    } finally {
      isVoLTEUpdating.value = false
    }
  }

  const updateProfileOptions = async (next: {
    setIMSAPNAsDefault: boolean
    enablePCSCFViaPCO: boolean
  }) => {
    const id = modemId.value
    if (!enabled.value || !id || volteEnabled.value || isVoLTEUpdating.value) return
    isVoLTEUpdating.value = true
    try {
      await api.updateSettings(id, {
        enabled: false,
        networkDriver: volteNetworkDriver.value === 'mbim' ? undefined : volteNetworkDriver.value,
        setIMSAPNAsDefault: next.setIMSAPNAsDefault,
        enablePCSCFViaPCO: next.enablePCSCFViaPCO,
      })
      await fetchSettings(id)
      onSuccess?.(t('modemDetail.settings.volteProfileOptionsSuccess'))
    } catch (err) {
      console.error('[useModemVoLTE] Failed to update IMS profile options:', err)
      onError?.(t('modemDetail.settings.volteUpdateFailed'))
    } finally {
      isVoLTEUpdating.value = false
    }
  }

  watch(
    [modemId, enabled],
    async ([id, canUseVoLTE]) => {
      reset()
      if (!id || !canUseVoLTE) {
        return
      }
      await fetchSettings(id)
      if (!enabled.value || modemId.value !== id) return
      pollTimer.value = window.setInterval(() => void fetchSettings(id, true), pollIntervalMs)
    },
    { immediate: true },
  )

  watch(
    volteConnected,
    (connected) => {
      if (durationTimer.value !== undefined) window.clearInterval(durationTimer.value)
      durationTimer.value = undefined
      if (!connected) return
      durationTimer.value = window.setInterval(() => {
        volteDurationSeconds.value += 1
      }, 1000)
    },
    { immediate: true },
  )

  onUnmounted(stopTimers)

  return {
    volteEnabled,
    volteConnected,
    volteState,
    volteDurationSeconds,
    volteModemRegistered,
    volteNetworkDriver,
    setIMSAPNAsDefault,
    enablePCSCFViaPCO,
    isVoLTELoading,
    isVoLTEUpdating,
    updateVoLTE,
    updateNetworkDriver,
    updateProfileOptions,
  }
}

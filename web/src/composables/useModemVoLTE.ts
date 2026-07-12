import { computed, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useVoLTEApi } from '@/apis/volte'

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
  const volteCanEnable = ref(false)
  const volteModemRegistered = ref(false)
  const isVoLTELoading = ref(false)
  const isVoLTEUpdating = ref(false)
  const isVoLTEFetching = ref(false)
  const pollTimer = ref<number>()
  const durationTimer = ref<number>()

  const canUpdateVoLTE = computed(
    () => !isVoLTEUpdating.value && (volteEnabled.value || volteCanEnable.value),
  )

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
    volteCanEnable.value = false
    volteModemRegistered.value = false
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
      volteCanEnable.value = settings?.canEnable ?? false
      volteModemRegistered.value = settings?.modemRegistered ?? false
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
    if (!enabled.value || !id || !canUpdateVoLTE.value) return
    isVoLTEUpdating.value = true
    try {
      await api.updateSettings(id, { enabled: nextEnabled })
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
    volteCanEnable,
    volteModemRegistered,
    isVoLTELoading,
    isVoLTEUpdating,
    canUpdateVoLTE,
    updateVoLTE,
  }
}

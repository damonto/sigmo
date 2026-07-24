import { computed, ref, type ComputedRef, type Ref } from 'vue'

import { useModemInternet } from '@/composables/useModemInternet'
import { useModemMsisdn } from '@/composables/useModemMsisdn'
import { useModemWiFiCallingSettings } from '@/composables/useModemWiFiCallingSettings'
import type { EsimProfile } from '@/types/esim'
import type { Modem } from '@/types/modem'

type Options = {
  modemId: ComputedRef<string>
  modem: Ref<Modem | null>
  canUseWiFiCalling: ComputedRef<boolean>
  loadInternetConnection?: ComputedRef<boolean>
  loadWiFiCallingSettings?: ComputedRef<boolean>
  refreshModem: (id: string) => Promise<void>
  onSuccess?: (message: string) => void
}

export const useEsimProfileQuickActions = ({
  modemId,
  modem,
  canUseWiFiCalling,
  loadInternetConnection,
  loadWiFiCallingSettings,
  refreshModem,
  onSuccess,
}: Options) => {
  const msisdnDialogOpen = ref(false)
  const isInternetQuickActionRunning = ref(false)
  const isWiFiCallingQuickActionRunning = ref(false)
  const shouldLoadInternetConnection = computed(
    () => (loadInternetConnection?.value ?? true) || isInternetQuickActionRunning.value,
  )
  const shouldLoadWiFiCallingSettings = computed(
    () =>
      canUseWiFiCalling.value &&
      ((loadWiFiCallingSettings?.value ?? true) || isWiFiCallingQuickActionRunning.value),
  )

  const {
    isInternetLoading,
    isInternetConnecting,
    isInternetDisconnecting,
    isInternetConnected,
    handleInternetConnect,
    handleInternetDisconnect,
  } = useModemInternet({
    modemId,
    enabled: shouldLoadInternetConnection,
    onSuccess,
  })

  const {
    settingsWiFiCallingEnabled,
    settingsWiFiCallingConnected,
    settingsWiFiCallingState,
    isWiFiCallingSettingsLoading,
    isWiFiCallingSettingsUpdating,
    isWiFiCallingReconnecting,
    isWiFiCallingDisconnecting,
    handleWiFiCallingUpdate,
    reconnectWiFiCalling,
    disconnectWiFiCalling,
  } = useModemWiFiCallingSettings({
    modemId,
    enabled: shouldLoadWiFiCallingSettings,
    onSuccess,
  })

  const { msisdnInput, isMsisdnUpdating, isMsisdnValid, resetMsisdnInput, handleMsisdnUpdate } =
    useModemMsisdn({
      modemId,
      modem,
      refreshModem,
      onSuccess,
    })

  const isInternetBusy = computed(
    () =>
      isInternetLoading.value ||
      isInternetConnecting.value ||
      isInternetDisconnecting.value ||
      isInternetQuickActionRunning.value,
  )
  const isWiFiCallingBusy = computed(
    () =>
      isWiFiCallingSettingsLoading.value ||
      isWiFiCallingSettingsUpdating.value ||
      isWiFiCallingReconnecting.value ||
      isWiFiCallingDisconnecting.value ||
      isWiFiCallingQuickActionRunning.value ||
      settingsWiFiCallingState.value === 'connecting',
  )

  const openMsisdnDialog = () => {
    resetMsisdnInput()
    msisdnDialogOpen.value = true
  }

  const saveMsisdn = async () => {
    const updated = await handleMsisdnUpdate()
    if (updated) {
      msisdnDialogOpen.value = false
    }
  }

  const handleNetworkQuickToggle = async (_profile: EsimProfile, nextValue: boolean) => {
    isInternetQuickActionRunning.value = true
    try {
      if (nextValue) {
        await handleInternetConnect()
        return
      }
      await handleInternetDisconnect()
    } finally {
      isInternetQuickActionRunning.value = false
    }
  }

  const handleWiFiCallingQuickToggle = async (_profile: EsimProfile, nextValue: boolean) => {
    if (!canUseWiFiCalling.value) return
    isWiFiCallingQuickActionRunning.value = true
    try {
      if (!nextValue) {
        await disconnectWiFiCalling()
        return
      }
      if (!settingsWiFiCallingEnabled.value) {
        settingsWiFiCallingEnabled.value = true
        await handleWiFiCallingUpdate()
      }
      await reconnectWiFiCalling()
    } finally {
      isWiFiCallingQuickActionRunning.value = false
    }
  }

  return {
    msisdnDialogOpen,
    msisdnInput,
    isMsisdnUpdating,
    isMsisdnValid,
    isInternetConnected,
    isInternetBusy,
    settingsWiFiCallingEnabled,
    settingsWiFiCallingConnected,
    isWiFiCallingBusy,
    openMsisdnDialog,
    saveMsisdn,
    handleNetworkQuickToggle,
    handleWiFiCallingQuickToggle,
  }
}

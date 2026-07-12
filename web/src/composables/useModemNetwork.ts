import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNetworkApi } from '@/apis/network'
import type {
  BandResponse,
  ModeResponse,
  NetworkResponse,
  SetCurrentModesRequest,
} from '@/types/network'

type Options = {
  modemId: ComputedRef<string>
  onRegistered?: (id: string) => Promise<void> | void
  onChanged?: (id: string) => Promise<void> | void
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

export const useModemNetwork = ({
  modemId,
  onRegistered,
  onChanged,
  onSuccess,
  onError,
}: Options) => {
  const { t } = useI18n()
  const networkApi = useNetworkApi()

  const networkDialogOpen = ref(false)
  const availableNetworks = ref<NetworkResponse[]>([])
  const selectedNetwork = ref('')
  const modeOptions = ref<ModeResponse[]>([])
  const selectedMode = ref('')
  const supportedBands = ref<BandResponse[]>([])
  const selectedBands = ref<number[]>([])
  const airplaneModeSupported = ref(false)
  const airplaneModeEnabled = ref(false)
  const volteManaged = ref(false)
  const volteCanEnable = ref(false)
  const volteModemRegistered = ref(false)
  const isNetworkLoading = ref(false)
  const isNetworkRegistering = ref(false)
  const isNetworkSettingsLoading = ref(false)
  const isModeUpdating = ref(false)
  const isBandUpdating = ref(false)
  const isAirplaneModeUpdating = ref(false)
  const isVoLTEUpdating = ref(false)

  const hasAvailableNetworks = computed(() => availableNetworks.value.length > 0)
  const hasNetworkSelection = computed(() => selectedNetwork.value.trim().length > 0)
  const hasModeSelection = computed(() => selectedMode.value.trim().length > 0)
  const hasBandSelection = computed(() => selectedBands.value.length > 0)
  const canScanNetworks = computed(() => !isNetworkLoading.value && !airplaneModeEnabled.value)
  const canUpdateMode = computed(
    () => hasModeSelection.value && !isModeUpdating.value && !airplaneModeEnabled.value,
  )
  const canUpdateBands = computed(
    () => hasBandSelection.value && !isBandUpdating.value && !airplaneModeEnabled.value,
  )
  const canUpdateAirplaneMode = computed(
    () => airplaneModeSupported.value && !isAirplaneModeUpdating.value,
  )
  const canUpdateVoLTE = computed(
    () =>
      !isVoLTEUpdating.value &&
      !airplaneModeEnabled.value &&
      (volteManaged.value || volteCanEnable.value),
  )
  let networkSettingsRequestID = 0

  const resetNetworks = () => {
    networkDialogOpen.value = false
    availableNetworks.value = []
    selectedNetwork.value = ''
    modeOptions.value = []
    selectedMode.value = ''
    supportedBands.value = []
    selectedBands.value = []
    airplaneModeSupported.value = false
    airplaneModeEnabled.value = false
    resetVoLTE()
  }

  const resetVoLTE = () => {
    volteManaged.value = false
    volteCanEnable.value = false
    volteModemRegistered.value = false
  }

  const openNetworkDialog = async () => {
    const targetId = modemId.value
    if (!targetId) return
    if (!canScanNetworks.value) return
    selectedNetwork.value = ''
    isNetworkLoading.value = true
    try {
      const { data } = await networkApi.scanNetworks(targetId)
      availableNetworks.value = data.value ?? []
      networkDialogOpen.value = true
    } catch (err) {
      console.error('[useModemNetwork] Failed to scan networks:', err)
      availableNetworks.value = []
      networkDialogOpen.value = false
    } finally {
      isNetworkLoading.value = false
    }
  }

  const handleNetworkRegister = async () => {
    const targetId = modemId.value
    if (!targetId) return
    if (!hasNetworkSelection.value || isNetworkRegistering.value) return
    isNetworkRegistering.value = true
    try {
      await networkApi.registerNetwork(targetId, selectedNetwork.value)
      await onRegistered?.(targetId)
      networkDialogOpen.value = false
      onSuccess?.(t('modemDetail.settings.networkSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to register network:', err)
    } finally {
      isNetworkRegistering.value = false
    }
  }

  const refreshNetworkSettings = async () => {
    const targetId = modemId.value
    if (!targetId) return
    const requestId = ++networkSettingsRequestID
    isNetworkSettingsLoading.value = true
    try {
      const [modes, bands, airplane, volte] = await Promise.allSettled([
        networkApi.getModes(targetId),
        networkApi.getBands(targetId),
        networkApi.getAirplaneMode(targetId),
        networkApi.getVoLTE(targetId),
      ])
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return

      if (modes.status === 'fulfilled') {
        const modesData = modes.value.data.value
        modeOptions.value = modesData?.supported ?? []
        selectedMode.value = modesData ? modeKey(modesData.current) : ''
      } else {
        console.error('[useModemNetwork] Failed to load network modes:', modes.reason)
        modeOptions.value = []
        selectedMode.value = ''
      }

      if (bands.status === 'fulfilled') {
        const bandsData = bands.value.data.value
        supportedBands.value = bandsData?.supported ?? []
        selectedBands.value = bandsData?.current ?? []
      } else {
        console.error('[useModemNetwork] Failed to load network bands:', bands.reason)
        supportedBands.value = []
        selectedBands.value = []
      }

      if (airplane.status === 'fulfilled') {
        const airplaneData = airplane.value.data.value
        airplaneModeSupported.value = airplaneData?.supported ?? false
        airplaneModeEnabled.value = airplaneData?.enabled ?? false
      } else {
        console.error('[useModemNetwork] Failed to load airplane mode:', airplane.reason)
        airplaneModeSupported.value = false
        airplaneModeEnabled.value = false
      }

      if (volte.status === 'fulfilled') {
        const volteData = volte.value.data.value
        volteManaged.value = volteData?.managed ?? false
        volteCanEnable.value = volteData?.canEnable ?? false
        volteModemRegistered.value = volteData?.modemRegistered ?? false
      } else {
        console.error('[useModemNetwork] Failed to load VoLTE:', volte.reason)
        resetVoLTE()
      }
    } catch (err) {
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return
      console.error('[useModemNetwork] Failed to load network settings:', err)
      modeOptions.value = []
      selectedMode.value = ''
      supportedBands.value = []
      selectedBands.value = []
      airplaneModeSupported.value = false
      airplaneModeEnabled.value = false
      resetVoLTE()
    } finally {
      if (requestId === networkSettingsRequestID) {
        isNetworkSettingsLoading.value = false
      }
    }
  }

  const handleModeUpdate = async () => {
    const targetId = modemId.value
    const mode = modeFromKey(selectedMode.value)
    if (!targetId || !mode) return
    if (isModeUpdating.value) return
    isModeUpdating.value = true
    try {
      await networkApi.setCurrentModes(targetId, mode)
      await refreshNetworkSettings()
      onSuccess?.(t('modemDetail.settings.networkModeSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to set current modes:', err)
    } finally {
      isModeUpdating.value = false
    }
  }

  const toggleBand = (band: number, checked: boolean) => {
    const anyBand = 256
    if (!checked) {
      selectedBands.value = selectedBands.value.filter((value) => value !== band)
      return
    }
    if (band === anyBand) {
      selectedBands.value = [anyBand]
      return
    }
    selectedBands.value = selectedBands.value.filter((value) => value !== anyBand)
    if (!selectedBands.value.includes(band)) {
      selectedBands.value = [...selectedBands.value, band]
    }
  }

  const handleBandUpdate = async () => {
    const targetId = modemId.value
    if (!targetId) return
    if (!hasBandSelection.value || isBandUpdating.value) return
    isBandUpdating.value = true
    try {
      await networkApi.setCurrentBands(targetId, { bands: selectedBands.value })
      await refreshNetworkSettings()
      onSuccess?.(t('modemDetail.settings.networkBandSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to set current bands:', err)
    } finally {
      isBandUpdating.value = false
    }
  }

  const handleAirplaneModeUpdate = async (enabled: boolean) => {
    const targetId = modemId.value
    if (!targetId) return
    if (!canUpdateAirplaneMode.value) return
    isAirplaneModeUpdating.value = true
    try {
      await networkApi.setAirplaneMode(targetId, { enabled })
      await refreshNetworkSettings()
      await onChanged?.(targetId)
      onSuccess?.(
        enabled
          ? t('modemDetail.settings.networkAirplaneModeEnabledSuccess')
          : t('modemDetail.settings.networkAirplaneModeDisabledSuccess'),
      )
    } catch (err) {
      console.error('[useModemNetwork] Failed to set airplane mode:', err)
      onError?.(t('modemDetail.settings.networkAirplaneModeUpdateFailed'))
    } finally {
      isAirplaneModeUpdating.value = false
    }
  }

  const handleVoLTEUpdate = async (managed: boolean) => {
    const targetId = modemId.value
    if (!targetId) return
    if (!canUpdateVoLTE.value) return
    isVoLTEUpdating.value = true
    try {
      await networkApi.setVoLTE(targetId, { managed })
      await refreshNetworkSettings()
      await onChanged?.(targetId)
      onSuccess?.(
        managed
          ? t('modemDetail.settings.networkVoLTEEnabledSuccess')
          : t('modemDetail.settings.networkVoLTEDisabledSuccess'),
      )
    } catch (err) {
      console.error('[useModemNetwork] Failed to set VoLTE:', err)
      onError?.(t('modemDetail.settings.networkVoLTEUpdateFailed'))
    } finally {
      isVoLTEUpdating.value = false
    }
  }

  watch(
    modemId,
    async (id) => {
      if (!id) {
        resetNetworks()
        return
      }
      await refreshNetworkSettings()
    },
    { immediate: true },
  )

  return {
    networkDialogOpen,
    availableNetworks,
    selectedNetwork,
    modeOptions,
    selectedMode,
    supportedBands,
    selectedBands,
    airplaneModeSupported,
    airplaneModeEnabled,
    volteManaged,
    volteCanEnable,
    volteModemRegistered,
    isNetworkLoading,
    isNetworkRegistering,
    isNetworkSettingsLoading,
    isModeUpdating,
    isBandUpdating,
    isAirplaneModeUpdating,
    isVoLTEUpdating,
    hasAvailableNetworks,
    hasNetworkSelection,
    hasModeSelection,
    hasBandSelection,
    canScanNetworks,
    canUpdateMode,
    canUpdateBands,
    canUpdateAirplaneMode,
    canUpdateVoLTE,
    openNetworkDialog,
    handleNetworkRegister,
    refreshNetworkSettings,
    handleModeUpdate,
    toggleBand,
    handleBandUpdate,
    handleAirplaneModeUpdate,
    handleVoLTEUpdate,
  }
}

const modeKey = (mode: Pick<ModeResponse, 'allowed' | 'preferred'>) =>
  `${mode.allowed}:${mode.preferred}`

const modeFromKey = (key: string): SetCurrentModesRequest | null => {
  const parts = key.split(':')
  if (parts.length !== 2) return null
  const allowedPart = parts[0]
  const preferredPart = parts[1]
  if (allowedPart === undefined || preferredPart === undefined) return null
  const allowed = Number(allowedPart)
  const preferred = Number(preferredPart)
  if (!Number.isFinite(allowed) || !Number.isFinite(preferred)) return null
  return { allowed, preferred }
}
